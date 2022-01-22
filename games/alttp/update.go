package alttp

import (
	"bytes"
	"fmt"
	"log"
	"o2/snes"
	"o2/snes/asm"
	"o2/snes/lorom"
	"strings"
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

	// select target SRAM routine:
	var targetSNES uint32
	if g.nextUpdateA {
		targetSNES = preMainUpdateAAddr
	} else {
		targetSNES = preMainUpdateBAddr
	}

	// generate SRAM routine:
	// create an assembler:
	a := &asm.Emitter{
		Code: &bytes.Buffer{},
		Text: &strings.Builder{},
	}
	updated := g.generateSRAMRoutine(a, targetSNES)
	if !updated {
		return
	}

	// prevent more updates until the upcoming write completes:
	g.updateStage = 1
	log.Println("alttp: update: write started")

	// calculate target address in FX Pak Pro address space:
	// SRAM starts at $E00000
	target := lorom.BusAddressToPak(targetSNES)
	g.lastUpdateTarget = target
	g.lastUpdateFrame = g.lastGameFrame

	// write generated asm routine to SRAM:
	err := q.MakeWriteCommands(
		[]snes.Write{
			{
				Address: target,
				Size:    uint8(a.Code.Len()),
				Data:    a.Code.Bytes(),
			},
			// finally, update the JSR instruction to point to the updated routine:
			{
				// JSR $7C00 | JSR $7E00
				// update the $7C or $7E byte in the JSR instruction:
				Address: lorom.BusAddressToPak(preMainAddr + 2),
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
			}

			g.updateStage = 2

			q := make([]snes.Read, 0, 8)
			q = g.enqueueUpdateCheckRead(q)
			// must always read module number LAST to validate the prior reads:
			q = g.enqueueMainRead(q, nil)
			g.readSubmit(q)
		},
	).EnqueueTo(q)
	if err != nil {
		log.Println(fmt.Errorf("alttp: update: error enqueuing snes write for update routine: %w", err))
		return
	}
}

func (g *Game) generateSRAMRoutine(a *asm.Emitter, targetSNES uint32) (updated bool) {
	a.SetBase(targetSNES)

	// assume 8-bit mode for accumulator and index registers:
	a.AssumeSEP(0x30)

	a.Comment("don't update if link is currently frozen:")
	a.LDA_abs(0x02E4)
	a.BEQ(0x01)
	a.RTS()

	a.Comment("only sync during 00 submodule for modules 07,09,0B:")
	// NOTE: alternatively could branch backwards to the above RTS instruction but I'm too lazy
	// to figure out the values for that.

	//    LDA  $11  : BEQ cont              //    if (u8[$11] == $00) goto cont;
	//                                      //    else u8[$11] is non-zero:
	//    LDA  $10  : CMP #$07  : BEQ bail  //    if (u8[$10] == $07) goto bail;
	//                CMP #$09  : BEQ bail  //    if (u8[$10] == $09) goto bail;
	//                CMP #$0B  : BNE cont  //    if (u8[$10] != $0B) goto cont;
	//
	//bail:                                 // bail:
	//    RTS                               //    return;
	//cont:                                 // cont:

	a.LDA_dp(0x11)
	a.BEQ(15) // _cont
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BEQ(8) // _bail
	a.CMP_imm8_b(0x09)
	a.BEQ(4) // _bail
	a.CMP_imm8_b(0x0B)
	a.BNE(1) // _cont
	//_bail:
	a.RTS()
	//_cont:

	// custom asm overrides update asm generation:
	if !g.generateCustomAsm(a) {
		if !g.generateUpdateAsm(a) {
			// nothing to emit:
			return false
		}
	}

	// clear out our routine with an RTS instruction at the start:
	a.Comment("disable update routine with RTS instruction:")
	// MUST be in SEP(0x20) mode!
	a.LDA_imm8_b(0x60) // RTS
	a.STA_long(targetSNES)
	// back to 8-bit mode for accumulator:
	a.SEP(0x30)
	a.RTS()

	// dump asm:
	log.Print(a.Text.String())

	if a.Code.Len() > 255 {
		panic(fmt.Errorf("alttp: generated update ASM larger than 255 bytes: %d", a.Code.Len()))
	}

	return true
}

