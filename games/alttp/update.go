package alttp

import (
	"errors"
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"github.com/alttpo/snes/timing"
	"log"
	"o2/games"
	"o2/snes"
	"time"
)

func (g *Game) updateWRAM() {
	if !g.local.IsInGame() {
		return
	}

	q := g.queue
	if q == nil {
		return
	}

	defer g.updateLock.Unlock()
	g.updateLock.Lock()

	if g.updateStage > 0 {
		return
	}
	if time.Now().Sub(g.cooldownTime) < timing.Frame*30 {
		return
	}

	// select target SRAM routine:
	var targetSNES uint32
	if g.nextUpdateA {
		targetSNES = preMainUpdateAAddr
	} else {
		targetSNES = preMainUpdateBAddr
	}

	// generate SRAM routine:
	// create an assembler:
	a := asm.NewEmitter(make([]byte, 0x200), true)
	updated := g.generateSRAMRoutine(a, targetSNES)
	if !updated {
		return
	}

	// prevent more updates until the upcoming write completes:
	g.updateStage = 1
	log.Println("alttp: update: write started")

	// calculate target address in FX Pak Pro address space:
	// SRAM starts at $E00000
	var err error
	var target uint32
	target, err = lorom.BusAddressToPak(targetSNES)
	g.lastUpdateTarget = target
	g.lastUpdateFrame = g.lastGameFrame
	g.lastUpdateTime = time.Now()

	// write generated asm routine to SRAM:
	var targetJSR uint32
	targetJSR, err = lorom.BusAddressToPak(preMainAddr + 2)
	err = q.MakeWriteCommands(
		[]snes.Write{
			{
				Address: target,
				Size:    uint8(a.Len()),
				Data:    a.Bytes(),
			},
			// finally, update the JSR instruction to point to the updated routine:
			{
				// JSR $7C00 | JSR $7E00
				// update the $7C or $7E byte in the JSR instruction:
				Address: targetJSR,
				Size:    1,
				Data:    []byte{uint8(targetSNES >> 8)},
			},
		},
		func(cmd snes.Command, err error) {
			log.Println("alttp: update: write completed")

			defer g.updateLock.Unlock()
			g.updateLock.Lock()

			if g.updateStage != 1 {
				log.Printf("alttp: update: write complete but updateStage = %d (should be 1)\n", g.updateStage)
				return
			}

			g.updateStage = 2
			g.lastUpdateTime = time.Now()

			q := make([]snes.Read, 0, 8)
			q = g.enqueueUpdateCheckRead(q)
			// must always read module number LAST to validate the prior reads:
			q = g.enqueueMainRead(q, nil)
			g.readSubmit(q)
		},
	).EnqueueTo(q)
	if err != nil {
		log.Println(fmt.Errorf("alttp: update: error enqueuing snes write for update routine: %w", err))
		var termErr *snes.TerminalError
		if errors.As(err, &termErr) {
			log.Println("alttp: update: terminal error encountered; disconnecting from queue")
			_ = termErr
			g.queue = nil
		}
		return
	}
}

func (g *Game) generateSRAMRoutine(a *asm.Emitter, targetSNES uint32) (updated bool) {
	// don't update during non-zero submodules in main gameplay modules:
	module := g.wramU8(0x10)
	if module == 0x07 || module == 0x09 || module == 0x0b {
		// good module, check submodule:
		if g.wramU8(0x11) != 0 {
			return false
		}
	} else {
		// bad module:
		return false
	}
	// don't update if Link is currently frozen:
	if g.wramU8(0x02e4) != 0 {
		return false
	}

	a.SetBase(targetSNES)

	// assume 8-bit mode for accumulator and index registers:
	a.AssumeSEP(0x30)

	// clear out our routine with an RTS instruction at the start:
	a.Comment("disable update routine with RTS instruction and copy of $1A:")
	a.REP(0x30)
	a.LDA_imm16_lh(0x60, g.lastGameFrame) // RTS
	a.STA_long(targetSNES)
	a.SEP(0x30)

	a.Label("moduleCheck")
	a.Comment("only sync during 00 submodule for modules 07,09,0B:")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BEQ("submoduleCheck")
	a.CMP_imm8_b(0x09)
	a.BEQ("submoduleCheck")
	a.CMP_imm8_b(0x0B)
	a.BNE("syncExit")
	a.Label("submoduleCheck")
	a.LDA_dp(0x11)
	a.BNE("syncExit")
	a.Comment("don't update if Link is frozen:")
	a.LDA_abs(0x02E4)
	a.BEQ("syncStart")
	a.Label("syncExit")
	a.RTS()
	a.Label("syncStart")

	// custom asm overrides update asm generation:
	if !g.generateCustomAsm(a) {
		if !g.generateUpdateAsm(a) {
			// nothing to emit:
			return false
		}
	}

	// back to 8-bit mode for accumulator:
	if a.Flags()&0x30 != 0x30 {
		a.SEP(0x30)
	}
	a.RTS()

	if err := a.Finalize(); err != nil {
		a.WriteTextTo(log.Writer())
		panic(err)
	}

	// dump asm:
	a.WriteTextTo(log.Writer())

	if a.Len() > 255 {
		panic(fmt.Errorf("alttp: generated update ASM larger than 255 bytes: %d", a.Len()))
	}

	return true
}

