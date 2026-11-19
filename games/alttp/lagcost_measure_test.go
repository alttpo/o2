package alttp

// Measurements on the vanilla JP ROM behind the lag checks' budgets (lagcheck.go):
//
//	LAGCOST=1 go test -run TestLagCost_ -v
//
// TestLagCost_GameRoutines times the game routines that generated update routines call (updCalleeCycles), and
// TestLagCost_StageHooks the game's own work from the stage hooks' sites to the end of the frame (stageRestCycles).

import (
	"os"
	"testing"

	"github.com/alttpo/snes/emulator/cpu65c816"
	"o2/snes"
)

// callCycles calls the routine at fn like JSL (or JSR through rtl, an RTL in fn's bank, if rtl != 0) with the
// registers set by setup, with NMI off, and returns the CPU cycles until it returns.
func (g *realGame) callCycles(fn, rtl uint32, m8, x8 bool, setup func()) (cycles uint64, ok bool) {
	var ret uint32 = 0x7EFFF0
	c := &g.e.CPU
	saved := *c
	defer func() {
		cycles0, all := c.AllCycles, c.AllClocks
		*c = saved
		c.AllCycles, c.AllClocks = cycles0, all
	}()
	g.e.Bus.EaWrite(0x004200, 0x01)
	defer g.e.Bus.EaWrite(0x004200, 0x81)
	push := func(b byte) {
		g.e.Bus.EaWrite(uint32(c.SP), b)
		c.SP--
	}
	sp := c.SP
	push(byte(uint32(ret) >> 16))
	push(byte(uint32(ret-1) >> 8))
	push(byte(uint32(ret - 1)))
	if rtl != 0 {
		push(byte((rtl - 1) >> 8))
		push(byte(rtl - 1))
	}
	p := byte(0)
	if m8 {
		p |= 0x20
	}
	if x8 {
		p |= 0x10
	}
	c.SetFlags(p)
	c.RDBR = 0
	c.RD = 0
	if setup != nil {
		setup()
	}
	g.e.SetPC(fn)
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	for cycles < 2_000_000 {
		if g.e.GetPC() == ret && c.SP == sp {
			return cycles, true
		}
		n, _ := g.e.Step()
		cycles += uint64(n)
	}
	return cycles, false
}

func setA(c *cpu65c816.CPU, v uint16) { c.RA, c.RAl, c.RAh = v, byte(v), byte(v>>8) }
func setX(c *cpu65c816.CPU, v uint16) { c.RX, c.RXl = v, byte(v) }
func setY(c *cpu65c816.CPU, v uint16) { c.RY, c.RYl = v, byte(v) }

func jpROMFunctions(t *testing.T) map[romFunction]uint32 {
	if os.Getenv("LAGCOST") == "" {
		t.Skip("set LAGCOST=1 to measure")
	}
	contents, err := os.ReadFile(jpROMPath)
	if err != nil {
		t.Skipf("no JP ROM: %v", err)
	}
	rom, err := snes.NewROM("jp.sfc", contents)
	if err != nil {
		t.Fatal(err)
	}
	fns := make(map[romFunction]uint32)
	fillRomFunctions(rom, fns)
	return fns
}

func TestLagCost_GameRoutines(t *testing.T) {
	fns := jpROMFunctions(t)
	g := newFightGame(t, 0)
	w := g.e.WRAM[:]
	c := &g.e.CPU
	measure := func(name string, fn romFunction, rtl romFunction, m8, x8 bool, setup func()) {
		var r uint32
		if rtl >= 0 {
			r = fns[rtl]
		}
		n, ok := g.callCycles(fns[fn], r, m8, x8, setup)
		t.Logf("%-36s %7d cycles ok=%v (%s)", name, n, ok, g.where())
		g.runFrame(0)
	}
	const none = romFunction(-1)

	// the overworld:
	for i := 0; i < 30; i++ {
		g.runFrame(0)
	}
	measure("UpdatePaletteArmorGloves", fnUpdatePaletteArmorGloves, none, true, true, nil)
	measure("UpdatePaletteSword", fnUpdatePaletteSword, none, true, true, nil)
	measure("UpdatePaletteShield", fnUpdatePaletteShield, none, true, true, nil)
	measure("DecompGfxSword", fnDecompGfxSword, none, true, true, nil)
	measure("DecompGfxShield", fnDecompGfxShield, none, true, true, nil)
	measure("LoadSpriteGfx", fnLoadSpriteGfx, none, true, true, nil)
	measure("ResetSpriteProperties", fnResetSpriteProperties, none, true, true, func() { setX(c, 0x0F) })
	measure("Overworld_DrawMap16_Persist", fnOverworldDrawMap16Persist, none, false, false, func() {
		setA(c, 0x0034)
		setX(c, 0x0410)
	})
	measure("Overworld_RevealSecret", fnOverworldRevealSecret, fnBank1BRTL, false, false, func() { setX(c, 0x0410) })
	measure("SpawnThrowableTerrain_silently", fnSpawnThrowableTerrainSilently, none, true, true, func() {
		setA(c, 0)
		w[0x00], w[0x01], w[0x02], w[0x03] = w[0x22], w[0x23], w[0x20], w[0x21]
	})
	measure("Overworld_AlterWeathervane", fnOverworldAlterWeathervane, none, true, true, nil)
	measure("Overworld_OpenGargoylesDomain", fnOverworldOpenGargoylesDomain, none, true, true, nil)
	measure("Overworld_CreatePyramidHole", fnOverworldCreatePyramidHole, none, true, true, nil)
	measure("Overworld_FinishMirrorWarp", fnOverworldFinishMirrorWarp, none, true, true, nil)
	g.settle()

	// the underworld: Hyrule Castle's first room with pots ($0055 via entrance 4):
	if !g.enterRoom(0x04, 0x0055) {
		t.Fatalf("no room: %s", g.where())
	}
	measure("RoomDraw_16x16Single", fnRoomDraw16x16Single, fnBank01RTL, false, false, func() { setY(c, 0) })
	measure("RevealPotItem", fnRevealPotItem, fnBank01RTL, false, false, func() {
		setA(c, uint16(w[0x0540])|uint16(w[0x0541])<<8)
	})
	measure("ToggleStarTileGraphics", fnToggleStarTileGraphics, none, true, true, nil)
	measure("Underworld_LightTorch", fnUnderworldLightTorch, none, true, true, func() { w[0x0333] = 0xC0 })
	measure("UpdateTilemapWithCommonObject", fnUpdateTilemapWithCommonObject, none, true, true, func() {
		setY(c, 6*2)
		w[0x00], w[0x01], w[0x02], w[0x03] = w[0x22], w[0x23], w[0x20], w[0x21]
	})
	measure("RoomTag_SpawnPrize", fnRoomTagSpawnPrize, fnBank01RTL, true, true, nil)
}

