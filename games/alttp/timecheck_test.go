package alttp

import (
	"encoding/binary"
	"testing"

	"github.com/alttpo/snes/emulator"
)

// Time checks (see emitTimeCheck): remoteFrame skips the scan or drawing when the scanline says it would end after
// the NMI, or when an NMI came during the frame.

// setScanline makes the test emulator's V counter read as line (its IO registers are a plain register file).
func setScanline(h *skScenario, i int, line uint8) {
	h.gs[i].e.Bus.EaWrite(0x00213D, line)
}

// fillScanWorst fills g1's OAM buffer for the scan's worst cases between two time checks: every entry used; at the
// top of each block of rfScanBlock entries (the first scanned of it), a death poof chr that is also a captured chr,
// near the last of the 16 dying enemies listed (so its check looks at them all, and it isn't copied); if captures,
// then 16 captured chrs in the VRAM slot range, which are copied (all there is room for); and the rest 16x16 chrs
// showing VRAM slots. A bomb exists, so that captured chrs are looked at.
func fillScanWorst(h *skScenario, captures bool) {
	e := h.gs[0].e
	scrX := binary.LittleEndian.Uint16(e.WRAM[0xE2:])
	scrY := binary.LittleEndian.Uint16(e.WRAM[0xE8:])
	// Link's own entries are copied before and skipped by the scan; keep them out of the way, at the bottom:
	e.WRAM[0x0352], e.WRAM[0x0353] = 0x00, 0x00
	copies := 0
	for k := 0x7F; k >= 0; k-- {
		chr, x, y, ext := uint8(0x20), uint8(k), uint8(0x40), uint8(0x02)
		switch {
		case k%rfScanBlock == rfScanBlock-1:
			chr, x, y, ext = 0xA9, 0x80, 0x80, 0x00
		case captures && copies < linkOAMExtrasMax:
			chr = 0x26
			copies++
		}
		copy(e.WRAM[0x0800+k*4:], []byte{x, y, chr, 0x20})
		e.WRAM[0x0A20+k] = ext
	}
	e.WRAM[0x0C4A] = 0x07
	// listed from the last sprite slot down, so enemy 0 is the last; the others are far from the poofs:
	for x := 0; x < 16; x++ {
		sx, sy := scrX+0x10, scrY+0x10
		if x == 0 {
			sx, sy = scrX+0x80, scrY+0x80
		}
		e.WRAM[0x0DD0+x] = 0x06
		e.WRAM[0x0D10+x], e.WRAM[0x0D30+x] = uint8(sx), uint8(sx>>8)
		e.WRAM[0x0D00+x], e.WRAM[0x0D20+x] = uint8(sy), uint8(sy>>8)
		e.WRAM[0x0F70+x] = 0
	}
}

// fillDrawWorst fills the remote sprites table to remoteEntryMax entries, adding copies of its first blob to the extra
// sprites behind Link, and g1's OAM buffer with the game's sprites in all but the 32 lowest entries, so that each
// draw searches past them once.
func fillDrawWorst(h *skScenario) {
	e := h.gs[0].e
	tbl := e.SRAM[remoteTableAddr&0x7FFF:]
	end := func(o int) uint16 { return binary.LittleEndian.Uint16(tbl[o:]) }
	n := int(end(rtFrontEnd)) / rtEntrySize
	if add := remoteEntryMax - n; add > 0 {
		ents := tbl[rtEntries:]
		blob := make([]byte, rtEntrySize)
		copy(blob, ents[end(rtShadowsEnd):])
		copy(ents[add*rtEntrySize:], ents[:n*rtEntrySize])
		for j := 0; j < add; j++ {
			copy(ents[j*rtEntrySize:], blob)
		}
		for _, o := range []int{rtExtrasEnd, rtSlottedEnd, rtShadowsEnd, rtBlobsEnd, rtFrontEnd} {
			binary.LittleEndian.PutUint16(tbl[o:], end(o)+uint16(add*rtEntrySize))
		}
	}
	for k := 0x20; k < 0x80; k++ {
		copy(e.WRAM[0x0800+k*4:], []byte{uint8(k), 0x40, 0xC0, 0x20})
	}
}

