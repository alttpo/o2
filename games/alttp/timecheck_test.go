package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/alttpo/snes/emulator"
)

// Time checks (see emitTimeCheck): remoteFrame skips the scan or drawing when the scanline says it would end after
// the NMI, or when an NMI came during the frame.

// setScanline puts gi's PPU at the start of line, from where it counts on as the CPU runs.
func setScanline(h *skScenario, i int, line int) {
	setPPULine(h.gs[i].e, line)
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

// fillCopyBlock fills g1's OAM buffer for the costliest block of the scan without a death chr: its first block copies
// linkOAMExtrasMax captured chrs in the VRAM slot range (all there is room for), and the rest are 16x16 chrs showing
// VRAM slots. A bomb exists, so that captured chrs are looked at.
func fillCopyBlock(h *skScenario) {
	e := h.gs[0].e
	e.WRAM[0x0352], e.WRAM[0x0353] = 0x00, 0x00
	for k := 0x7F; k >= 0; k-- {
		chr := uint8(0x20)
		if k > 0x7F-linkOAMExtrasMax {
			chr = 0x26
		}
		copy(e.WRAM[0x0800+k*4:], []byte{uint8(k), 0x40, chr, 0x20})
		e.WRAM[0x0A20+k] = 0x02
	}
	e.WRAM[0x0C4A] = 0x07
	for x := 0; x < 16; x++ {
		e.WRAM[0x0DD0+x] = 0
	}
}

// drawMix is what fillDrawMix puts in the remote sprites table: the sprites of each list (the extras all in front of
// Link) and the number of parts the slotted sprites spread over.
type drawMix struct {
	extras, slotted, parts, shadows, blobs int
}

// drawn returns how many of the mix's sprites are drawn: all but the first shadow and blob if the slotted sprites are.
func (m drawMix) drawn() int {
	n := m.extras + m.slotted + m.shadows + m.blobs
	if m.slotted > 0 {
		n -= min(m.shadows, 1) + min(m.blobs, 1)
	}
	return n
}

func (m drawMix) String() string {
	return fmt.Sprintf("%d extras, %d slotted in %d parts, %d shadows, %d blobs", m.extras, m.slotted, m.parts,
		m.shadows, m.blobs)
}

// fillDrawMix fills the remote sprites table with the mix's sprites, all on g1's screen, with its late line as o2
// computes it, and g1's OAM buffer with the game's sprites in all but the 32 lowest entries, so that drawing searches
// past them once. The slotted sprites take the parts in turn, so that every part is drawn and its rows listed.
func fillDrawMix(h *skScenario, m drawMix) {
	e := h.gs[0].e
	var tb [remoteTableSize]byte
	copy(tb[:], e.SRAM[remoteTableAddr&0x7FFF:])
	o := rtEntries
	ent := func(i int, chr, attr uint8) {
		binary.LittleEndian.PutUint16(tb[o:], 0x1030+uint16(i%8)*16)
		binary.LittleEndian.PutUint16(tb[o+2:], 0x0230+uint16(i/8)*16)
		tb[o+4], tb[o+5], tb[o+6] = chr, attr, entrySize16
		o += rtEntrySize
	}
	end := func(f int) { binary.LittleEndian.PutUint16(tb[f:], uint16(o-rtEntries)) }
	end(rtExtrasEnd)
	for i := 0; i < m.slotted; i++ {
		ent(i, uint8(i%max(m.parts, 1))<<6, 0x20)
	}
	end(rtSlottedEnd)
	for i := 0; i < m.shadows; i++ {
		ent(i, remoteShadowChr, remoteBlobAttr)
	}
	end(rtShadowsEnd)
	for i := 0; i < m.blobs; i++ {
		ent(i, remoteBlobChr0, remoteBlobAttr)
	}
	end(rtBlobsEnd)
	for i := 0; i < m.extras; i++ {
		ent(i, 0x26, 0x22)
	}
	end(rtFrontEnd)
	tb[rtParts] = uint8(m.parts)
	tb[rtRequired] = uint8(min(m.parts, 2))
	for p := 0; p < remotePartMax; p++ {
		binary.LittleEndian.PutUint16(tb[rtPartSrc+p*4:], 0x8000+uint16(p)*0x40)
		binary.LittleEndian.PutUint16(tb[rtPartSrc+p*4+2:], 0x8200+uint16(p)*0x40)
		tb[rtPartFlags+p] = partTopRow | partBottomRow
		if p >= 2 {
			tb[rtPartFlags+p] |= partWRAM
		}
	}
	tb[rtDrawLate] = drawLateLineOf(&tb)
	copy(e.SRAM[remoteTableAddr&0x7FFF:], tb[:])
	for k := 0x20; k < 0x80; k++ {
		copy(e.WRAM[0x0800+k*4:], []byte{uint8(k), 0x40, 0xC0, 0x20})
	}
}

// fillDrawWorst is fillDrawMix with n slotted sprites in every part, the costliest kind to draw.
func fillDrawWorst(h *skScenario, n int) {
	fillDrawMix(h, drawMix{slotted: n, parts: remotePartMax})
}

// drawMixes are the tables the drawing budget is checked with: each kind alone, from none to a full table, and the
// first player with Link's sprites and the rest as blobs, or with every extra sprite:
var drawMixes = func() (l []drawMix) {
	l = append(l, drawMix{})
	for _, n := range []int{1, 2, 8, 16, remoteEntryMax} {
		l = append(l, drawMix{extras: n})
	}
	for _, n := range []int{1, 2, 8, remoteEntryMax / 2} {
		l = append(l, drawMix{shadows: n, blobs: n})
	}
	for parts := 1; parts <= remotePartMax; parts++ {
		for _, n := range []int{parts, 8, remoteEntryMax} {
			l = append(l, drawMix{slotted: n, parts: parts})
		}
	}
	for _, n := range []int{1, 4, (remoteEntryMax - remotePartMax) / 2} {
		l = append(l, drawMix{slotted: 4, parts: 4, shadows: n, blobs: n})
	}
	l = append(l, drawMix{extras: remoteExtraMax, slotted: 4, parts: 4, shadows: 1, blobs: 1})
	return
}()

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

	// the scan, with and without captures, and a block copying all it can:
	for _, fill := range []func(){
		func() { fillScanWorst(h, true) },
		func() { fillScanWorst(h, false) },
		func() { fillCopyBlock(h) },
	} {
		g1.beforeEmulate = func() {
			clearOAM()
			fill()
		}
		for f := 0; f < 2; f++ {
			h.profileFrame(p, 0)
		}
	}
	if n := binary.LittleEndian.Uint16(g1.e.SRAM[(linkOAMCopyAddr+linkOAMCopyDying)&0x7FFF:]); n != linkOAMDyingMax*4 {
		t.Errorf("listed %d bytes of dying enemies; expected %d", n, linkOAMDyingMax*4)
	}
	// drawing each mix:
	for _, m := range drawMixes {
		pd := newProfile()
		g1.beforeEmulate = func() {
			clearOAM()
			fillDrawMix(h, m)
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
		if drawn != m.drawn() {
			t.Errorf("%v: drew %d; expected %d", m, drawn, m.drawn())
		}
		if nl := binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen)); m.slotted > 0 && nl == 0 {
			t.Errorf("%v: listed no rows to upload", m)
		}
		draw := pd.spanMax["draw"]
		budget := uint64(rfDrawCycles(m.extras, m.slotted, m.shadows, m.blobs, m.parts))
		t.Logf("drawing %v (drew %d): %d cycles (budget %d, late from line %d)", m, drawn, draw, budget,
			rfDrawLateLine(int(budget)))
		if draw > budget {
			t.Errorf("drawing %v: %d cycles; budget rfDrawCycles %d", m, draw, budget)
		}

		// then a frame too late to draw that replays them:
		pr := newCycleProfile(t, "VT test")
		pr.addCheckpoints(t, "rf_replay_time", "replay")
		pr.onPC[pr.label(t, "rf_draw_time")] = func() { setScanline(h, 0, rfNMILine-1) }
		pr.onPC[pr.label(t, "rf_replay_time")] = func() { setScanline(h, 0, 0) }
		h.profileFrame(pr, 0)
		replayed := 0
		for k := 0; k < 0x20; k++ {
			if g1.e.WRAM[0x0801+k*4] != 0xF0 {
				replayed++
			}
		}
		if replayed != drawn {
			t.Errorf("%v: replayed %d; expected the %d drawn", m, replayed, drawn)
		}
		replay := pr.spanMax["replay"]
		budget = uint64(rfReplayBaseCycles + drawn*rfReplayEntryCycles)
		t.Logf("replaying %d sprites: %d cycles (budget %d, late from line %d)", replayed, replay, budget,
			rfReplayLateLine(drawn))
		if replay > budget {
			t.Errorf("replaying %d sprites: %d cycles; budget rfReplayBaseCycles + n*rfReplayEntryCycles %d", drawn,
				replay, budget)
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

// timeCheck is a frame for timeCheckResult to run: the scanline remoteFrame starts on (the PPU counts on from there),
// whether the scan's first check starts on line 224 (and the drawing check on line 0), whether the drawing check
// starts on line 224, whether the replay's check then starts on line 0 (else it is late too), whether an NMI is counted
// when remoteFrame starts (a lag frame), and how far g1's screen is scrolled right from where it is.
type timeCheck struct {
	line                            int
	scanLate, drawLate, replay, lag bool
	scrollX                         uint16
	// setup, if set, is called before the frame, after the OAM buffer is cleared:
	setup func()
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
		binary.LittleEndian.PutUint16(e.WRAM[0xE2:], binary.LittleEndian.Uint16(e.WRAM[0xE2:])+c.scrollX)
		if c.setup != nil {
			c.setup()
		}
	}
	defer func() {
		g1.beforeEmulate = clearOAM
		setScanline(h, 0, 0)
		testStepHook = nil
	}()
	rfStart := labels.label(t, "rf_start")
	rfScanTime, rfDrawTime := labels.label(t, "rf_scan_time_7f"), labels.label(t, "rf_draw_time")
	rfReplayTime := labels.label(t, "rf_replay_time")
	testStepHook = func(e2 *emulator.System) {
		if e2 != e {
			return
		}
		switch e2.GetPC() &^ 0x800000 {
		case rfReplayTime:
			if c.replay {
				setScanline(h, 0, 0)
			}
		case rfStart:
			setScanline(h, 0, c.line)
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
		// the drawing check too late, the replay's in time: the frame just drawn again:
		{"too late to draw, in time to replay", timeCheck{drawLate: true, replay: true}, true, true, true},
		{"just before all checks' lines", timeCheck{line: int(min(rfDrawLateLine(rfDrawCycles(0, remoteEntryMax, 0, 0, remotePartMax)), rfScanLateLine)) - 1}, true, true, true},
		{"vblank", timeCheck{line: rfNMILine + 5}, true, true, true},
		{"line 259 (reads as 3)", timeCheck{line: 259}, true, true, true},
		// started past the drawing check's line: no capture nor scan, the last frame drawn replayed:
		{"started too late to scan and draw", timeCheck{line: rfScanLateLine}, true, false, true},
		// started too late even to replay:
		{"started too late to replay", timeCheck{line: rfNMILine - 10}, false, false, false},
		{"too late to draw", timeCheck{drawLate: true}, false, true, false},
		// remoteFrame takes a few lines to reach its first check, so starting it on line 224 checks in vblank; put the
		// checks themselves on line 224:
		{"checks on line 224", timeCheck{scanLate: true, drawLate: true}, false, false, false},
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

// clocksLeft returns the CPU's master clocks left before the NMI on the PPU's counters: those left on this line, and
// on each visible line after it what refresh and HDMA leave; or -1 if the NMI came already (a frame after start, or in
// vblank).
func clocksLeft(ppu *emulator.PPU, startFrame uint64) int {
	if ppu.Frame != startFrame || ppu.V >= rfNMILine {
		return -1
	}
	return 1364 - ppu.HClock + (rfNMILine-1-ppu.V)*(1364-40-rfHDMAClocks)
}

// remoteFrame may only make a frame late by its unavoidable least: whatever line it starts on before vblank, with the
// worst cases of the scan and of drawing, the game's tail after it (rfTailCycles, at 8 master clocks per cycle) still
// fits before the NMI on the PPU's counters (refresh and HDMA taking theirs), unless there wasn't room for the least
// remoteFrame takes (its start, and the late checks skipping all its work) to begin with.
func TestRemoteFrameTimeChecks_FinishBeforeNMI(t *testing.T) {
	labels := newCycleProfile(t, "VT test")
	h := newRSCycleScenario(t)
	g1 := h.gs[0]
	e := g1.e
	ppu := testPPU(e)
	rfStart := labels.label(t, "rf_start")
	clearOAM := g1.beforeEmulate
	defer func() {
		g1.beforeEmulate = clearOAM
		testStepHook = nil
		setScanline(h, 0, 0)
	}()
	for _, c := range []struct {
		name string
		fill func()
	}{
		{"the scan's worst case", func() { fillScanWorst(h, true) }},
		{"the scan's worst case without captures", func() { fillScanWorst(h, false) }},
		{"a block copying all it can", func() { fillCopyBlock(h) }},
		{"drawing 8 slotted sprites", func() { fillDrawWorst(h, 8) }},
		{fmt.Sprintf("drawing %d slotted sprites", remoteEntryMax), func() { fillDrawWorst(h, remoteEntryMax) }},
		{fmt.Sprintf("drawing %d blob players", remoteEntryMax/2), func() {
			fillDrawMix(h, drawMix{shadows: remoteEntryMax / 2, blobs: remoteEntryMax / 2})
		}},
		{"drawing Link and every extra sprite", func() {
			fillDrawMix(h, drawMix{extras: remoteExtraMax, slotted: 4, parts: 4, shadows: 1, blobs: 1})
		}},
	} {
		g1.beforeEmulate = func() {
			clearOAM()
			c.fill()
			// over this many frames the table would go stale; keep it current, and capturing on:
			e.SRAM[(remoteTableAddr+rtTTL)&0x7FFF] = 60
			e.SRAM[(remoteTableAddr+rtCaptureTTL)&0x7FFF] = 60
		}
		// per start line: the clocks left for the tail when remoteFrame started and when the frame ended:
		var before, after [rfNMILine]int
		for line := 0; line < rfNMILine; line++ {
			var startFrame uint64
			testStepHook = func(e2 *emulator.System) {
				if e2 == e && e2.GetPC()&^0x800000 == rfStart {
					setScanline(h, 0, line)
					startFrame = ppu.Frame
					before[line] = clocksLeft(ppu, startFrame) - rfTailCycles*8
				}
			}
			h.frame()
			after[line] = clocksLeft(ppu, startFrame) - rfTailCycles*8
		}
		// what remoteFrame took, in the frames that ended before the NMI (whose clocks are all counted):
		least, most := math.MaxInt, 0
		for line := range before {
			if after[line] >= -rfTailCycles*8 {
				least, most = min(least, before[line]-after[line]), max(most, before[line]-after[line])
			}
		}
		spare, spareLine := math.MaxInt, 0
		for line := range before {
			if before[line] < least {
				continue
			}
			if after[line] < spare {
				spare, spareLine = after[line], line
			}
			if after[line] < 0 {
				t.Errorf("%s from line %d: %d clocks left for the tail at the start, %d at the end", c.name, line,
					before[line], after[line])
			}
		}
		t.Logf("%s: remoteFrame took %d..%d clocks; least to spare for the tail %d, from line %d", c.name, least, most,
			spare, spareLine)
	}
}

// remoteOAM returns g1's used OAM entries but entry $10 (the captured chr timeCheckResult puts there), with their
// extended attributes, as a multiset.
func remoteOAM(h *skScenario) map[[5]byte]int {
	e := h.gs[0].e
	m := map[[5]byte]int{}
	for k := 0; k < 0x80; k++ {
		if k == 0x10 || e.WRAM[0x0801+k*4] == 0xF0 {
			continue
		}
		var v [5]byte
		copy(v[:4], e.WRAM[0x0800+k*4:])
		v[4] = e.WRAM[0x0A20+k]
		m[v]++
	}
	return m
}

func equalOAM(a, b map[[5]byte]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// A drawing check too late to draw replays the last frame drawn: the same OAM entries and rows to upload, for up to
// rfReuseFrames frames in a row, and only with the scroll offsets it was drawn with. The counters say what happened.
func TestRemoteFrameReplay(t *testing.T) {
	labels := newCycleProfile(t, "VT test")
	h := newRSCycleScenario(t)
	e := h.gs[0].e
	rp := func(o uint32) uint16 {
		return binary.LittleEndian.Uint16(e.SRAM[(remoteReplayAddr+o)&0x7FFF:])
	}
	nmiLen := func() uint16 { return binary.LittleEndian.Uint16(h.remoteState(0, rsNMILen)) }

	if drawn, _, _ := timeCheckResult(t, h, labels, timeCheck{line: 17}); drawn == 0 {
		t.Fatalf("drew nothing in time")
	}
	if got := rp(rpStartLine); got != 17 {
		t.Errorf("rpStartLine = %d; expected 17", got)
	}
	want, wantLen := remoteOAM(h), nmiLen()
	lateDraws, replays := rp(rpLateDraws), rp(rpReplays)
	for f := 0; f <= rfReuseFrames; f++ {
		drawn, _, _ := timeCheckResult(t, h, labels, timeCheck{line: 40, drawLate: true, replay: true})
		got := remoteOAM(h)
		if f < rfReuseFrames {
			if !equalOAM(got, want) {
				t.Errorf("replay %d: OAM entries %v; expected the last drawn, %v", f, got, want)
			}
			if n := nmiLen(); n != wantLen {
				t.Errorf("replay %d: listed %d bytes of rows to upload; expected the last drawn's %d", f, n, wantLen)
			}
		} else if drawn != 0 || nmiLen() != 0 {
			t.Errorf("replay %d: drew %d sprites and listed %d bytes of rows; expected nothing after %d replays", f,
				drawn, nmiLen(), rfReuseFrames)
		}
	}
	if got := rp(rpLateDraws) - lateDraws; got != rfReuseFrames+1 {
		t.Errorf("rpLateDraws counted %d; expected %d", got, rfReuseFrames+1)
	}
	if got := rp(rpReplays) - replays; got != rfReuseFrames {
		t.Errorf("rpReplays counted %d; expected %d", got, rfReuseFrames)
	}
	if got := rp(rpLateDrawLine); got != 40 {
		t.Errorf("rpLateDrawLine = %d; expected 40", got)
	}

	// no time even for the replay: nothing:
	timeCheckResult(t, h, labels, timeCheck{})
	if drawn, _, _ := timeCheckResult(t, h, labels, timeCheck{drawLate: true}); drawn != 0 || nmiLen() != 0 {
		t.Errorf("too late to replay: drew %d sprites and listed %d bytes of rows; expected nothing", drawn, nmiLen())
	}
	// nor once the recording can't be replayed any more, though in time for it:
	if drawn, _, _ := timeCheckResult(t, h, labels, timeCheck{drawLate: true, replay: true}); drawn != 0 {
		t.Errorf("after a frame too late to replay: drew %d sprites; expected nothing", drawn)
	}

	// an OAM entry it was drawn into is the game's this frame: that sprite is left out, the rest replayed into theirs:
	timeCheckResult(t, h, labels, timeCheck{})
	want = remoteOAM(h)
	taken := int(e.SRAM[(remoteReplayAddr+rpEntries+5)&0x7FFF])
	game := [5]byte{0x10, 0x20, 0xC0, 0x20, 0x00}
	occupy := func() {
		copy(e.WRAM[0x0800+taken*4:], game[:4])
		e.WRAM[0x0A20+taken] = game[4]
	}
	timeCheckResult(t, h, labels, timeCheck{drawLate: true, replay: true, setup: occupy})
	got := remoteOAM(h)
	if got[game] != 1 {
		t.Errorf("game's sprite in OAM entry $%02x overwritten", taken)
	}
	delete(got, game)
	for k, v := range got {
		if want[k] < v {
			t.Errorf("replayed %v %d times; drawn %d times", k, v, want[k])
		}
	}
	if n, m := len(remoteOAMEntries(h)), countOAM(want); n != m {
		t.Errorf("replayed with entry $%02x taken: %d entries used; expected %d (%d drawn, less the one left out, plus "+
			"the game's)", taken, n, m, m)
	}

	// the screen scrolled since the frame drawn: nothing:
	timeCheckResult(t, h, labels, timeCheck{})
	if drawn, _, _ := timeCheckResult(t, h, labels, timeCheck{drawLate: true, replay: true, scrollX: 1}); drawn != 0 {
		t.Errorf("scrolled: replayed %d sprites; expected nothing", drawn)
	}
}

// remoteOAMEntries returns the indices of g1's used OAM entries but entry $10.
func remoteOAMEntries(h *skScenario) (l []int) {
	e := h.gs[0].e
	for k := 0; k < 0x80; k++ {
		if k != 0x10 && e.WRAM[0x0801+k*4] != 0xF0 {
			l = append(l, k)
		}
	}
	return
}

func countOAM(m map[[5]byte]int) (n int) {
	for _, v := range m {
		n += v
	}
	return
}
