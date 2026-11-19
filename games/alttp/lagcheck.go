package alttp

import (
	"fmt"

	"github.com/alttpo/snes/asm"
)

// Lag checks
//
// The patch must not make the game lag more than it does by itself. The game's main loop runs a frame (the module,
// then PrepareOAMForTransfer and `STZ $12`) after each NMI; if the NMI at the start of vblank (scanline 225) comes
// before `STZ $12`, the frame's updates are skipped: a lag frame. So every piece of the patch's per-frame sync code
// runs after the module, when the scanline shows how much of the frame is left, and first checks the time (see
// emitTimeCheck): if the most it can cost, plus the game's work after preMain (rfTailCycles), would end after the NMI,
// it skips its work for this frame, or does less of it. Each later check sees the time the earlier ones used.
//
//   - the update routine o2 generates (see update.go) starts with a time check against the scanline o2 computes from
//     the routine's estimated worst case (estimateUpdateCycles); when late, it returns without disabling itself, so it
//     runs again in the next frame. preMain calls it right after the module, in the same modules as before.
//   - link color (emitLinkColor), sound effects (emitSfx), the manipulable objects pending after a room load
//     (emitManipPending: before each object), copying Link's OAM entries (preMain) and remoteFrame (see remoteframe.go)
//     each check before their work.
//   - not checked: the stage hooks (emitMap16StageHook, emitManipStageHook), which run inside the game's area and room
//     loads. Those frames take several frames' time by themselves (measured on the JP ROM, lagcost_measure_test.go: up
//     to 145,000 cycles of the game's own work after the map16 hook, and more after the manip hook), so where such a
//     frame ends, and whether a hook's cycles move its end past another NMI, can't be told from the scanline at the
//     hook. A hook costs about 40 cycles with nothing staged for any room, and up to map16StageHookCycles and
//     manipStageHookCycles with full tables; the chance that it adds a lag frame to a load is about its cycles over a
//     frame's (about 45,000).
//
// A late frame is not special: the NMI that came during it starts the next frame's time (see remoteframe.go).

const (
	// the earliest late scanline a time check can test (see emitTimeCheck): lines 0..55 are also what the vblank lines
	// 256..261 (and PAL's) read as:
	lagMinLateLine = 56

	// the time checks of the pieces of preMain after the module, each from the check to the next check (or the end of
	// preMain), with a margin over what lagcheck_test.go (TestLagCheck_Budgets) measures from the check to the end of
	// the piece (130, 184, 170 and 217 + RoomDraw_16x16Single); sfx's also covers manipPending with nothing pending and
	// the sprites gate test after it:
	linkColorCycles    = 300
	sfxCycles          = 300
	linkOAMCopyCycles  = 250
	manipPendingCycles = 150
	// one object of the manipulable objects pending after a room load: RoomDraw_16x16Single and the pending entry:
	manipPendingEntryCycles = updRoomDrawCycles + 300

	// the stage hooks' worst case (estimated): the map16 hook scanning a full table, the manip hook a full table
	// against 16 slots with a few matches:
	map16StageHookCycles = 3_000
	manipStageHookCycles = 18_000

	// an update routine whose estimate is longer than a frame has after the module can't run without lag frames, which
	// the game also has when it does the same for the local player (decompressing the sword's graphics, spawning a
	// dungeon prize, the mirror warp's area reload). It runs only in frames whose module ended by this scanline, so it
	// costs no more lag frames than its own length:
	updHeavyLateLine = 120
)

// lateLineFor returns the first scanline on which work of the given cycles, followed by the game's work after preMain,
// starts too late to finish before the NMI; 0 if it can't finish before the NMI from any scanline.
func lateLineFor(cycles int) uint8 {
	l := rfNMILine - (cycles+rfTailCycles+rfCyclesPerLine-1)/rfCyclesPerLine
	if l < lagMinLateLine {
		return 0
	}
	return uint8(l)
}

// mustLateLineFor is lateLineFor for the patch's fixed budgets, which always fit.
func mustLateLineFor(cycles int, what string) uint8 {
	l := lateLineFor(cycles)
	if l == 0 {
		panic(fmt.Errorf("%s: %d cycles never fit in a frame", what, cycles))
	}
	return l
}

