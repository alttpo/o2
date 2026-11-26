package alttp

import (
	"o2/games/alttp/underworldmap"
	"os"
	"testing"
	"time"
)

// areaWarpMap returns the overworld's areas of the vanilla JP ROM, once all are drawn, or skips the test without it.
func areaWarpMap(t *testing.T) ([]byte, *underworldmap.OverworldStream) {
	t.Helper()
	rom, err := os.ReadFile("alttp-jp-vanilla.sfc")
	if err != nil {
		t.Skip(err)
	}
	s := underworldmap.SharedOverworld(rom)
	deadline := time.After(time.Minute)
	for {
		_, done, err, changed := s.Progress(1 << 30)
		if done {
			if err != nil {
				t.Fatal(err)
			}
			return rom, s
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("the overworld map took too long")
		}
	}
}

// reachableAreaPoint returns a point of the area where Link can stand and is reachable, past its first row.
func reachableAreaPoint(t *testing.T, a *underworldmap.StreamArea) (x, y uint16) {
	t.Helper()
	n := a.Walk.Size
	for i := n * n / 2; i < n*n; i++ {
		if a.Walk.Cells[i]&underworldmap.WalkReachable != 0 {
			return uint16(i%n) * 8, uint16(i/n)*8 - 8
		}
	}
	t.Fatalf("area $%02x has no reachable point", a.Area.Area)
	return
}

func TestDebug_AreaWarpFromUnderworld(t *testing.T) {
	rom, s := areaWarpMap(t)
	gs := newDebugScenario(t, 0xFF)
	gs.g.ProvideUnpatchedROM(rom)
	e := gs.e

	// the dark world's pyramid, a large area, from a dungeon:
	const area = 0x5B
	a := s.Area(area)
	if a == nil {
		t.Fatal("no area $5B")
	}
	x, y := reachableAreaPoint(t, a)
	e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0x1B] = 0x07, 0x00, 0x01
	e.WRAM[0x040C] = 0x04

	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{{T: debugOpWarp, W: "area", R: area, X: x, Y: y}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	// out of the cave room $0100 through the iris, to the dark world:
	if m, c := e.WRAM[0x10], e.WRAM[0x010C]; m != 0x0F || c != 0x08 {
		t.Fatalf("expected module $0F then $08; got $%02x then $%02x", m, c)
	}
	if v := wram16(e.WRAM[:], 0xA0); v != underworldmap.AreaWarpRoom {
		t.Errorf("room: expected $%04x; got $%04x", underworldmap.AreaWarpRoom, v)
	}
	if v := e.WRAM[0xF3CA]; v != 0x40 {
		t.Errorf("world: expected $40; got $%02x", v)
	}
	p := underworldmap.PlaceInArea(area, x, y)
	offs, vals := p.CacheWords()
	for i, o := range offs {
		if v := wram16(e.WRAM[:], 0xC140+uint32(o)); v != vals[i] {
			t.Errorf("cache +$%02x: expected $%04x; got $%04x", o, vals[i], v)
		}
	}
	if v := wram16(e.WRAM[:], 0x0696); v != 0 {
		t.Errorf("door: expected none; got $%04x", v)
	}
	if n := len(gs.g.debug.queue); n != 1 {
		t.Fatalf("expected the warp to stay queued; %d queued", n)
	}

	// Link stands at the point; the warp is done:
	e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0x1B], e.WRAM[0x8A] = 0x09, 0x00, 0x00, area
	setWram16(e.WRAM[:], 0x22, p.X)
	setWram16(e.WRAM[:], 0x20, p.Y)
	gs.g.debug.queue[0].sent = time.Now().Add(-time.Second)
	for i := 0; i < 2; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected the warp done; %d queued", n)
	}
}