func (g *Game) enqueueUpdateCheckRead(q []snes.Read) []snes.Read {
	log.Println("alttp: update: enqueueUpdateCheckRead")
	// read the first instruction of the last update routine to check if it completed (if it's a RTS):
	addr := g.lastUpdateTarget
	if addr != 0xFFFFFF {
		q = g.readEnqueue(q, addr, 0x02, nil)
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
	updated := false

	// generate update ASM code for any 8-bit values:
	for offs, item := range g.syncableItems {
		if item.Size() != 1 {
			a.Comment(fmt.Sprintf("TODO: ignoring non-1 size syncableItem[%#04x]", offs))
			continue
		}
		if !item.IsEnabled() {
			continue
		}
		if !item.CanUpdate() {
			continue
		}

		// clone the assembler to a temporary:
		ta := a.Clone()
		// generate the update asm routine in the temporary assembler:
		u := item.GenerateUpdate(ta)
		if u {
			// don't emit the routine if it pushes us over the code size limit:
			if ta.Code.Len()+a.Code.Len()+10 <= 255 {
				a.Append(ta)
				updated = true
			}
		}
	}

	if g.SyncSmallKeys {
		// clone the assembler to a temporary:
		ta := a.Clone()
		// generate the update asm routine in the temporary assembler:
		u := g.doSyncSmallKeys(ta)
		if u {
			// don't emit the routine if it pushes us over the code size limit:
			if ta.Code.Len()+a.Code.Len()+10 <= 255 {
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
			if !s.CanUpdate() {
				continue
			}

			// clone the assembler to a temporary:
			ta := a.Clone()
			// generate the update asm routine in the temporary assembler:
			u := s.GenerateUpdate(ta)
			if u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Code.Len()+a.Code.Len()+10 <= 255 {
					a.Append(ta)
					updated = true
				}
			}
		}
	}

	{
		updated16 := false
		// clone to a temporary assembler for 16-bit mode:
		a16 := a.Clone()
		// switch to 16-bit mode:
		a16.Comment("switch to 16-bit mode:")
		a16.REP(0x30)

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

		// sync all the underworld supertile state:
		if g.SyncUnderworld {
			for i := range g.underworld {
				s := &g.underworld[i]
				if !s.IsEnabled() {
					continue
				}
				if !s.CanUpdate() {
					continue
				}

				// clone the assembler to a temporary:
				ta := a16.Clone()
				// generate the update asm routine in the temporary assembler:
				u := s.GenerateUpdate(ta)
				if u {
					// don't emit the routine if it pushes us over the code size limit:
					if ta.Code.Len()+a16.Code.Len()+a.Code.Len()+10 <= 255 {
						a16.Append(ta)
						updated16 = true
					}
				}
			}
		}

		// sync any other u16 data:
		for _, s := range g.syncableBitU16 {
			if !s.IsEnabled() {
				continue
			}
			if !s.CanUpdate() {
				continue
			}

			// clone the assembler to a temporary:
			ta := a16.Clone()
			// generate the update asm routine in the temporary assembler:
			u := s.GenerateUpdate(ta)
			if u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Code.Len()+a16.Code.Len()+a.Code.Len()+10 <= 255 {
					a16.Append(ta)
					updated16 = true
				}
			}
		}

		// append to the current assembler:
		if updated16 {
			// switch back to 8-bit mode:
			a16.Comment("switch back to 8-bit mode:")
			a16.SEP(0x30)
			if a.Code.Len()+a16.Code.Len()+10 <= 255 {
				// commit the changes to the parent assembler:
				a.Append(a16)
				updated = true
			}
		}
	}

	return updated
}