func (g *Game) enqueueUpdateCheckRead(q []snes.Read) []snes.Read {
	log.Println("alttp: update: enqueueUpdateCheckRead")
	// read the first instruction of the last update routine to check if it completed (if it's a RTS):
	addr := g.lastUpdateTarget
	if addr != 0xFFFFFF {
		q = g.readEnqueue(q, addr, uint8(0x02+len(g.updateGenerators)), nil)
	}
	return q
}

func (g *Game) generateCustomAsm(a *asm.Emitter) bool {
	g.customAsmLock.Lock()
	defer g.customAsmLock.Unlock()

	if g.customAsm == nil {
		return false
	}

	a.Comment("custom asm code from websocket:")
	a.EmitBytes(g.customAsm)
	g.customAsm = nil

	return true
}

func (g *Game) generateUpdateAsm(a *asm.Emitter) bool {
	const epilogSize = 3

	updated := false
	if g.generated == nil {
		g.generated = make(map[uint32]struct{})
	} else {
		clear(g.generated)
	}
	if g.updateGenerators == nil {
		g.updateGenerators = make([]games.SyncStrategy, 0, 20)
	} else {
		g.updateGenerators = g.updateGenerators[:0]
	}

	tmp := [0x100]byte{}

	// group up all 16-bit updates first, then reset back to 8-bit and do those updates:

	// generate update ASM code for any 8-bit values:
	for offs := g.syncableOffsMin; offs <= g.syncableOffsMax; offs++ {
		var s games.SyncStrategy
		var ok bool

		if s, ok = g.syncable[offs]; !ok {
			continue
		}
		if !s.IsEnabled() {
			continue
		}
		if s.Size() != 1 {
			continue
		}

		// clone the assembler to a temporary:
		newEmitter := func() *asm.Emitter { return a.Clone(tmp[:]) }
		// generate the update asm routine in the temporary assembler:
		if u, ta := s.GenerateUpdate(newEmitter, uint32(len(g.updateGenerators))); u {
			// don't emit the routine if it pushes us over the code size limit:
			if ta.Len()+a.Len()+epilogSize <= 255 {
				g.updateGenerators = append(g.updateGenerators, s)
				g.generated[offs] = struct{}{}
				a.Append(ta)
				updated = true
			}
		}
	}

	if g.SyncSmallKeys {
		// clone the assembler to a temporary:
		ta := a.Clone(tmp[:])
		// generate the update asm routine in the temporary assembler:
		u := g.doSyncSmallKeys(ta)
		if u {
			// don't emit the routine if it pushes us over the code size limit:
			if ta.Len()+a.Len()+epilogSize <= 255 {
				//g.updateGenerators = append(g.updateGenerators, item)
				a.Append(ta)
				updated = true
			}
		}
	}

	if g.SyncOverworld {
		for i := range g.overworld {
			s := &g.overworld[i]
			if !s.IsEnabled() {
				continue
			}

			// clone the assembler to a temporary:
			newEmitter := func() *asm.Emitter { return a.Clone(tmp[:]) }
			// generate the update asm routine in the temporary assembler:
			if u, ta := s.GenerateUpdate(newEmitter, uint32(len(g.updateGenerators))); u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= 255 {
					g.updateGenerators = append(g.updateGenerators, s)
					a.Append(ta)
					updated = true
				}
			}
		}
	}

	{
		updated16 := false
		// clone to a temporary assembler for 16-bit mode:
		a16 := a.Clone(tmp[:])
		// switch to 16-bit mode:
		a16.Comment("switch to 16-bit mode:")
		a16.REP(0x30)

		tmp2 := [0x100]byte{}

		// sync u16 data:
		for offs := g.syncableOffsMin; offs <= g.syncableOffsMax; offs++ {
			var s games.SyncStrategy
			var ok bool

			if s, ok = g.syncable[offs]; !ok {
				continue
			}
			if !s.IsEnabled() {
				continue
			}
			if s.Size() != 2 {
				continue
			}

			// clone the assembler to a temporary:
			newEmitter := func() *asm.Emitter { return a16.Clone(tmp2[:]) }
			// generate the update asm routine in the temporary assembler:
			if u, ta := s.GenerateUpdate(newEmitter, uint32(len(g.updateGenerators))); u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a16.Len()+a.Len()+epilogSize <= 255 {
					g.updateGenerators = append(g.updateGenerators, s)
					a16.Append(ta)
					updated16 = true
				}
			}
		}

		// sync all the underworld supertile state:
		if g.SyncUnderworld {
			for i := range g.underworld {
				s := &g.underworld[i]
				if !s.IsEnabled() {
					continue
				}

				// clone the assembler to a temporary:
				newEmitter := func() *asm.Emitter { return a16.Clone(tmp2[:]) }
				// generate the update asm routine in the temporary assembler:
				if u, ta := s.GenerateUpdate(newEmitter, uint32(len(g.updateGenerators))); u {
					// don't emit the routine if it pushes us over the code size limit:
					if ta.Len()+a16.Len()+a.Len()+epilogSize <= 255 {
						g.updateGenerators = append(g.updateGenerators, s)
						a16.Append(ta)
						updated16 = true
					}
				}
			}
		}

		if g.SyncTunicColor {
			// update Link's palette:
			local := g.LocalPlayer()
			lightColor := local.PlayerColor

			// show current link palette colors:
			//currentColor9 := g.wramU16(0xC6E0 + (0x9 << 1))
			//currentColorA := g.wramU16(0xC6E0 + (0xA << 1))
			//currentColorB := g.wramU16(0xC6E0 + (0xB << 1))
			currentColorC := g.wramU16(0xC6E0 + (0xC << 1))
			//if currentColorC != lightColor {
			//	log.Printf("palette: [%s]\n", util.DelimitedGen(
			//		[]interface{}{currentColor9, currentColorA, currentColorB, currentColorC},
			//		func(v interface{}) string {
			//			return fmt.Sprintf("$%04x", v.(uint16))
			//		},
			//	))
			//}

			canUpdate := false
			if g.colorPendingUpdate > 0 {
				g.colorPendingUpdate--
				if currentColorC == g.colorUpdatedTo {
					g.colorPendingUpdate = 0
					canUpdate = true
				}
			} else {
				canUpdate = true
			}

			if canUpdate && currentColorC != lightColor {
				// link palette occupies last 16 colors of palette copy in WRAM ($7EC6E0..FF):
				// $7EC4E0..FF is a second copy of the palette used for restoring colors during special effects
				a16.Comment("update link palette:")

				// vanilla palette with green tunic from $9..$C is [$3647, $3b68, $0a4a, $12ef]
				// $9 =  dark tunic color
				// $A = light tunic color
				// $B =  dark cap color
				// $C = light cap color

				// set light color on cap:
				a16.LDA_imm16_w(lightColor)
				a16.STA_long(0x7EC6E0 + (0x0C << 1))
				a16.STA_long(0x7EC4E0 + (0x0C << 1))
				// set light color on tunic:
				a16.STA_long(0x7EC6E0 + (0x0A << 1))
				a16.STA_long(0x7EC4E0 + (0x0A << 1))

				// set dark color on tunic; make 75% as bright:
				darkColor := ((lightColor & 31) * 3 / 4) |
					((((lightColor >> 5) & 31) * 3 / 4) << 5) |
					((((lightColor >> 10) & 31) * 3 / 4) << 10)

				// set dark color on cap:
				a16.LDA_imm16_w(darkColor)
				a16.STA_long(0x7EC6E0 + (0x0B << 1))
				a16.STA_long(0x7EC4E0 + (0x0B << 1))
				// set dark color on tunic:
				a16.STA_long(0x7EC6E0 + (0x09 << 1))
				a16.STA_long(0x7EC4E0 + (0x09 << 1))

				// set $15 to non-zero to indicate palette copy:
				// TODO: is this safe to do as a 16-bit operation? should be fine for 99.9998% of the time.
				a16.INC_dp(0x15)

				updated16 = true
				g.colorPendingUpdate = 4
				g.colorUpdatedTo = lightColor
			}
		}

		// append to the current assembler:
		if updated16 {
			// switch back to 8-bit mode:
			//a16.Comment("switch back to 8-bit mode:")
			//a16.SEP(0x30)
			if a.Len()+a16.Len()+epilogSize <= 255 {
				// commit the changes to the parent assembler:
				a.Append(a16)
				updated = true
			}
		}
	}

	return updated
}
