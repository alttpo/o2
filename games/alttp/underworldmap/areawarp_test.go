package underworldmap

import (
	"os"
	"testing"

	"github.com/alttpo/snes/asm"
)

// TestAreaWarpInGame runs the game past the area warp, the iris and Link walking out of the door until he stands in
// the overworld, as the debug page's warp does it (emitDebugAreaWarp): first from a fresh state, then from the
// overworld. Link must end at the point, with the map16 window in step with the scroll.
func TestAreaWarpInGame(t *testing.T) {
	rom, err := os.ReadFile("../alttp-jp-vanilla.sfc")
	if err != nil {
		t.Skip(err)
	}
	a, _ := addressesFor(rom)
	e, err := newEmu(rom, a)
	if err != nil {
		t.Fatal(err)
	}
	pc := uint32(0x00_0000 | scratchBase + 0xF00)
	em := asm.NewEmitter(e.scratch00[0xF00:], false)
	em.SetBase(pc)
	em.SEP(0x30)
	em.JSL(a.runModule)
	em.STP()
	if err = em.Finalize(); err != nil {
		t.Fatal(err)
	}
	frames := func(max int) int {
		for i := 0; i < max; i++ {
			if e.wram8(0x10) == 0x09 && e.wram8(0x11) == 0 {
				return i
			}
			if err := e.call(pc); err != nil {
				t.Fatal(err)
			}
		}
		return max
	}
	// a routine running a frame of the game:
	check := func(p AreaPlacement) {
		t.Helper()
		if n := frames(2000); n == 2000 {
			t.Fatalf("area $%02x: Link isn't standing in the overworld", p.Area)
		}
		q := e.areaPlacement()
		_, oy := AreaOrigin(q.Area)
		if q.Area != p.Area || q.X != p.X || q.Y != p.Y || q.ScrollX != p.ScrollX || q.ScrollY-p.ScrollY > 3 {
			t.Errorf("area $%02x: asked for %+v; got %+v", p.Area, p, q)
		}
		if int(q.Map16>>7) != (int(q.ScrollY-oy)+int(int16(q.UnkY)))>>4 {
			t.Errorf("area $%02x: map16 row of $%04x out of step with scroll $%04x and counter %d", p.Area, q.Map16, q.ScrollY, int16(q.UnkY))
		}
	}
	// the first warp from a fresh state, as the generator loads the areas:
	p := PlaceInArea(0x58, 0x1c0, 0x98)
	if err := e.loadArea(p); err != nil {
		t.Fatal(err)
	}
	check(p)
	// then each one from where Link stands, as the debug routine does it:
	for _, pt := range [][3]uint16{{0x58, 0x1b8, 0xb8}, {0x58, 0x2d0, 0x1d8}, {0x1b, 0x200, 0x300}, {0x58, 0x80, 0x300}} {
		p := PlaceInArea(uint8(pt[0]), pt[1], pt[2])
		offs, vals := p.CacheWords()
		for i, o := range offs {
			e.setWRAM16(AreaCacheBase-0x7E0000+uint32(o), vals[i])
		}
		e.setWRAM16(0xA0, AreaWarpRoom)
		e.setWRAM16(0x0696, 0)
		e.setWRAM16(0x0698, 0)
		e.sys.WRAM[0xF3CA] = p.Area & 0x40
		e.sys.WRAM[0x010C] = 0x08
		e.sys.WRAM[0x4D], e.sys.WRAM[0x46], e.sys.WRAM[0x5D] = 0, 0, 0
		if e.sys.WRAM[0x1B] == 0 {
			e.sys.WRAM[0x9C], e.sys.WRAM[0x9D], e.sys.WRAM[0x9E] = 0x20, 0x40, 0x80
			e.sys.WRAM[0x1B] = 1
		}
		e.sys.WRAM[0x10], e.sys.WRAM[0x11], e.sys.WRAM[0xB0] = 0x0F, 0, 0
		check(p)
	}
}
