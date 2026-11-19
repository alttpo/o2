package alttp

// How many remote players remoteFrame can draw in a frame without the frame running late, measured in real gameplay
// on the o2-patched JP ROM: in a trivial underworld room (Link's house, $104) and overworld area (outside it, $2C),
// with Link standing still. Each frame the test writes a remote sprites table of n players before the frame runs, and
// records where the frame ends (the line of `STZ $12`; the NMI comes on line 225), what remoteFrame costs from its
// start to its return, and whether its time checks skipped any work.
//
//	REMOTE_PLAYERS=1 go test -run TestRemoteFramePlayers -v

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alttpo/snes/emulator"
	"o2/snes"
	"o2/util"
)

// newPatchedRealGame is newRealGame with the JP ROM patched by o2 (with the sprites gate on) and the PPU's HDMA
// overhead per line set to what the time checks assume; also returns the patch listing's labels.
func newPatchedRealGame(t *testing.T) (*realGame, *cycleProfile) {
	romData, err := os.ReadFile(jpROMPath)
	if err != nil {
		t.Skipf("no JP ROM: %v", err)
	}
	rom, err := snes.NewROM("alttp-jp.sfc", append([]byte(nil), romData...))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	util.LogPath = filepath.Join(dir, "o2.log")
	err = NewPatcher(rom).Patch()
	util.LogPath = ""
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "o2-patch.asm"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	prof := parseCycleProfile(t, f)

	g := &realGame{t: t, ppu: &emulator.PPU{HDMAClocks: rfHDMAClocks}, pad: &emulator.Joypads{}}
	g.e = &emulator.System{Devices: []emulator.Device{g.ppu, g.pad, &emulator.Math{}, &apuStub{}}}
	if err := g.e.CreateEmulator(); err != nil {
		t.Fatal(err)
	}
	copy(g.e.ROM[:], rom.Contents)
	if srm, err := os.ReadFile("../../../alttp-jp.srm"); err == nil {
		copy(g.e.SRAM[:0x2000], srm)
	}
	g.e.Reset()
	return g, prof
}

// rpLayout is what a remote sprites table holds: the first player drawn with Link's sprites (4 slotted sprites in 4
// parts), the players drawn as blobs (each a shadow and a blob; the first player's are in the table too, skipped when
// its slotted sprites are drawn), and extra sprites (shadows, sparkles, dust...).
type rpLayout struct {
	slotted bool
	players int
	extras  int
}

func (l rpLayout) entries() int {
	n := 2*l.players + l.extras
	if l.slotted {
		n += 4
	}
	return n
}

// writeRemoteTable writes a remote sprites table of the layout for the room or area the game is in, with its sprites
// on screen around Link.
func (g *realGame) writeRemoteTable(l rpLayout) {
	if l.entries() > remoteEntryMax {
		g.t.Fatalf("layout %+v: %d entries, more than %d", l, l.entries(), remoteEntryMax)
	}
	w := g.e.WRAM[:]
	var tb [remoteTableSize]byte
	tb[rtTTL], tb[rtCaptureTTL] = 0xFF, 0xFF
	tb[rtIndoors] = w[0x1B]
	if w[0x1B] != 0 {
		binary.LittleEndian.PutUint16(tb[rtLocation:], g.wram16(0xA0))
	} else {
		binary.LittleEndian.PutUint16(tb[rtLocation:], uint16(w[0x8A]))
	}
	scrX := g.wram16(0xE2) - g.wram16(0x011A)
	scrY := g.wram16(0xE8) - g.wram16(0x011C)
	o := rtEntries
	entry := func(sx, sy int, chr, attr, flags uint8) {
		binary.LittleEndian.PutUint16(tb[o:], scrX+uint16(sx))
		binary.LittleEndian.PutUint16(tb[o+2:], scrY+uint16(sy))
		tb[o+4], tb[o+5], tb[o+6] = chr, attr, flags
		o += rtEntrySize
	}
	end := func(field int) { binary.LittleEndian.PutUint16(tb[field:], uint16(o-rtEntries)) }
	px := func(i int) int { return 24 + (i%8)*26 }
	py := func(i int) int { return 40 + (i/8)*60 }

	for j := 0; j < l.extras; j++ {
		entry(px(j%max(l.players, 1))+(j/max(l.players, 1))*4, py(j%max(l.players, 1))+20, remoteShadowChr, remoteBlobAttr, entrySize16)
	}
	end(rtExtrasEnd)
	if l.slotted {
		// body and head from LinkGraphics, sword and shield from the WRAM buffer:
		parts := []struct {
			src   [2]uint16
			flags uint8
		}{
			{[2]uint16{0x8000 + 0x00<<5, 0x8000 + 0x10<<5}, partTopRow | partBottomRow},
			{[2]uint16{0x8000 + 0x02<<5, 0x8000 + 0x12<<5}, partTopRow | partBottomRow},
			{[2]uint16{0x9000, 0x9200}, partTopRow | partBottomRow | partWRAM},
			{[2]uint16{0x9400, 0x9600}, partTopRow | partBottomRow | partWRAM},
		}
		tb[rtRequired] = 2
		binary.LittleEndian.PutUint16(tb[rtParts:], uint16(len(parts)))
		for i, pt := range parts {
			binary.LittleEndian.PutUint16(tb[rtPartSrc+i*4:], pt.src[0])
			binary.LittleEndian.PutUint16(tb[rtPartSrc+i*4+2:], pt.src[1])
			tb[rtPartFlags+i] = pt.flags
		}
		entry(px(0), py(0)+8, 0<<6, 0x20, entrySize16)
		entry(px(0), py(0), 1<<6, 0x20, entrySize16)
		entry(px(0)+12, py(0)+4, 2<<6, 0x20, entrySize16)
		entry(px(0)-8, py(0)+6, 3<<6, 0x20, entrySize16)
	}
	end(rtSlottedEnd)
	for i := 0; i < l.players; i++ {
		entry(px(i), py(i)+16+remoteShadowYOffs, remoteShadowChr, remoteBlobAttr, entrySize16)
	}
	end(rtShadowsEnd)
	for i := 0; i < l.players; i++ {
		entry(px(i), py(i)+16, remoteBlobChr0, remoteBlobAttr, entrySize16)
	}
	end(rtBlobsEnd)
	end(rtFrontEnd)
	copy(g.e.SRAM[remoteTableAddr&0x7FFF:], tb[:])
}

