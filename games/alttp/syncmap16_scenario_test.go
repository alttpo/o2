package alttp

import (
	"github.com/alttpo/snes/mapping/lorom"
	"testing"
)

const (
	m16Area      = uint16(0x18) // Kakariko Village
	m16OtherArea = uint16(0x1B)
	// a position near $84 and one far away from it:
	m16Pos      = uint16(0x0410)
	m16PosFar   = uint16(0x1F7C)
	m16Bush     = uint16(0x0DC7)
	m16Drawn    = uint32(0x14000) // test stub marks drawn tiles at $7F4000,X
	m16ScrollAt = uint16(0x0390)
	// a position in VRAM 20 rows below $84 (row 7, column 8) and one at the far side of a small area:
	m16PosLow      = uint16(0x0D90)
	m16PosSmallFar = uint16(0x0F3C)
	m16ScreenSize  = uint32(0x0712)
)

// newM16Scenario stubs Overworld_DrawMap16_Persist to write the tile to $7E2000,X and mark it drawn at $7F4000,X.
func newM16Scenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := newSKScenario(t, players)
	for _, gs := range h.gs {
		addr := gs.g.romFunctions[fnOverworldDrawMap16Persist]
		pak, _ := lorom.BusAddressToPak(addr)
		copy(gs.e.ROM[pak:], []byte{
			0x9F, 0x00, 0x20, 0x7E, // STA.l $7E2000,X
			0x9F, 0x00, 0x40, 0x7F, // STA.l $7F4000,X
			0x6B, // RTL
		})
	}
	return h
}

// inArea puts the player in an overworld area as freshly loaded.
func (h *skScenario) inArea(i int, area uint16) {
	h.onOverworld(i, 0)
	e := h.gs[i].e
	e.WRAM[0x1B] = 0
	e.WRAM[0x8A] = uint8(area)
	e.WRAM[0x8B] = 0
	e.WRAM[0x84] = uint8(m16ScrollAt & 0xFF)
	e.WRAM[0x85] = uint8(m16ScrollAt >> 8)
	// a large area (Overworld_LoadGFXAndScreenSize):
	e.WRAM[m16ScreenSize] = 0x20
	e.WRAM[map16LogIndex] = 0
	e.WRAM[map16LogIndex+1] = 0
	for o := uint32(0); o < 0x4000; o++ {
		e.WRAM[0x2000+o] = 0
		e.WRAM[m16Drawn+o] = 0
	}
}

func (h *skScenario) wramU16(i int, addr uint32) uint16 {
	e := h.gs[i].e
	return uint16(e.WRAM[addr]) | uint16(e.WRAM[addr+1])<<8
}

func (h *skScenario) setWRAMU16(i int, addr uint32, v uint16) {
	e := h.gs[i].e
	e.WRAM[addr] = uint8(v)
	e.WRAM[addr+1] = uint8(v >> 8)
}

// changeMap16 does what the game does for a persistent map16 change (e.g. lifting a bush).
func (h *skScenario) changeMap16(i int, pos, tile uint16) {
	h.setWRAMU16(i, 0x2000+uint32(pos), tile)
	x := uint32(h.wramU16(i, uint32(map16LogIndex)))
	h.setWRAMU16(i, 0xF800+x, pos)
	h.setWRAMU16(i, 0xFA00+x, tile)
	h.setWRAMU16(i, uint32(map16LogIndex), uint16(x+2))
}

func (h *skScenario) logLen(i int) int {
	return int(h.wramU16(i, uint32(map16LogIndex)) >> 1)
}

func (h *skScenario) expectTile(when string, i int, pos, tile uint16, drawn bool) {
	h.t.Helper()
	if actual := h.wramU16(i, 0x2000+uint32(pos)); actual != tile {
		h.t.Errorf("%s: g%d: expected tile $%04x at $%04x; got $%04x", when, i+1, tile, pos, actual)
	}
	d := h.wramU16(i, m16Drawn+uint32(pos)) == tile
	if d != drawn {
		h.t.Errorf("%s: g%d: expected tile at $%04x drawn = %v; got %v", when, i+1, pos, drawn, d)
	}
}

