package alttp

import (
	"bytes"
	"fmt"
	"log"
	"o2/snes"
	"o2/snes/asm"
	"strings"
)

func snesBankToLinear(addr uint32) uint32 {
	bank := addr >> 16
	linbank := ((bank & 1) << 15) + ((bank >> 1) << 16)
	linoffs := linbank + (addr & 0x7FFF)
	return linoffs
}

func xlatSNEStoPak(snes uint32) uint32 {
	if snes&0x8000 == 0 {
		if snes >= 0x700000 && snes < 0x7E0000 {
			sram := snesBankToLinear(snes-0x700000) + 0xE00000
			return sram
		} else if snes >= 0x7E0000 && snes < 0x800000 {
			wram := (snes - 0x7E0000) + 0xE50000
			return wram
		}
	}
	return snes
}

func (g *Game) updateWRAM() {
	if !g.local.Module.IsInGame() {
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

	// create an assembler:
	a := asm.Emitter{
		Code: &bytes.Buffer{},
		Text: &strings.Builder{},
	}
	a.SetBase(targetSNES)

	// assume 8-bit mode for accumulator and index registers:
	a.AssumeSEP(0x30)

	a.Comment("don't update if link is currently frozen:")
	a.LDA_abs(0x02E4)
	a.BEQ(0x01)
	a.RTS()

	// custom asm overrides update asm generation:
	if !g.generateCustomAsm(a) {
		if !g.generateUpdateAsm(a) {
			// nothing to emit:
			return
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
		panic(fmt.Errorf("generated update ASM larger than 255 bytes: %d", a.Code.Len()))
	}

	// prevent more updates until the upcoming write completes:
	g.updateStage = 1
	log.Println("write started")

	// calculate target address in FX Pak Pro address space:
	// SRAM starts at $E00000
	target := xlatSNEStoPak(targetSNES)
	g.lastUpdateTarget = target

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
				Address: xlatSNEStoPak(preMainAddr + 2),
				Size:    1,
				Data:    []byte{uint8(targetSNES >> 8)},
			},
		},
		func(cmd snes.Command, err error) {
			log.Println("write completed")
			defer g.updateLock.Unlock()
			g.updateLock.Lock()
			// expect a read now to prevent double-write:
			if g.updateStage == 1 {
				g.updateStage = 2
			}
		},
	).EnqueueTo(q)
	if err != nil {
		log.Println(fmt.Errorf("error enqueuing snes write for update routine: %w", err))
		return
	}
}

func (g *Game) generateCustomAsm(a asm.Emitter) bool {
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

func (g *Game) generateUpdateAsm(a asm.Emitter) bool {
	updated := false

	//// generate update ASM code for any 16-bit values:
	//a.REP(0x20)
	//for _, item := range g.syncableItems {
	//	if item.Size() != 2 {
	//		continue
	//	}
	//	if !item.IsEnabled() {
	//		continue
	//	}
	//
	//	// clone the assembler to a temporary:
	//	ta := a.Clone()
	//	// generate the update asm routine in the temporary assembler:
	//	u := item.GenerateUpdate(ta)
	//	if u {
	//		// don't emit the routine if it pushes us over the code size limit:
	//		if ta.Code.Len() + a.Code.Len() + 10 > 255 {
	//			// continue to try to find smaller routines that might fit:
	//			continue
	//		}
	//		a.Append(ta)
	//	}
	//
	//	updated = updated || u
	//}

	// generate update ASM code for any 8-bit values:
	for _, item := range g.syncableItems {
		if item.Size() != 1 {
			a.Comment(fmt.Sprintf("TODO: ignoring non-1 size syncableItem[%#04x]", item.Offset()))
			continue
		}
		if !item.IsEnabled() {
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

	if g.SyncUnderworld {
		for i := range g.underworld {
			// clone the assembler to a temporary:
			ta := a.Clone()
			// generate the update asm routine in the temporary assembler:
			u := g.underworld[i].GenerateUpdate(ta)
			if u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Code.Len()+a.Code.Len()+10 <= 255 {
					a.Append(ta)
					updated = true
				}
			}
		}
	}

	if g.SyncOverworld {
		for i := range g.overworld {
			// clone the assembler to a temporary:
			ta := a.Clone()
			// generate the update asm routine in the temporary assembler:
			u := g.overworld[i].GenerateUpdate(ta)
			if u {
				// don't emit the routine if it pushes us over the code size limit:
				if ta.Code.Len()+a.Code.Len()+10 <= 255 {
					a.Append(ta)
					updated = true
				}
			}
		}
	}

	return updated
}