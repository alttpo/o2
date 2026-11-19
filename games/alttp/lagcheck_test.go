package alttp

import (
	"encoding/binary"
	"testing"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
)

// instrLen agrees with the assembler on the lengths of the instructions update routines are made of, in both register
// sizes.
func TestLagCheck_InstrLen(t *testing.T) {
	a := asm.NewEmitter(make([]byte, 0x400), false)
	a.SetBase(0x707400)
	a.AssumeSEP(0x30)
	var lens []int
	emit := func(f func()) {
		n := a.Len()
		f()
		lens = append(lens, a.Len()-n)
	}
	both := func() {
		emit(func() { a.LDA_dp(0x10) })
		emit(func() { a.STA_dp(0x10) })
		emit(func() { a.LDA_abs(0x0DD0) })
		emit(func() { a.STA_abs_x(0x0DD0) })
		emit(func() { a.STZ_abs_x(0x0DD0) })
		emit(func() { a.LDA_abs_x(0x0DD0) })
		emit(func() { a.LDA_long(0x7EF340) })
		emit(func() { a.STA_long(0x7EF340) })
		emit(func() { a.LDA_long_x(0x7EF340) })
		emit(func() { a.CMP_long(0x7EF340) })
		emit(func() { a.INC_abs(0x0400) })
		emit(func() { a.INC_dp(0x15) })
		emit(func() { a.TAX() })
		emit(func() { a.DEX() })
		emit(func() { a.PHA() })
		emit(func() { a.PLA() })
		emit(func() { a.PHK() })
		emit(func() { a.PEA(0x1234) })
		emit(func() { a.JSL(0x1BC97C) })
		emit(func() { a.JML(0x1BC97C) })
		emit(func() { a.BEQ_imm8(0x02) })
		emit(func() { a.BNE_imm8(0x02) })
	}
	both()
	emit(func() { a.LDA_imm8_b(0x12) })
	emit(func() { a.LDX_imm8_b(0x12) })
	emit(func() { a.CMP_imm8_b(0x07) })
	emit(func() { a.AND_imm8_b(0x07) })
	emit(func() { a.ORA_imm8_b(0x07) })
	emit(func() { a.REP(0x30) })
	both()
	emit(func() { a.LDA_imm16_w(0x1234) })
	emit(func() { a.LDX_imm16_w(0x1234) })
	emit(func() { a.CMP_imm16_w(0x1234) })
	emit(func() { a.AND_imm16_w(0x1234) })
	emit(func() { a.SEP(0x20) })
	emit(func() { a.LDA_imm8_b(0x12) })
	emit(func() { a.LDX_imm16_w(0x1234) })
	emit(func() { a.SEP(0x10) })
	emit(func() { a.RTS() })
	if err := a.Finalize(); err != nil {
		t.Fatal(err)
	}

	code := a.Bytes()
	m8, x8 := true, true
	pc := 0
	for i, want := range lens {
		op := code[pc]
		if n := instrLen(op, m8, x8); n != want {
			t.Fatalf("instruction %d at %d: opcode $%02x: instrLen %d, assembled %d", i, pc, op, n, want)
		}
		switch op {
		case 0xC2:
			m8 = m8 && code[pc+1]&0x20 == 0
			x8 = x8 && code[pc+1]&0x10 == 0
		case 0xE2:
			m8 = m8 || code[pc+1]&0x20 != 0
			x8 = x8 || code[pc+1]&0x10 != 0
		}
		pc += want
	}

	// straight-line code costs updInstrCycles per instruction, a JSL also its callee:
	if c := estimateUpdateCycles(code, func(uint32) (int, bool) { return 1000, true }); c != len(lens)*updInstrCycles+4*1000 {
		t.Errorf("estimateUpdateCycles: %d, expected %d", c, len(lens)*updInstrCycles+4*1000)
	}
}

// A loop is charged updLoopIterations times.
func TestLagCheck_EstimateLoop(t *testing.T) {
	a := asm.NewEmitter(make([]byte, 0x40), false)
	a.SetBase(0x707400)
	a.AssumeSEP(0x30)
	a.LDX_imm8_b(0x0F)
	a.Label("loop")
	a.LDA_abs_x(0x0DD0)
	a.DEX()
	a.BPL("loop")
	a.RTS()
	if err := a.Finalize(); err != nil {
		t.Fatal(err)
	}
	expected := updInstrCycles * (1 + 3*updLoopIterations + 1)
	if c := estimateUpdateCycles(a.Bytes(), nil); c != expected {
		t.Errorf("estimateUpdateCycles: %d, expected %d", c, expected)
	}
}