// emitTimeCheckLong is emitTimeCheck with long addressing, for code that runs with any DB. Called with A 8-bit;
// clobbers A. About 22 cycles.
func emitTimeCheckLong(a *asm.Emitter, lateLine uint8, p, late string) {
	a.Comment(fmt.Sprintf("time check: the scanline, latched; too late from line %d until vblank:", lateLine))
	a.Label(p)
	a.LDA_long(0x002137)
	a.LDA_long(0x00213F)
	a.LDA_long(0x00213D)
	a.CMP_imm8_b(rfNMILine)
	a.BCS(p + "_ok")
	a.CMP_imm8_b(lateLine)
	a.BCC(p + "_ok")
	a.JMP_abs(late)
	a.Label(p + "_ok")
}

// The update routine's time check (emitUpdateTimeCheck) is at its start; o2 writes the late scanline into its CMP once
// the routine is generated (setUpdateLateLine):
const (
	// the offset of the late scanline's byte in the routine:
	updLateLineOffs = 4*3 + 2 + 2 + 1
	// the bytes of the time check:
	updTimeCheckSize = updLateLineOffs + 1 + 2 + 1
)

// emitUpdateTimeCheck emits the time check at the start of an update routine: return, without disabling the routine,
// unless the frame has time left for it. The late scanline is a placeholder until setUpdateLateLine.
func emitUpdateTimeCheck(a *asm.Emitter) {
	start := a.Len()
	a.Comment("time check: the scanline, latched; return to run again next frame if too late for this routine:")
	a.LDA_long(0x002137)
	a.LDA_long(0x00213F)
	a.LDA_long(0x00213D)
	a.CMP_imm8_b(rfNMILine)
	a.BCS("upd_time_ok")
	a.CMP_imm8_b(lagMinLateLine)
	a.BCC("upd_time_ok")
	a.RTS()
	a.Label("upd_time_ok")
	if a.Len()-start != updTimeCheckSize {
		panic(fmt.Errorf("update time check: %d bytes, expected %d", a.Len()-start, updTimeCheckSize))
	}
}

// setUpdateLateLine writes the late scanline of an update routine whose worst case is cycles into its time check, and
// returns it.
func setUpdateLateLine(code []byte, cycles int) uint8 {
	l := lateLineFor(cycles)
	if l == 0 {
		l = updHeavyLateLine
	}
	code[updLateLineOffs] = l
	return l
}

// The cycles of the game routines that update routines call, measured on the JP ROM (lagcost_measure_test.go,
// TestLagCost_GameRoutines) with about half again as much for other states:
const (
	updRoomDrawCycles = 1_000
)

var updCalleeCycles = map[romFunction]int{
	fnUpdatePaletteArmorGloves:      1_100,             // 745
	fnUpdatePaletteSword:            400,               // 201
	fnUpdatePaletteShield:           400,               // 243
	fnDecompGfxSword:                200_000,           // 170,227: two sheets decompressed
	fnDecompGfxShield:               200_000,           // 160,887
	fnLoadSpriteGfx:                 1_800,             // 1,176
	fnOverworldFinishMirrorWarp:     700_000,           // 613,024: the area reloaded
	fnOverworldCreatePyramidHole:    3_400,             // 2,218
	fnOverworldAlterWeathervane:     3_000,             // 1,980
	fnOverworldOpenGargoylesDomain:  2_400,             // 1,556
	fnSkullWoodsFire:                3_000,             // not measured; like the other entrance routines
	fnResetSpriteProperties:         400,               // 208
	fnToggleStarTileGraphics:        1_400,             // 890
	fnOverworldDrawMap16Persist:     400,               // 248
	fnRoomDraw16x16Single:           updRoomDrawCycles, // 627
	fnOverworldRevealSecret:         400,               // 223
	fnRevealPotItem:                 400,               // 186
	fnSpawnThrowableTerrainSilently: 1_000,             // 586
	fnUnderworldLightTorch:          1_500,             // 1,000
	fnUpdateTilemapWithCommonObject: 1_000,             // 662
	fnRoomTagSpawnPrize:             200_000,           // 171,580: the prize's graphics decompressed
}

// The cycles of the ROM routines behind the SRAM stubs, which update routines call with a table (see updatedata.go):
// a base plus each entry with the game routines it calls. They are charged to the table (updateData.add).
const (
	updTableBaseCycles = 300
	// map16Update: Overworld_DrawMap16_Persist and memorizing the change; a reveal adds Overworld_RevealSecret and the
	// spawn:
	updMap16EntryCycles  = 400 + 300
	updMap16RevealCycles = 400 + 1_000 + 400
	// manipUpdate: up to two RoomDraw_16x16Single (a moved block), or one with RevealPotItem and the spawn:
	updManipEntryCycles = 2*updRoomDrawCycles + 400 + 1_000 + 500
	// openChests: drawing an open chest:
	updChestEntryCycles = updRoomDrawCycles + 300
	// flyingTilesErase: UpdateTilemapWithCommonObject:
	updFlyingTileEntryCycles = 1_000 + 200
)

