package alttp

import (
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"github.com/alttpo/snes/timing"
	"log"
	"o2/games"
	"o2/snes"
	"strings"
	"time"
)

func (g *Game) updateWRAM(q snes.Queue) (writes []snes.Write, ok bool) {
	ok = false

	if !g.local.IsInGame() {
		return
	}
	// never write update routines to SRAM unless the SNES is running the ROM we patched:
	if !g.ROMVerified {
		return
	}

	if q == nil {
		return
	}

	if g.updateStage > 0 {
		return
	}
	if time.Now().Sub(g.cooldownTime) < timing.Frame*2 {
		return
	}

	// select target SRAM routine:
	var targetSNES uint32
	if g.nextUpdateA {
		targetSNES = updateBufAAddr
	} else {
		targetSNES = updateBufBAddr
	}

	// generate SRAM routine:
	var err error
	// create an assembler:
	a := asm.NewEmitter(make([]byte, updateBufSize*2), true)
	var updated bool
	updated, err = g.generateSRAMRoutine(a, targetSNES)
	if err != nil {
		log.Printf("alttp: error generating asm update routine: %v\n", err)
		return
	}
	if !updated {
		return
	}

	// prevent more updates until the upcoming write completes:
	g.updateStage = 1
	log.Println("alttp: update: write started")

	// calculate target address in FX Pak Pro address space:
	// SRAM starts at $E00000
	var target uint32
	target, err = lorom.BusAddressToPak(targetSNES)
	g.lastUpdateTarget = target
	g.lastUpdateFrame = g.lastGameFrame
	g.lastUpdateTime = time.Now()

	// write generated asm routine to SRAM:
	var targetJSR uint32
	targetJSR, err = lorom.BusAddressToPak(updateJSRAddr)
	code := a.Bytes()
	writes = []snes.Write{
		{
			// may be larger than 255 bytes; the device drivers split it into chunks (e.g. FX Pak Pro VPUTs) and write
			// them in order. Write all but the first two bytes first: if callUpdate still points at this buffer
			// (e.g. after o2 restarted), the routine that was there already ran and starts with RTS, so the game
			// never runs a partially written routine:
			Address: target + 2,
			Data:    code[2:],
		},
		{
			Address: target,
			Size:    2,
			Data:    code[:2],
		},
		// finally, update the JSR instruction to point to the updated routine:
		{
			// JSR $7400 | JSR $7800
			// update the $74 or $78 byte in the JSR instruction:
			Address: targetJSR,
			Size:    1,
			Data:    []byte{uint8(targetSNES >> 8)},
		},
	}
	ok = true

	return
}

func (g *Game) generateSRAMRoutine(a *asm.Emitter, targetSNES uint32) (updated bool, err error) {
	module := g.wramU8(0x10)
	if module == 0x07 || module == 0x09 || module == 0x0b {
		// good module, check submodule:
		if g.wramU8(0x11) != 0 {
			updated = false
			return
		}
		// don't update during cutscenes, when Link is frozen:
		if g.wramU8(0x02e4) != 0 {
			updated = false
			return
		}
	} else if module == 0x0e {
		// the menus, map and text boxes are ok. The patched ROM runs update routines in this module too, so updates
		// don't wait for the player to close them. NPCs freeze Link ($02E4) while they talk, so that is ignored here;
		// the generated routines check for the modules they need themselves.
	} else {
		// bad module:
		updated = false
		return
	}

	// refer to patcher.go for the SRAM stub (callUpdate) which JSRs here every frame
	a.SetBase(targetSNES)

	// assume 8-bit mode for accumulator and index registers:
	a.AssumeSEP(0x30)

	// custom asm overrides update asm generation:
	if !g.generateCustomAsm(a) {
		if !g.generateUpdateAsm(a) {
			// nothing to emit:
			updated = false
			return
		}
	}

	// clear out our routine with an RTS instruction at the start: {
	a.Comment("disable update routine with RTS instruction and copy of $1A:")
	a.REP(0x30)
	a.LDA_imm16_lh(0x60, g.lastGameFrame) // RTS
	a.STA_long(targetSNES)
	a.SEP(0x30)
	a.RTS()
	// } 12 bytes

	if err = a.Finalize(); err != nil {
		logAsm(a)
		return
	}

	// dump asm:
	logAsm(a)

	if a.Len() > updateBufSize {
		err = fmt.Errorf("alttp: generated update ASM larger than %d bytes: %d", updateBufSize, a.Len())
		return
	}

	updated = true
	return
}

