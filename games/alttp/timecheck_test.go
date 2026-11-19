package alttp

import (
	"bytes"
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
// the first of which lists linkOAMDyingMax dying enemies (in the last sprite slots looked at, so that the list looks
// at them all); if captures, then captured chrs in the VRAM slot range until linkOAMExtrasMax are copied (all there is
// room for); and the rest 16x16 chrs showing VRAM slots. A bomb exists, so that captured chrs are looked at.
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
		}
		if chr != 0x20 {
			copies++
		}
		copy(e.WRAM[0x0800+k*4:], []byte{x, y, chr, 0x20})
		e.WRAM[0x0A20+k] = ext
	}
	e.WRAM[0x0C4A] = 0x07
	// listed from the last sprite slot down; one is near the poofs:
	for x := 0; x < 16; x++ {
		sx, sy := scrX+0x10, scrY+0x10
		if x == 0 {
			sx, sy = scrX+0x80, scrY+0x80
		}
		e.WRAM[0x0DD0+x] = 0
		if x < linkOAMDyingMax {
			e.WRAM[0x0DD0+x] = 0x06
		}
		e.WRAM[0x0D10+x], e.WRAM[0x0D30+x] = uint8(sx), uint8(sx>>8)
		e.WRAM[0x0D00+x], e.WRAM[0x0D20+x] = uint8(sy), uint8(sy>>8)
		e.WRAM[0x0F70+x] = 0
	}
}