const (
	// the most an instruction of a generated routine costs (a 16-bit read-modify-write with indexing):
	updInstrCycles = 9
	// the iterations a loop of a generated routine is charged with (they go over the 16 sprite slots or fewer):
	updLoopIterations = 16
	// a call of an unknown routine:
	updUnknownCallCycles = 2_000
)

// instrLen returns the length of the 65816 instruction with opcode op, given the M and X flags (true: 8-bit).
func instrLen(op byte, m8, x8 bool) int {
	imm := func(eight bool) int {
		if eight {
			return 2
		}
		return 3
	}
	lo, hi := op&0x0F, op>>4
	switch lo {
	case 0x0:
		switch op {
		case 0x20:
			return 3
		case 0x40, 0x60:
			return 1
		case 0xA0, 0xC0, 0xE0:
			return imm(x8)
		}
		return 2
	case 0x2:
		switch op {
		case 0x22:
			return 4
		case 0x62, 0x82:
			return 3
		case 0xA2:
			return imm(x8)
		}
		return 2
	case 0x4:
		switch op {
		case 0x44, 0x54, 0xF4:
			return 3
		}
		return 2
	case 0x8, 0xA, 0xB:
		return 1
	case 0x9:
		if hi&1 == 0 {
			return imm(m8)
		}
		return 3
	case 0xC:
		if op == 0x5C {
			return 4
		}
		return 3
	case 0xD, 0xE:
		return 3
	case 0xF:
		return 4
	}
	// 1, 3, 5, 6, 7:
	return 2
}

// estimateUpdateCycles returns the most cycles the update routine code (run with A and X 8-bit) can
// take, but for the tables its ROM routines are called with (charged by updateData): updInstrCycles per instruction,
// each loop (a backward branch) updLoopIterations times, and each JSL or JML to a routine outside it callee's cycles
// (ok = false: updUnknownCallCycles).
func estimateUpdateCycles(code []byte, callee func(addr uint32) (int, bool)) (cycles int) {
	m8, x8 := true, true
	// the cycles of the code before each instruction, for the loops:
	before := make([]int, len(code)+1)
	for pc := 0; pc < len(code); {
		before[pc] = cycles
		op := code[pc]
		n := instrLen(op, m8, x8)
		if pc+n > len(code) {
			break
		}
		c := updInstrCycles
		switch op {
		case 0xC2: // REP
			if code[pc+1]&0x20 != 0 {
				m8 = false
			}
			if code[pc+1]&0x10 != 0 {
				x8 = false
			}
		case 0xE2: // SEP
			if code[pc+1]&0x20 != 0 {
				m8 = true
			}
			if code[pc+1]&0x10 != 0 {
				x8 = true
			}
		case 0x22, 0x5C: // JSL, JML
			addr := uint32(code[pc+1]) | uint32(code[pc+2])<<8 | uint32(code[pc+3])<<16
			if k, ok := callee(addr); ok {
				c += k
			} else {
				c += updUnknownCallCycles
			}
		case 0x20, 0xFC: // JSR
			c += updUnknownCallCycles
		case 0x10, 0x30, 0x50, 0x70, 0x80, 0x90, 0xB0, 0xD0, 0xF0: // branches
			if rel := int(int8(code[pc+1])); rel < 0 {
				if t := pc + 2 + rel; t >= 0 {
					c += (cycles + c - before[t]) * (updLoopIterations - 1)
				}
			}
		}
		cycles += c
		pc += n
	}
	return
}

// updCallee returns the cycles of the routine at addr an update routine calls: the game routines of updCalleeCycles,
// and the SRAM stubs (whose tables are charged by updateData).
func (g *Game) updCallee(addr uint32) (int, bool) {
	switch addr {
	case map16UpdateStubAddr, manipUpdateStubAddr, openChestsStubAddr, flyingTilesStubAddr:
		return 0, true
	}
	for fn, a := range g.romFunctions {
		if a != addr && a|0x800000 != addr {
			continue
		}
		if c, ok := updCalleeCycles[fn]; ok {
			return c, true
		}
	}
	return 0, false
}