func TestMap16Scenario_Change(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("g1 lifted a bush", 1, m16Pos, m16Bush, true)
	// g2 memorized the change, and nothing is echoed back and forth:
	h.frames(skSettleFrames * 4)
	if h.logLen(0) != 1 || h.logLen(1) != 1 {
		t.Errorf("expected one log entry for both players; got %d and %d", h.logLen(0), h.logLen(1))
	}
	if h.wramU16(1, 0xF800) != m16Pos || h.wramU16(1, 0xFA00) != m16Bush {
		t.Errorf("g2: expected change memorized in its log")
	}

	// only new changes are applied; g2 lifts another bush next to it:
	h.changeMap16(1, m16Pos+2, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("g2 lifted a bush", 0, m16Pos+2, m16Bush, true)
	h.frames(skSettleFrames * 4)
	if h.logLen(0) != 2 || h.logLen(1) != 2 {
		t.Errorf("expected two log entries for both players; got %d and %d", h.logLen(0), h.logLen(1))
	}
}

func TestMap16Scenario_OffScreenNotDrawn(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames)

	h.changeMap16(0, m16PosFar, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("far away", 1, m16PosFar, m16Bush, false)
}

func TestMap16Scenario_InVRAMBelowScreenDrawn(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames)

	// VRAM holds rows -7..+24 of $84 in large areas; this tile is in VRAM, so it must be drawn there:
	h.changeMap16(0, m16PosLow, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("in VRAM below the screen", 1, m16PosLow, m16Bush, true)
}

func TestMap16Scenario_SmallAreaAlwaysDrawn(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16Area)
	for _, gs := range h.gs {
		// small areas are entirely in VRAM and never stream tiles while scrolling:
		gs.e.WRAM[m16ScreenSize] = 0
	}
	h.frames(skSettleFrames)

	h.changeMap16(0, m16PosSmallFar, m16Bush)
	h.frames(skSettleFrames)
	h.expectTile("small area", 1, m16PosSmallFar, m16Bush, true)
}

func TestMap16Scenario_OtherArea(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16OtherArea)
	h.frames(skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)
	if h.wramU16(1, 0x2000+uint32(m16Pos)) != 0 {
		t.Errorf("other area: g2: expected the tile unchanged")
	}
	if h.wramU16(1, m16Drawn+uint32(m16Pos)) != 0 {
		t.Errorf("other area: g2: expected nothing drawn")
	}
}

func TestMap16Scenario_EnterAreaCatchesUp(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16OtherArea)
	h.frames(skSettleFrames)

	for k := uint16(0); k < 12; k++ {
		h.changeMap16(0, m16Pos+k*2, m16Bush+k)
	}
	h.frames(skSettleFrames)

	// g2 enters the area; the area load resets its log:
	h.gs[1].e.WRAM[skSubModule] = 0x01
	h.frames(2)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames * 4)
	for k := uint16(0); k < 12; k++ {
		h.expectTile("g2 entered", 1, m16Pos+k*2, m16Bush+k, true)
	}
	if h.logLen(0) != 12 || h.logLen(1) != 12 {
		t.Errorf("expected 12 log entries for both players; got %d and %d", h.logLen(0), h.logLen(1))
	}

	// g1 leaves and comes back; g2 passes the changes on:
	h.gs[0].e.WRAM[skSubModule] = 0x01
	h.frames(2)
	h.inArea(0, m16OtherArea)
	h.frames(4)
	h.gs[0].e.WRAM[skSubModule] = 0x01
	h.frames(2)
	h.inArea(0, m16Area)
	h.frames(skSettleFrames * 4)
	for k := uint16(0); k < 12; k++ {
		h.expectTile("g1 returned", 0, m16Pos+k*2, m16Bush+k, true)
	}
}

func TestMap16Scenario_ConflictDoesNotEcho(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames)

	// both players change the same tile differently in the same frame:
	h.changeMap16(0, m16Pos, m16Bush)
	h.changeMap16(1, m16Pos, m16Bush+1)
	h.frames(skSettleFrames * 8)
	if h.logLen(0) > 4 || h.logLen(1) > 4 {
		t.Errorf("expected the logs to stop growing; got %d and %d entries", h.logLen(0), h.logLen(1))
	}
	a, b := h.wramU16(0, 0x2000+uint32(m16Pos)), h.wramU16(1, 0x2000+uint32(m16Pos))
	if a != b {
		t.Errorf("expected both players to agree on the tile; got $%04x and $%04x", a, b)
	}
}

