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