func TestDebug_AreaWarpRejects(t *testing.T) {
	rom, s := areaWarpMap(t)
	gs := newDebugScenario(t, 0xFF)
	gs.g.ProvideUnpatchedROM(rom)
	x, y := reachableAreaPoint(t, s.Area(0x2C))

	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{
		// the edge:
		{T: debugOpWarp, W: "area", R: 0x2C, X: 0, Y: 0},
		// not a multiple of 8:
		{T: debugOpWarp, W: "area", R: 0x2C, X: x + 1, Y: y},
		// a screen of a large area that isn't its parent:
		{T: debugOpWarp, W: "area", R: 0x1C, X: x, Y: y},
		// out of the small area:
		{T: debugOpWarp, W: "area", R: 0x2C, X: x, Y: 0x200},
		// no such area:
		{T: debugOpWarp, W: "area", R: 0x80, X: x, Y: y},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected bad area warps to be rejected; %d queued", n)
	}
}

// TestPlaceInAreaExits compares the placement with the exit table's for the exits it reproduces: the map16 window and
// its counters in step with the scroll, for Link and the scroll where the exits put them.
func TestPlaceInAreaExits(t *testing.T) {
	for _, path := range []string{"alttp-jp-vanilla.sfc", "../../../alttp-us.sfc"} {
		rom, err := os.ReadFile(path)
		if err != nil {
			t.Log(err)
			continue
		}
		exits, err := underworldmap.OverworldExits(rom)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for i, x := range exits {
			if x.Area >= 0x80 || x.CameraY-x.ScrollY != 0x6D {
				continue
			}
			ox, oy := underworldmap.AreaOrigin(underworldmap.AreaParent(x.Area))
			// the counters in 0..15, as PlaceInArea has them:
			unkY := uint16(x.UnkY) & 0xF
			row := (x.ScrollY - oy + unkY) >> 4
			if x.Map16>>7 != row {
				continue
			}
			n++
			if unkY != -x.ScrollY&0xF {
				t.Errorf("%s: exit $%02x: y counter $%x for scroll $%04x", path, i, unkY, x.ScrollY)
			}
			// PlaceInArea for Link where the exit's scroll centers him:
			p := underworldmap.PlaceInArea(underworldmap.AreaParent(x.Area), x.ScrollX+0x76-ox, x.ScrollY+0x60-oy)
			if p.ScrollY == x.ScrollY && p.Map16>>7 != x.Map16>>7 {
				t.Errorf("%s: exit $%02x: map16 row $%02x; the exit's $%02x", path, i, p.Map16>>7, x.Map16>>7)
			}
		}
		if n < 30 {
			t.Errorf("%s: only %d exits compared", path, n)
		}
	}
}

// TestDebug_AreaWarpFromOverworld: from the overworld, Link is taken as indoors for the iris, so that its first step
// doesn't put his Y back in the cache, with the black fixed color it sets outdoors.
func TestDebug_AreaWarpFromOverworld(t *testing.T) {
	rom, s := areaWarpMap(t)
	gs := newDebugScenario(t, 0xFF)
	gs.g.ProvideUnpatchedROM(rom)
	e := gs.e
	x, y := reachableAreaPoint(t, s.Area(0x2C))
	e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0x1B], e.WRAM[0x8A] = 0x09, 0x00, 0x00, 0x1B

	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{{T: debugOpWarp, W: "area", R: 0x2C, X: x, Y: y}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	if m, c := e.WRAM[0x10], e.WRAM[0x010C]; m != 0x0F || c != 0x08 {
		t.Fatalf("expected module $0F then $08; got $%02x then $%02x", m, c)
	}
	if v := e.WRAM[0x1B]; v != 1 {
		t.Errorf("indoors: expected 1; got %d", v)
	}
	if c := [3]uint8{e.WRAM[0x9C], e.WRAM[0x9D], e.WRAM[0x9E]}; c != [3]uint8{0x20, 0x40, 0x80} {
		t.Errorf("fixed color: got %02x", c)
	}
	p := underworldmap.PlaceInArea(0x2C, x, y)
	if v := wram16(e.WRAM[:], 0xC148); v != p.Y+0x10-underworldmap.ExitWalkDown {
		t.Errorf("cache Y: expected $%04x; got $%04x", p.Y+0x10-underworldmap.ExitWalkDown, v)
	}
}