func TestMap16Scenario_OtherTeam(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.gs[1].g.local.Team = 1
	h.inArea(0, m16Area)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)
	if h.wramU16(1, 0x2000+uint32(m16Pos)) != 0 {
		t.Errorf("other team: g2: expected the tile unchanged")
	}
}

func TestMap16Scenario_ManyChangesWhileAway(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16OtherArea)
	h.frames(skSettleFrames)

	// g1 lifts a semicircle of rocks while g2 is elsewhere; g2 then enters the area and must take all of them:
	const n = 10
	for k := 0; k < n; k++ {
		h.changeMap16(0, m16Pos+uint16(k<<1), m16Bush)
	}
	h.frames(skSettleFrames)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames * 3)
	for k := 0; k < n; k++ {
		h.expectTile("entered area", 1, m16Pos+uint16(k<<1), m16Bush, true)
	}
}

func TestMap16Scenario_RemoteInMenu(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inArea(1, m16OtherArea)
	h.frames(skSettleFrames)

	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)
	// g1 opens the item menu; g2 then enters the area and must still take g1's changes:
	h.gs[0].e.WRAM[skModule] = 0x0E
	h.frames(2)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames * 2)
	h.expectTile("g1 in menu", 1, m16Pos, m16Bush, true)
}

func TestMap16Scenario_ExitDungeonIntoArea(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inRoom(1, 0x0083)
	h.frames(skSettleFrames)

	// g1 lifts a semicircle of rocks while g2 is in a dungeon; g2 then exits into the area:
	stones := []uint16{0x02A2, 0x02A8, 0x0324, 0x0326}
	for _, pos := range stones {
		h.changeMap16(0, pos, 0x0DC4)
		h.frames(4)
	}
	h.frames(skSettleFrames)
	h.inArea(1, m16Area)
	h.frames(skSettleFrames * 3)
	for _, pos := range stones {
		h.expectTile("exited dungeon", 1, pos, 0x0DC4, true)
	}
}

func TestMap16Scenario_WithPaletteUpdate(t *testing.T) {
	h := newM16Scenario(t, 2)
	h.inArea(0, m16Area)
	h.inRoom(1, 0x0083)
	h.frames(skSettleFrames)

	stones := []uint16{0x02A2, 0x02A8, 0x0324, 0x0326}
	for _, pos := range stones {
		h.changeMap16(0, pos, 0x0DC4)
	}
	h.frames(skSettleFrames)

	// g2 exits into the area while its tunic colors need updating too (the area load reloads the palette), which
	// together with the map16 routine does not fit in one update routine:
	g := h.gs[1].g
	g.SyncTunicColor = true
	g.local.PlayerColor = 0x5CD2
	h.inArea(1, m16Area)
	// hold off updates (Link frozen) until g2 has g1's log, so that both are due in the same update:
	e := h.gs[1].e
	e.WRAM[0x02E4] = 1
	h.frames(skSettleFrames)
	e.WRAM[0x02E4] = 0
	h.frames(skSettleFrames * 3)
	for _, pos := range stones {
		h.expectTile("exited dungeon", 1, pos, 0x0DC4, true)
	}
	if c := h.wramU16(1, 0xC6F8); c != 0x5CD2 {
		t.Errorf("g2: expected tunic color $5cd2; got $%04x", c)
	}
}

// newM16StripeScenario is newM16Scenario with a draw stub that also uses $10 bytes of the VRAM stripe buffer ($1000)
// per tile like Overworld_DrawMap16_Persist; settleStripes empties it like NMI does when $14 is set.
func newM16StripeScenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := newSKScenario(t, players)
	for _, gs := range h.gs {
		addr := gs.g.romFunctions[fnOverworldDrawMap16Persist]
		pak, _ := lorom.BusAddressToPak(addr)
		copy(gs.e.ROM[pak:], []byte{
			0x9F, 0x00, 0x20, 0x7E, // STA.l $7E2000,X
			0x9F, 0x00, 0x40, 0x7F, // STA.l $7F4000,X
			0xAF, 0x00, 0x10, 0x7E, // LDA.l $7E1000
			0x18,             // CLC
			0x69, 0x10, 0x00, // ADC #$0010
			0x8F, 0x00, 0x10, 0x7E, // STA.l $7E1000
			0x6B, // RTL
		})
	}
	return h
}