// fillDrawWorst fills the remote sprites table with n copies of its first slotted sprite (the costliest kind to draw;
// the other lists are emptied), and g1's OAM buffer with the game's sprites in all but the 32 lowest entries, so that
// each draw searches past them once.
func fillDrawWorst(h *skScenario, n int) {
	e := h.gs[0].e
	tbl := e.SRAM[remoteTableAddr&0x7FFF:]
	end := func(o int) uint16 { return binary.LittleEndian.Uint16(tbl[o:]) }
	ents := tbl[rtEntries:]
	if end(rtSlottedEnd) != uint16(n*rtEntrySize) || end(rtExtrasEnd) != 0 {
		slotted := make([]byte, rtEntrySize)
		copy(slotted, ents[end(rtExtrasEnd):])
		for j := 0; j < n; j++ {
			copy(ents[j*rtEntrySize:], slotted)
		}
		binary.LittleEndian.PutUint16(tbl[rtExtrasEnd:], 0)
		for _, o := range []int{rtSlottedEnd, rtShadowsEnd, rtBlobsEnd, rtFrontEnd} {
			binary.LittleEndian.PutUint16(tbl[o:], uint16(n*rtEntrySize))
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
		p.addCheckpoints(t, "rf_draw_time", "draw")
		p.addCheckpoints(t, "rf_dying_time", "dying")
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
	if n := binary.LittleEndian.Uint16(g1.e.SRAM[(linkOAMCopyAddr+linkOAMCopyDying)&0x7FFF:]); n != linkOAMDyingMax*4 {
		t.Errorf("listed %d bytes of dying enemies; expected %d", n, linkOAMDyingMax*4)
	}
	// drawing n sprites:
	for _, n := range []int{0, 1, 2, 4, 8, 16, 24, remoteEntryMax} {
		pd := newProfile()
		g1.beforeEmulate = func() {
			clearOAM()
			fillDrawWorst(h, n)
		}
		for f := 0; f < 2; f++ {
			h.profileFrame(pd, 0)
		}
		drawn := 0
		for k := 0; k < 0x20; k++ {
			if g1.e.WRAM[0x0801+k*4] != 0xF0 {
				drawn++
			}
		}
		if nl := binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen)); n > 0 && nl == 0 {
			t.Errorf("%d sprites: listed no rows to upload", n)
		}
		draw := pd.spanMax["draw"]
		budget := uint64(rfDrawBaseCycles + n*rfDrawEntryCycles)
		t.Logf("drawing %d sprites (drew %d): %d cycles (budget %d, late from line %d)", n, drawn, draw, budget,
			rfDrawLateLine(n))
		if draw > budget {
			t.Errorf("drawing %d sprites: %d cycles; budget rfDrawBaseCycles + n*rfDrawEntryCycles %d", n, draw, budget)
		}
	}

	// late checks: the exit after each kind:
	late := uint64(0)
	for _, label := range []string{"rf_scan_time_5f", "rf_draw_time"} {
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
	dying := p.spanMax["dying"] + late
	t.Logf("scan: %d + a late check's exit %d = %d cycles (budget %d, late from line %d); %d cycles per line, tail %d",
		p.spanMax["scan"], late, scan, rfScanCycles, rfScanLateLine, rfCyclesPerLine, rfTailCycles)
	t.Logf("listing the dying enemies: %d + a late check's exit %d = %d cycles (budget %d, late from line %d)",
		p.spanMax["dying"], late, dying, rfDyingCycles, rfDyingLateLine)
	if p.spanMax["dying"] == 0 {
		t.Errorf("the dying enemies' check not reached")
	}
	if scan > rfScanCycles {
		t.Errorf("scan: %d cycles; budget rfScanCycles %d", scan, rfScanCycles)
	}
	if dying > rfDyingCycles {
		t.Errorf("listing the dying enemies: %d cycles; budget rfDyingCycles %d", dying, rfDyingCycles)
	}
}

// timeCheck is a frame for timeCheckResult to run: the scanline it starts on, whether the scan's first check reads
// line 224 (and the drawing check line 0), whether the drawing check reads line 224, and whether an NMI is counted
// when remoteFrame starts (a lag frame).
type timeCheck struct {
	line                    uint8
	scanLate, drawLate, lag bool
}

// timeCheckResult runs a frame of the scenario as c says, and returns the remote sprites drawn, the captured entries'
// byte length, and whether the first player's body was drawn (rsBodyOK, which is only meaningful if anything was).
func timeCheckResult(t *testing.T, h *skScenario, labels *cycleProfile, c timeCheck) (drawn int, captured uint16, body bool) {
	t.Helper()
	g1 := h.gs[0]
	e := g1.e
	clearOAM := g1.beforeEmulate
	g1.beforeEmulate = func() {
		clearOAM()
		// a bomb and a captured chr, so that the scan runs and captures:
		e.WRAM[0x0C4A] = 0x07
		copy(e.WRAM[0x0800+0x10*4:], []byte{0x40, 0x40, 0x26, 0x20})
		setScanline(h, 0, c.line)
	}
	defer func() {
		g1.beforeEmulate = clearOAM
		setScanline(h, 0, 0)
		testStepHook = nil
	}()
	rfStart := labels.label(t, "rf_start")
	rfScanTime, rfDrawTime := labels.label(t, "rf_scan_time_7f"), labels.label(t, "rf_draw_time")
	testStepHook = func(e2 *emulator.System) {
		switch e2.GetPC() &^ 0x800000 {
		case rfStart:
			if c.lag {
				rtSRAM(e2, rtmNMIFrames)[0]++
			}
		case rfScanTime:
			if c.scanLate {
				setScanline(h, 0, rfNMILine-1)
			}
		case rfDrawTime:
			if c.drawLate {
				setScanline(h, 0, rfNMILine-1)
			} else if c.scanLate {
				setScanline(h, 0, 0)
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
	body = h.remoteState(0, rsBodyOK)[0] != 0
	return
}

func TestRemoteFrameTimeChecks(t *testing.T) {
	labels := newCycleProfile(t, "VT test")
	h := newRSCycleScenario(t)
	for _, c := range []struct {
		name string
		timeCheck
		draw, capture, body bool
	}{
		{"top of the frame", timeCheck{line: 0}, true, true, true},
		{"just before all checks' lines", timeCheck{line: min(rfDrawLateLine(remoteEntryMax), rfScanLateLine) - 1}, true, true, true},
		{"vblank", timeCheck{line: rfNMILine + 5}, true, true, true},
		{"line 256+ (reads as 0..5)", timeCheck{line: 3}, true, true, true},
		{"too late to scan and draw", timeCheck{line: rfScanLateLine}, false, false, false},
		{"too late to draw", timeCheck{drawLate: true}, false, true, false},
		{"line 224", timeCheck{line: rfNMILine - 1}, false, false, false},
		{"lagging", timeCheck{lag: true}, true, true, true},
		// a late scan reuses the VRAM slots the parts got rfReuseFrames times in a row, then draws the first player
		// as a blob:
		{"top of the frame again", timeCheck{line: 0}, true, true, true},
	} {
		drawn, captured, body := timeCheckResult(t, h, labels, c.timeCheck)
		if (drawn != 0) != c.draw {
			t.Errorf("%s (line %d): drew %d sprites; expected drawing: %v", c.name, c.line, drawn, c.draw)
		}
		if (captured != 0) != c.capture {
			t.Errorf("%s (line %d): captured %d bytes; expected capturing: %v", c.name, c.line, captured, c.capture)
		}
		if c.draw && body != c.body {
			t.Errorf("%s (line %d): body drawn %v; expected %v", c.name, c.line, body, c.body)
		}
		if !c.draw {
			if n := binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen)); n != 0 {
				t.Errorf("%s: listed %d bytes of rows to upload; expected none", c.name, n)
			}
		}
	}
	slots := append([]byte(nil), h.remoteState(0, rsPartSlot)[:remotePartMax]...)
	skip := h.remoteState(0, rsSkipSlots)[0]
	for f := 0; f <= rfReuseFrames; f++ {
		drawn, captured, body := timeCheckResult(t, h, labels, timeCheck{scanLate: true})
		reuse := f < rfReuseFrames
		if drawn == 0 || captured != 0 || body != reuse {
			t.Errorf("late scan %d: drew %d sprites, captured %d bytes, body drawn %v; expected drawing, no captures, "+
				"body drawn %v", f, drawn, captured, body, reuse)
		}
		nl := binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen))
		if reuse {
			if got := h.remoteState(0, rsPartSlot)[:remotePartMax]; !bytes.Equal(got, slots) {
				t.Errorf("late scan %d: part slots % x; expected the last ones, % x", f, got, slots)
			}
			if got := h.remoteState(0, rsSkipSlots)[0]; got != skip {
				t.Errorf("late scan %d: kept follower slots %x; expected the last ones, %x", f, got, skip)
			}
			if nl == 0 {
				t.Errorf("late scan %d: listed no rows to upload", f)
			}
		} else if nl != 0 {
			t.Errorf("late scan %d: listed %d bytes of rows to upload, without slots", f, nl)
		}
	}
	// a scan in time hands out the slots again:
	if _, _, body := timeCheckResult(t, h, labels, timeCheck{}); !body {
		t.Errorf("after the late scans: body not drawn")
	}
}