type rpFrame struct {
	// remoteFrame's start line and cycles from its start to the frame hook's return; the line `STZ $12` ran on, or -1
	// if the NMI came first:
	startLine, endLine int
	rfCycles           uint64
	// the time checks that skipped work, and the sprites drawn:
	lateScan, lateDraw, replay bool
	drawn                      int
}

// runMeasuredFrame runs a frame like runFrame, measuring it.
func (g *realGame) runMeasuredFrame(rfStart uint32) (m rpFrame, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			g.t.Logf("crashed: %v: %s", r, g.where())
			g.crashed, ok = true, false
		}
	}()
	rp := g.e.SRAM[remoteReplayAddr&0x7FFF:]
	ctr := func(o int) uint16 { return binary.LittleEndian.Uint16(rp[o:]) }
	ls, ld, rr := ctr(rpLateScans), ctr(rpLateDraws), ctr(rpReplays)
	binary.LittleEndian.PutUint16(rp[rpLen:], 0)

	for i := 0; i < 4 && g.e.GetPC() == 0x00805A; i++ {
		g.e.Step()
	}
	if !g.e.RunUntil(rfStart, 20_000_000) {
		return m, false
	}
	c0 := g.e.CPU.AllCycles
	if !g.e.RunUntil(0x00805A, 200_000) {
		return m, false
	}
	m.rfCycles = g.e.CPU.AllCycles - c0
	frame := g.ppu.Frame
	if !g.e.RunUntil(0x00805F, 200_000) {
		return m, false
	}
	m.endLine = g.ppu.V
	if g.ppu.Frame != frame || g.ppu.V >= rfNMILine {
		m.endLine = -1
	}
	m.startLine = int(ctr(rpStartLine))
	m.lateScan, m.lateDraw, m.replay = ctr(rpLateScans) != ls, ctr(rpLateDraws) != ld, ctr(rpReplays) != rr
	m.drawn = int(ctr(rpLen)) / rpEntrySize
	g.frame++
	return m, true
}

type rpStats struct {
	frames, lag, lateScans, lateDraws, replays int
	minDrawn, maxDrawn                         int
	maxStart, maxEnd, minHeadroom              int
	maxRF                                      uint64
}

func (s rpStats) String() string {
	return fmt.Sprintf("drawn %2d..%2d  remoteFrame max %6d cycles (%3d lines)  start <= line %3d  STZ $12 <= line %3d  "+
		"headroom >= %3d lines  late scans %d, late draws %d, replays %d, lag %d of %d",
		s.minDrawn, s.maxDrawn, s.maxRF, (s.maxRF+rfCyclesPerLine-1)/rfCyclesPerLine, s.maxStart, s.maxEnd,
		s.minHeadroom, s.lateScans, s.lateDraws, s.replays, s.lag, s.frames)
}