func (h *skScenario) settleStripes(n int) {
	for f := 0; f < n; f++ {
		h.frame()
		for _, gs := range h.gs {
			if gs.e.WRAM[0x14] != 0 {
				gs.e.WRAM[0x1000] = 0
				gs.e.WRAM[0x1001] = 0
				gs.e.WRAM[0x14] = 0
			}
			// the game's own stripes queued for the next frame:
			if h.gameStripes != 0 {
				gs.e.WRAM[0x1000] = h.gameStripes
				gs.e.WRAM[0x14] = 1
			}
		}
	}
}

func TestMap16Scenario_FullLogMakesProgress(t *testing.T) {
	testMap16FullLog(t, 0)
}

func TestMap16Scenario_FullLogMakesProgressWithBusyStripes(t *testing.T) {
	// only room for 3 tiles per frame; updates apply part of their changes and the rest later:
	testMap16FullLog(t, 0x50)
}

func testMap16FullLog(t *testing.T, gameStripes uint8) {
	h := newM16StripeScenario(t, 2)
	h.gameStripes = gameStripes
	h.inArea(0, m16Area)
	h.inRoom(1, 0x0083)
	h.settleStripes(skSettleFrames)

	// g1 fills its whole log while g2 is in a dungeon; spread over the large area so some are in VRAM around $84
	// (row 7, column 8) and some are not:
	positions := make([]uint16, 0, map16LogMax)
	for k := 0; k < map16LogMax; k++ {
		row, col := uint16(k/16)*3, uint16(k%16)*4
		positions = append(positions, row<<7|col<<1)
	}
	for _, pos := range positions {
		h.changeMap16(0, pos, m16Bush)
	}
	if h.logLen(0) != map16LogMax {
		t.Fatalf("g1: expected a full log of %d entries; got %d", map16LogMax, h.logLen(0))
	}
	h.settleStripes(skSettleFrames)

	h.inArea(1, m16Area)
	h.settleStripes(map16LogMax * 2)

	s84 := m16ScrollAt
	missing := 0
	for _, pos := range positions {
		dr := int(pos&0x1F80)>>7 - int(s84&0x1F80)>>7
		dc := int(pos&0x007E)>>1 - int(s84&0x007E)>>1
		inVRAM := dr >= -7 && dr < 24 && dc >= -7 && dc < 24
		if h.wramU16(1, 0x2000+uint32(pos)) != m16Bush {
			missing++
			continue
		}
		h.expectTile("full log", 1, pos, m16Bush, inVRAM)
	}
	if missing > 0 {
		t.Errorf("g2: %d of %d changes not applied", missing, len(positions))
	}
	// g2 passes them on in its own log:
	if n := h.logLen(1); n != map16LogMax {
		t.Errorf("g2: expected its log to hold all %d changes; got %d", map16LogMax, n)
	}
}

func TestMap16Scenario_PortalWarpKeepsPreviousAreaLog(t *testing.T) {
	const lwArea, dwArea = uint16(0x07), uint16(0x47)
	h := newM16Scenario(t, 2)
	h.inArea(0, lwArea)
	h.inArea(1, dwArea)
	h.frames(skSettleFrames)

	// g1 pounds a peg in the light world:
	h.changeMap16(0, m16Pos, m16Bush)
	h.frames(skSettleFrames)

	// g1 warps through the portal: the area changes and submodule 0 resumes while Link is still landing, with the
	// light world's log still there:
	e := h.gs[0].e
	e.WRAM[0x8A] = uint8(dwArea)
	e.WRAM[0x5D] = linkStateMirror
	h.frames(skSettleFrames)
	// Link lands; LinkState_CrossingWorlds resets the log:
	e.WRAM[0x5D] = 0
	h.setWRAMU16(0, uint32(map16LogIndex), 0)
	h.frames(skSettleFrames)

	if actual := h.wramU16(1, 0x2000+uint32(m16Pos)); actual != 0 {
		t.Errorf("g2: expected the light world's change not applied in the dark world; got tile $%04x", actual)
	}
	if h.logLen(1) != 0 {
		t.Errorf("g2: expected an empty log; got %d entries", h.logLen(1))
	}
}