// The budgets the time checks' scanlines are computed from cover the worst cases of the work from each check to the
// next one, or to the end of the frame.
func TestCycleBudgets_RemoteFrameTimeChecks(t *testing.T) {
	newProfile := func() *cycleProfile {
		p := newCycleProfile(t, "VT test")
		p.addCheckpoints(t, "rf_scan_time_", "scan")
		p.addCheckpoints(t, "rf_fx_time", "scan")
		p.addCheckpoints(t, "rf_draw_time", "draw")
		return p
	}
	p := newProfile()
	h := newRSCycleScenario(t)
	g1 := h.gs[0]
	clearOAM := g1.beforeEmulate
	defer func() { g1.beforeEmulate = clearOAM }()

	// the scan, with and without captures:
	for _, captures := range []bool{true, false} {
		g1.beforeEmulate = func() {
			clearOAM()
			fillScanWorst(h, captures)
		}
		for f := 0; f < 2; f++ {
			h.profileFrame(p, 0)
		}
	}
	if n := g1.e.SRAM[captureDyingAddr&0x7FFF]; n != 16*4 {
		t.Errorf("listed %d bytes of dying enemies; expected %d", n, 16*4)
	}
	// drawing:
	g1.beforeEmulate = func() {
		clearOAM()
		fillDrawWorst(h)
	}
	for f := 0; f < 2; f++ {
		h.profileFrame(p, 0)
	}
	drawn := 0
	for k := 0; k < 0x20; k++ {
		if g1.e.WRAM[0x0801+k*4] != 0xF0 {
			drawn++
		}
	}
	t.Logf("drew %d sprites", drawn)
	if n := binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen)); n == 0 {
		t.Errorf("listed no rows to upload")
	}

	// late checks: the exit after each kind (when the poof's check is late, and when drawing's is):
	late := uint64(0)
	for _, label := range []string{"rf_scan_time_5f", "rf_fx_time", "rf_draw_time"} {
		pl := newCycleProfile(t, "VT test")
		pl.addCheckpoints(t, label, "late")
		at := pl.label(t, label)
		pl.onPC[at] = func() { setScanline(h, 0, rfNMILine-1) }
		g1.beforeEmulate = func() {
			clearOAM()
			fillScanWorst(h, false)
			setScanline(h, 0, 0)
		}
		h.profileFrame(pl, 0)
		if pl.spanMax["late"] == 0 {
			t.Errorf("%s: not reached", label)
		}
		t.Logf("late at %s: %d cycles to the end", label, pl.spanMax["late"])
		late = max(late, pl.spanMax["late"])
	}
	setScanline(h, 0, 0)

	scan := p.spanMax["scan"] + late
	draw := p.spanMax["draw"]
	t.Logf("scan: %d + a late check's exit %d = %d cycles (budget %d, late from line %d)",
		p.spanMax["scan"], late, scan, rfScanCycles, rfScanLateLine)
	t.Logf("drawing: %d cycles (budget %d, late from line %d); %d cycles per line, tail %d",
		draw, rfDrawCycles, rfDrawLateLine, rfCyclesPerLine, rfTailCycles)
	if scan > rfScanCycles {
		t.Errorf("scan: %d cycles; budget rfScanCycles %d", scan, rfScanCycles)
	}
	if draw > rfDrawCycles {
		t.Errorf("drawing: %d cycles; budget rfDrawCycles %d", draw, rfDrawCycles)
	}
}

// timeCheckResult runs a frame of the scenario with the scanline set to line, then to 224 from the drawing check on
// if drawLate, and, if lag, an NMI counted when remoteFrame starts; and returns the remote sprites drawn and the
// captured entries' byte length.
func timeCheckResult(t *testing.T, h *skScenario, labels *cycleProfile, line uint8, drawLate, lag bool) (drawn int, captured uint16) {
	t.Helper()
	g1 := h.gs[0]
	e := g1.e
	clearOAM := g1.beforeEmulate
	g1.beforeEmulate = func() {
		clearOAM()
		// a bomb and a captured chr, so that the scan runs and captures:
		e.WRAM[0x0C4A] = 0x07
		copy(e.WRAM[0x0800+0x10*4:], []byte{0x40, 0x40, 0x26, 0x20})
		setScanline(h, 0, line)
	}
	defer func() {
		g1.beforeEmulate = clearOAM
		setScanline(h, 0, 0)
		testStepHook = nil
	}()
	rfStart, rfDrawTime := labels.label(t, "rf_start"), labels.label(t, "rf_draw_time")
	testStepHook = func(e2 *emulator.System) {
		switch e2.GetPC() &^ 0x800000 {
		case rfStart:
			if lag {
				rtSRAM(e2, rtmNMIFrames)[0]++
			}
		case rfDrawTime:
			if drawLate {
				setScanline(h, 0, rfNMILine-1)
			}
		}
	}
	h.frame()
	for k := 0; k < 0x80; k++ {
		if k != 0x10 && e.WRAM[0x0801+k*4] != 0xF0 {
			drawn++
		}
	}
	captured = binary.LittleEndian.Uint16(e.SRAM[(linkOAMCopyAddr+linkOAMCopyExtras)&0x7FFF:])
	return
}

func TestRemoteFrameTimeChecks(t *testing.T) {
	labels := newCycleProfile(t, "VT test")
	h := newRSCycleScenario(t)
	for _, c := range []struct {
		name          string
		line          uint8
		drawLate, lag bool
		draw, capture bool
	}{
		{"top of the frame", 0, false, false, true, true},
		{"just before both checks' lines", min(rfDrawLateLine, rfScanLateLine) - 1, false, false, true, true},
		{"vblank", rfNMILine + 5, false, false, true, true},
		{"line 256+ (reads as 0..5)", 3, false, false, true, true},
		{"too late to scan", rfScanLateLine, false, false, false, false},
		{"too late to draw", 0, true, false, false, true},
		{"line 224", rfNMILine - 1, false, false, false, false},
		{"lagging", 0, false, true, false, false},
	} {
		drawn, captured := timeCheckResult(t, h, labels, c.line, c.drawLate, c.lag)
		if (drawn != 0) != c.draw {
			t.Errorf("%s (line %d): drew %d sprites; expected drawing: %v", c.name, c.line, drawn, c.draw)
		}
		if (captured != 0) != c.capture {
			t.Errorf("%s (line %d): captured %d bytes; expected capturing: %v", c.name, c.line, captured, c.capture)
		}
		if !c.draw {
			if n := binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen)); n != 0 {
				t.Errorf("%s: listed %d bytes of rows to upload; expected none", c.name, n)
			}
		}
	}
}