// measureLayout plays n frames standing still with the layout's table written before each.
func (g *realGame) measureLayout(rfStart uint32, l rpLayout, n int) (s rpStats) {
	rt := g.e.SRAM[runTimingAddr&0x7FFF:]
	lagOf := func() int32 { return u24Diff(u24(rt[rtmNMIFrames:]), u24(rt[rtmLoopFrames:])) }
	s.minDrawn, s.minHeadroom = 1<<30, 1<<30
	for f := 0; f < n; f++ {
		g.refill()
		g.writeRemoteTable(l)
		g.pad.Input[0] = 0
		lag0 := lagOf()
		m, ok := g.runMeasuredFrame(rfStart)
		if !ok {
			g.t.Fatalf("frame did not finish: %s", g.where())
		}
		// the first frames settle the VRAM slots and the replay:
		if f < 4 {
			continue
		}
		s.frames++
		if lagOf() != lag0 || m.endLine < 0 {
			s.lag++
		}
		if m.lateScan {
			s.lateScans++
		}
		if m.lateDraw {
			s.lateDraws++
		}
		if m.replay {
			s.replays++
		}
		s.minDrawn, s.maxDrawn = min(s.minDrawn, m.drawn), max(s.maxDrawn, m.drawn)
		s.maxStart = max(s.maxStart, m.startLine)
		s.maxRF = max(s.maxRF, m.rfCycles)
		if m.endLine >= 0 {
			s.maxEnd = max(s.maxEnd, m.endLine)
			s.minHeadroom = min(s.minHeadroom, rfNMILine-m.endLine)
		}
	}
	return
}

func TestRemoteFramePlayers(t *testing.T) {
	if os.Getenv("REMOTE_PLAYERS") == "" {
		t.Skip("set REMOTE_PLAYERS=1")
	}
	g, prof := newPatchedRealGame(t)
	g.e.SRAM[gatesAddr&0x7FFF] = gateSprites
	g.editSave(func(f []byte) {
		f[0x3C5] = 3
		f[0x3CA] = 0
		f[0x3C8] = 0
		fullInventory(f)
	})
	g.startGame()
	g.e.SRAM[gatesAddr&0x7FFF] = gateSprites
	rfStart := prof.label(t, "rf_start") | 0x800000 // run from FastROM

	const frames = 120
	layouts := func() (ls []rpLayout) {
		for n := 0; n <= 15; n++ {
			ls = append(ls, rpLayout{players: n})
		}
		for n := 1; 4+2*n <= remoteEntryMax; n++ {
			ls = append(ls, rpLayout{slotted: true, players: n})
		}
		for n := 1; 4+2*n <= remoteEntryMax; n++ {
			ls = append(ls, rpLayout{slotted: true, players: n, extras: min(remoteExtraMax, remoteEntryMax-4-2*n)})
		}
		return
	}()
	scene := func(name string) {
		g.walk(0, 30)
		t.Logf("%s: %s", name, g.where())
		var blob1, blob15, link1 rpStats
		worstStart := 0
		for _, l := range layouts {
			s := g.measureLayout(rfStart, l, frames)
			worstStart = max(worstStart, s.maxStart)
			switch {
			case !l.slotted && l.players == 1:
				blob1 = s
			case !l.slotted && l.players == 15:
				blob15 = s
			case l.slotted && l.players == 1 && l.extras == 0:
				link1 = s
			}
			kind := "blobs"
			if l.slotted {
				kind = "Link + blobs"
			}
			if l.extras > 0 {
				kind += fmt.Sprintf(" + %d extras", l.extras)
			}
			t.Logf("  %2d players, %2d entries (%s): %s", l.players, l.entries(), kind, s)
		}
		// past the table's capacity: what is left from the latest start of remoteFrame seen (less the game's work after
		// it, rfTailCycles), at the measured cost of each player:
		per := float64(blob15.maxRF-blob1.maxRF) / 14
		base := float64(blob1.maxRF) - per
		linkExtra := float64(link1.maxRF) - float64(blob1.maxRF)
		left := float64((rfNMILine-worstStart)*rfCyclesPerLine-rfTailCycles) - base
		t.Logf("  %s: remoteFrame starts by line %d; %.0f cycles base, %.0f per blob player (%.2f lines), Link's sprites "+
			"%.0f more than a blob; extrapolated: %d blob players, or Link and %d blob players, before the NMI", name,
			worstStart, base, per, per/rfCyclesPerLine, linkExtra, int(left/per), int((left-linkExtra)/per)-1)
	}
	scene("underworld")
	// walk out of the house's door:
	for f := 0; f < 600 && g.e.WRAM[0x1B] != 0; f++ {
		g.runFrame(0x0400)
	}
	if !g.settle() || g.e.WRAM[0x1B] != 0 {
		t.Fatalf("did not reach the overworld: %s", g.where())
	}
	g.walk(0x0400, 20)
	scene("overworld")
}