// TestLagCost_StageHooks times the game's work from the stage hooks' sites to the frame hook's return, in the frames
// that reach them: area loads (crossing edges, coming out of entrances) and room loads.
func TestLagCost_StageHooks(t *testing.T) {
	fns := jpROMFunctions(t)
	g := newFightGame(t, 0)
	sites := map[uint32]string{
		fns[fnOverworldHandleOverlays]: "map16 stage hook",
		manipStageHookAddr:             "manip stage hook",
	}
	worst := map[string]uint64{}
	lines := map[string]int{}
	// counts the cycles from a site to the frame hook ($00:805A):
	frame := func() {
		var site string
		var at uint64
		cycles := uint64(0)
		for i := 0; i < 4 && g.e.GetPC() == 0x00805A; i++ {
			n, _ := g.e.Step()
			cycles += uint64(n)
		}
		for cycles < 20_000_000 {
			pc := g.e.GetPC()
			if pc == 0x00805A {
				break
			}
			if s, ok := sites[pc&^0x800000]; ok && site == "" {
				site, at = s, cycles
				lines[s] = max(lines[s], g.ppu.V)
			}
			n, _ := g.e.Step()
			cycles += uint64(n)
		}
		if site != "" {
			worst[site] = max(worst[site], cycles-at)
		}
		g.frame++
	}
	run := func(n int, in uint16) {
		for i := 0; i < n; i++ {
			g.pad.Input[0] = in
			frame()
		}
	}
	for dir := 0; dir < 4; dir++ {
		// crossEdge uses runFrame, which doesn't count; walk across by hand:
		g.idle()
		run(200, []uint16{0x0800, 0x0400, 0x0200, 0x0100}[dir])
		run(60, 0)
	}
	for _, r := range []struct {
		entrance uint8
		room     uint16
	}{{0x04, 0x0055}, {0x04, 0x0061}, {0x08, 0x0083}, {0x26, 0x0099}} {
		w := g.e.WRAM[:]
		w[0x010E], w[0x010F] = r.entrance, 0
		w[0x1B] = 0
		w[0x10], w[0x11] = 0x06, 0x00
		run(120, 0)
		// walk around the room, through its doors:
		for _, in := range []uint16{0x0800, 0x0200, 0x0400, 0x0100} {
			run(90, in)
		}
		w[0x10], w[0x11] = 0x0F, 0x00
		run(200, 0)
	}
	for s, n := range worst {
		t.Logf("%s: worst %d cycles from the site to the frame hook, latest line at the site %d", s, n, lines[s])
	}
}

// TestLagCost_ModuleEndLines logs the scanlines the module returns to the frame hook on, idle and walking.
func TestLagCost_ModuleEndLines(t *testing.T) {
	jpROMFunctions(t)
	g := newFightGame(t, 0)
	hist := map[int]int{}
	for f := 0; f < 1200; f++ {
		in := []uint16{0, 0x0800, 0x0200, 0x0400, 0x0100, 0}[(f/200)%6]
		if f == 600 {
			g.exitToOverworld()
		}
		g.runFrame(in)
		hist[g.ppu.V/16*16]++
	}
	for l := 0; l < 262; l += 16 {
		if hist[l] != 0 {
			t.Logf("lines %3d..%3d: %d frames", l, l+15, hist[l])
		}
	}
}