// asmExecAborter is implemented by update generators that must be told when their generated routine could not be
// confirmed to have executed:
type asmExecAborter interface {
	AbortAsm()
}

// abortUpdateGenerators notifies all generators of the in-flight update routine that it was aborted.
func (g *Game) abortUpdateGenerators() {
	for _, generator := range g.updateGenerators {
		if aborter, ok := generator.(asmExecAborter); ok {
			aborter.AbortAsm()
		}
	}
	g.updateGenerators = nil
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
	const epilogSize = 12

	updated := false
	if g.generated == nil {
		g.generated = make(map[uint32]struct{})
	} else {
		clear(g.generated)
	}

	genIndex := uint32(0)
	asmConfirmers := g.updateGenerators[:0]
	g.updateGenerators = nil

	{
		// 8-bit updates first:
		tmp := [updateBufSize * 2]byte{}
		newEmitter := func() *asm.Emitter { return a.Clone(tmp[:]) }

		// generate update ASM code for any 8-bit values:
		for offs := g.syncableOffsMin; offs <= g.syncableOffsMax; offs++ {
			var s games.SyncStrategy

			if s = g.syncable[offs]; s == nil {
				continue
			}
			if !s.IsEnabled() {
				continue
			}
			if s.Size() != 1 {
				continue
			}

			if u, ta := s.GenerateUpdate(newEmitter, genIndex); u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, s)
					g.generated[offs] = struct{}{}
					a.Append(ta)
					updated = true
				}
			}
		}

		// not tied to a sync setting: synced prizes are what leave the player trapped in the boss room:
		if u, ta := g.generateBossPrizeUpdate(newEmitter, genIndex); u != nil {
			// don't emit the routine if it pushes us over the code size limit:
			if ta.Len()+a.Len()+epilogSize <= updateBufSize {
				genIndex++
				asmConfirmers = append(asmConfirmers, u)
				g.bossPrize.inflight = u

				a.Append(ta)
				updated = true
			}
		}

		if g.SyncUnderworld {
			if u, ta := g.generateCrystalSwitchUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					g.crystalSwitch.inflight = u

					a.Append(ta)
					updated = true
				}
			}
			if u, ta := g.generateRoomStateUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					g.roomState.inflight = u

					a.Append(ta)
					updated = true
				}
			}
			if u, ta := g.generateTorchesUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					g.torches.inflight = u

					a.Append(ta)
					updated = true
				}
			}
			if u, ta := g.generateStarSwitchUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					g.starSwitch.inflight = u

					a.Append(ta)
					updated = true
				}
			}
			if u, ta := g.generateFlyingTilesUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					g.flyingTiles.inflight = u

					a.Append(ta)
					updated = true
				}
			}
		}

		if g.SyncSmallKeys {
			g.adoptSmallKeys()
			for k := 0; k < smallKeyCount; k++ {
				if u, ta := g.generateSmallKeyUpdate(k, newEmitter, genIndex); u != nil {
					// don't emit the routine if it pushes us over the code size limit:
					if ta.Len()+a.Len()+epilogSize <= updateBufSize {
						genIndex++
						asmConfirmers = append(asmConfirmers, u)
						u.begin()

						a.Append(ta)
						updated = true
					}
				}
			}
		}

		if g.SyncHearts {
			g.adoptHearts()
			if u, ta := g.generateHeartsUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					u.begin()

					a.Append(ta)
					updated = true
				}
			}
		}

		if g.SyncOverworld {
			if u, ta := g.generateOwEventsUpdate(newEmitter, genIndex); u != nil {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a.Len()+epilogSize <= updateBufSize {
					genIndex++
					asmConfirmers = append(asmConfirmers, u)
					g.owEvents.inflight = u

					a.Append(ta)
					updated = true
				}
			}
		}

		if g.SyncOverworld {
			newEmitter := func() *asm.Emitter { return a.Clone(tmp[:]) }

			for i := range g.overworld {
				s := &g.overworld[i]
				if !s.IsEnabled() {
					continue
				}

				// generate the update asm routine in the temporary assembler:
				if u, ta := s.GenerateUpdate(newEmitter, genIndex); u {
					// don't emit the routine if it pushes us over the code size limit:
					if ta.Len()+a.Len()+epilogSize <= updateBufSize {
						genIndex++
						asmConfirmers = append(asmConfirmers, s)

						a.Append(ta)
						updated = true
					}
				}
			}
		}
	}

	{
		updated16 := false
		gen16Index := genIndex
		asm16Confirmers := make([]games.AsmExecConfirmer, 0, 20)
		// run once the 16-bit section is committed; the whole section is dropped if it does not fit, and routines
		// waiting for confirmation of a dropped routine would wait forever:
		var commit16 []func()

		// clone to a temporary assembler for 16-bit mode:
		tmp := [updateBufSize * 2]byte{}
		a16 := a.Clone(tmp[:])

		// switch to 16-bit mode:
		a16.Comment("switch to 16-bit mode:")
		a16.REP(0x30)

		tmp2 := [updateBufSize * 2]byte{}
		newEmitter := func() *asm.Emitter { return a16.Clone(tmp2[:]) }

		// sync u16 data:
		for offs := g.syncableOffsMin; offs <= g.syncableOffsMax; offs++ {
			var s games.SyncStrategy

			if s = g.syncable[offs]; s == nil {
				continue
			}
			if !s.IsEnabled() {
				continue
			}
			if s.Size() != 2 {
				continue
			}

			// generate the update asm routine in the temporary assembler:
			if u, ta := s.GenerateUpdate(newEmitter, gen16Index); u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Len()+a16.Len()+a.Len()+epilogSize <= updateBufSize {
					gen16Index++
					asm16Confirmers = append(asm16Confirmers, s)

					a16.Append(ta)
					updated16 = true
				}
			}
		}

		// apply map16 changes made by players in the same overworld area:
		if g.SyncOverworld {
			if u, ta := g.generateMap16Update(newEmitter, gen16Index); u != nil {
				if n := ta.Len() + a16.Len() + a.Len() + epilogSize; n > updateBufSize {
					log.Printf("alttp: map16: update routine too big: %d + %d + %d + %d = %d > %d\n", ta.Len(), a16.Len(), a.Len(), epilogSize, n, updateBufSize)
				}
				if ta.Len()+a16.Len()+a.Len()+epilogSize <= updateBufSize {
					gen16Index++
					asm16Confirmers = append(asm16Confirmers, u)
					commit16 = append(commit16, func() { g.map16.inflight = u })

					a16.Append(ta)
					updated16 = true
				}
			}
		}

		// reveal objects and move push blocks like the players in the same room did:
		if g.SyncUnderworld {
			if u, ta := g.generateManipUpdate(newEmitter, gen16Index); u != nil {
				if ta.Len()+a16.Len()+a.Len()+epilogSize <= updateBufSize {
					gen16Index++
					asm16Confirmers = append(asm16Confirmers, u)
					commit16 = append(commit16, func() { g.manip.inflight = u })

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

				// generate the update asm routine in the temporary assembler:
				if u, ta := s.GenerateUpdate(newEmitter, gen16Index); u {
					// don't emit the routine if it pushes us over the code size limit:
					if ta.Len()+a16.Len()+a.Len()+epilogSize <= updateBufSize {
						gen16Index++
						asm16Confirmers = append(asm16Confirmers, s)

						a16.Append(ta)
						updated16 = true
					}
				}
			}
		}

		// set the player's color for the patch to apply to Link's palette (see linkcolor.go):
		{
			ta := newEmitter()
			if u, commit := g.generateLinkColorUpdate(ta); u {
				if ta.Len()+a16.Len()+a.Len()+epilogSize <= updateBufSize {
					a16.Append(ta)
					updated16 = true
					commit16 = append(commit16, commit)
				}
			}
		}

		// append to the current assembler:
		if updated16 {
			// switch back to 8-bit mode:
			//a16.Comment("switch back to 8-bit mode:")
			//a16.SEP(0x30)
			if a.Len()+a16.Len()+epilogSize <= updateBufSize {
				// commit the changes to the parent assembler:
				asmConfirmers = append(asmConfirmers, asm16Confirmers...)
				genIndex = gen16Index
				for _, f := range commit16 {
					f()
				}

				a.Append(a16)
				updated = true
			} else {
				log.Printf("alttp: update: 16-bit section too big: %d + %d + %d > %d; dropped\n", a.Len(), a16.Len(), epilogSize, updateBufSize)
			}
		}
	}

	g.updateGenerators = asmConfirmers

	return updated
}

// logAsm logs the emitted asm text in a single write so other log messages can't interleave with it:
func logAsm(a *asm.Emitter) {
	sb := &strings.Builder{}
	_ = a.WriteTextTo(sb)
	log.Print(sb.String())
}

// emitReport stores the low byte of A (16-bit mode) as this generator's result for ConfirmAsmExecuted.
func emitReport(a *asm.Emitter, index uint32) {
	a.SEP(0x20)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x20)
}