// The update routine waits, still armed, while the frame has no time left for it after the module, and runs once it
// has.
func TestLagCheck_UpdateRoutineWaitsForTime(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.frames(skSettleFrames)
	h.expectKeys("initial", 0, 0)

	// g2's module ends late in every frame:
	g2 := h.gs[1]
	g2.frameLine = 222
	h.gs[0].e.WRAM[skCurrentKeys] = 1
	h.frames(skSettleFrames)
	h.expectKeys("g2 late", 1, 0)
	if g2.g.updateStage == 0 {
		t.Fatalf("g2: no update routine waiting")
	}
	target := g2.g.lastUpdateTarget - 0xE00000
	if op := g2.e.SRAM[target]; op == 0x60 {
		t.Fatalf("g2: the update routine was disabled without running")
	}
	late := g2.e.SRAM[target+updLateLineOffs]
	if late <= lagMinLateLine || late >= 222 {
		t.Errorf("g2: late line %d", late)
	}

	// time left again:
	g2.frameLine = 0
	h.frames(skSettleFrames)
	h.expectKeys("g2 in time", 1, 1)
}

// The pieces of preMain after the module stay within the budgets of their time checks (lagcheck.go).
func TestLagCheck_Budgets(t *testing.T) {
	p := newCycleProfile(t, "ZELDANODENSETSU")
	e, rom, err := createTestEmulator("ZELDANODENSETSU", nil)
	if err != nil {
		t.Fatal(err)
	}
	// RoomDraw_16x16Single returns with RTS, through the RTL before it:
	fns := make(map[romFunction]uint32)
	fillRomFunctions(rom, fns)
	for fn, op := range map[romFunction]byte{fnRoomDraw16x16Single: 0x60, fnBank01RTL: 0x6B} {
		pak, _ := lorom.BusAddressToPak(fns[fn])
		e.ROM[pak] = op
	}
	p.addSpan(t, "Link OAM copy", "linkOAMCopy_time", "linkOAMCopy_late")
	p.addSpan(t, "link color", "linkColor_time", "linkColorExit")
	p.addSpan(t, "sfx", "sfx_time", "sfxSkip")
	p.addSpan(t, "manip pending entry", "mp_time", "mp_done")

	sram := func(addr uint32) []byte { return e.SRAM[addr&0x7FFF:] }
	frame := func(setup func()) {
		t.Helper()
		e.WRAM[0x10], e.WRAM[0x11] = 0x07, 0x00
		e.WRAM[0xA0], e.WRAM[0xA1] = 0x12, 0x00
		e.SRAM[gatesAddr&0x7FFF] = gateSprites
		setup()
		setPPULine(e, 0)
		e.CPU.Reset()
		e.SetPC(testROMMainGameLoop)
		if !p.runUntil(e, testROMBreakPoint, 0x10_000) {
			t.Fatalf("frame did not finish; PC=$%06x", e.GetPC())
		}
	}
	for k := 0; k < 3; k++ {
		frame(func() {
			// a color to apply, unlike the palette's:
			binary.LittleEndian.PutUint16(sram(linkColorAddr), linkColorEnabled|0x1234+uint16(k))
			binary.LittleEndian.PutUint16(sram(linkColorAddr+2), 0x0567)
			// a local sound effect to append, and remote ones to play:
			e.WRAM[0x012E], e.WRAM[0x012F] = []uint8{0, 0x05, 0}[k], []uint8{0x20, 0, 0}[k]
			r := sram(sfxRemoteAddr)
			r[0], r[1] = 0x15, []uint8{0x16, 0, 0x16}[k]
			r[sfxRemoteQueued] = sram(linkOAMCopyAddr + linkOAMCopySfx + sfxLocalPlayed)[0] + 1
			// an object pending after the room load, still marked with its slot's attribute:
			binary.LittleEndian.PutUint16(sram(manipPendAddr), manipPendEntry)
			l := sram(manipPendList)
			binary.LittleEndian.PutUint16(l[0:], 0x0012)
			binary.LittleEndian.PutUint16(l[2:], 0)
			binary.LittleEndian.PutUint16(l[4:], manipPendRevealed)
			binary.LittleEndian.PutUint16(l[6:], 0x0100)
			e.WRAM[0x1_2000+0x80] = uint8(manipAttrBase)
			e.WRAM[0x1000], e.WRAM[0x1001] = 0, 0
		})
	}

	for _, s := range []struct {
		name   string
		budget int
	}{
		{"Link OAM copy", linkOAMCopyCycles},
		{"link color", linkColorCycles},
		{"sfx", sfxCycles},
		// RoomDraw_16x16Single is an RTL in the test ROM:
		{"manip pending entry", manipPendingEntryCycles - updRoomDrawCycles},
	} {
		n, ok := p.spanMax[s.name]
		if !ok || n == 0 {
			t.Errorf("%s: not run", s.name)
			continue
		}
		t.Logf("%s: %d cycles from its time check; budget %d", s.name, n, s.budget)
		if int(n) > s.budget {
			t.Errorf("%s: %d cycles; budget %d", s.name, n, s.budget)
		}
	}
}
