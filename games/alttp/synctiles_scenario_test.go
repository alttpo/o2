package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

const (
	ftRoom = uint16(0x0057)
	ftSlot = uint16(2)
	// ftCalls counts the calls of the UpdateTilemapWithCommonObject stub, which records the last X/Y in ftLastX/Y:
	ftCalls = uint32(0x1F00)
	ftLastX = uint32(0x1F01)
	ftLastY = uint32(0x1F03)
	// a few frames for the state to travel and the update routine to run:
	ftSettleFrames = 24
)

type ftScenario struct {
	*skScenario
}

// newFlyingTilesScenario stubs UpdateTilemapWithCommonObject to count its calls and record $00..$03 and Y.
func newFlyingTilesScenario(t *testing.T, players int) *ftScenario {
	t.Helper()
	h := &ftScenario{skScenario: newSKScenario(t, players)}
	for _, gs := range h.gs {
		addr := gs.g.romFunctions[fnUpdateTilemapWithCommonObject]
		pak, _ := lorom.BusAddressToPak(addr)
		copy(gs.e.ROM[pak:], []byte{
			0xEE, 0x00, 0x1F, // INC $1F00
			0xA5, 0x00, // LDA $00
			0x8D, 0x01, 0x1F, // STA $1F01
			0xA5, 0x01, // LDA $01
			0x8D, 0x02, 0x1F, // STA $1F02
			0xA5, 0x02, // LDA $02
			0x8D, 0x03, 0x1F, // STA $1F03
			0xA5, 0x03, // LDA $03
			0x8D, 0x04, 0x1F, // STA $1F04
			0x8C, 0x05, 0x1F, // STY $1F05
			0x6B, // RTL
		})
	}
	return h
}

// loadTileRoom does what Dungeon_LoadSingleOverlord does for the room's tile room overlord, in the upper left quadrant
// with the screen there.
func (h *ftScenario) loadTileRoom(i int, room uint16) {
	h.inRoom(i, room)
	e := h.gs[i].e
	for k := uint32(0); k < 0x48; k++ {
		e.WRAM[uint32(overlordType)+k] = 0
	}
	e.WRAM[uint32(overlordType+ftSlot)] = overlordTileRoom
	e.WRAM[uint32(overlordXLo+ftSlot)] = 0x70
	e.WRAM[uint32(overlordYLo+ftSlot)] = 0x80
	h.setWRAMU16(i, 0xE2, 0)
	h.setWRAMU16(i, 0xE8, 0)
	e.WRAM[ftCalls] = 0
}

// tick does what Overlord14_TileRoom does every frame, except for spawning the tile sprite.
func (h *ftScenario) tick(i int) {
	e := h.gs[i].e
	if e.WRAM[skModule] != 0x07 || e.WRAM[skSubModule] != 0 {
		return
	}
	if e.WRAM[uint32(overlordType+ftSlot)] != overlordTileRoom {
		return
	}
	x := (uint16(e.WRAM[uint32(overlordXLo+ftSlot)]) | uint16(e.WRAM[uint32(overlordXHi+ftSlot)])<<8) - h.wramU16(i, 0xE2)
	y := (uint16(e.WRAM[uint32(overlordYLo+ftSlot)]) | uint16(e.WRAM[uint32(overlordYHi+ftSlot)])<<8) - h.wramU16(i, 0xE8)
	if x&0xFF00 != 0 || y&0xFF00 != 0 {
		return
	}
	e.WRAM[uint32(overlordGen2+ftSlot)]--
	if e.WRAM[uint32(overlordGen2+ftSlot)] != 0x80 {
		return
	}
	e.WRAM[uint32(overlordGen1+ftSlot)]++
	if e.WRAM[uint32(overlordGen1+ftSlot)] != flyingTilesCount {
		e.WRAM[uint32(overlordGen2+ftSlot)] = 0xE0
	} else {
		e.WRAM[uint32(overlordType+ftSlot)] = 0
	}
}

// run runs n frames, ticking the overlords.
func (h *ftScenario) run(n int) {
	for f := 0; f < n; f++ {
		for i := range h.gs {
			h.tick(i)
		}
		h.frame()
	}
}

func (h *ftScenario) state(i int) (active bool, count, timer uint8) {
	e := h.gs[i].e
	return e.WRAM[uint32(overlordType+ftSlot)] == overlordTileRoom,
		e.WRAM[uint32(overlordGen1+ftSlot)],
		e.WRAM[uint32(overlordGen2+ftSlot)]
}

func (h *ftScenario) erased(i int) int { return int(h.gs[i].e.WRAM[ftCalls]) }

// expectSynced checks that g2 caught up with g1 within a few frames.
func (h *ftScenario) expectSynced(when string) {
	h.t.Helper()
	a1, c1, t1 := h.state(0)
	a2, c2, t2 := h.state(1)
	if a1 != a2 || c1 != c2 {
		h.t.Fatalf("%s: expected g2's overlord (active %v, %d tiles) to match g1's (active %v, %d tiles)", when, a2, c2, a1, c1)
	}
	if d := int(t2) - int(t1); a1 && (d < 0 || d > flyingTilesAhead) {
		h.t.Errorf("%s: expected g2's timer $%02x a few frames behind g1's $%02x", when, t2, t1)
	}
}

func TestFlyingTilesScenario_EnteringLaterTakesTheTimer(t *testing.T) {
	h := newFlyingTilesScenario(t, 2)
	h.loadTileRoom(0, ftRoom)
	h.inRoom(1, ssOtherRoom)
	// g1 sees 3 tiles spawn and is 40 frames into the wait for the 4th:
	h.run(128 + 96*2 + 40)
	if _, c, _ := h.state(0); c != 3 {
		t.Fatalf("g1: expected 3 tiles spawned; got %d", c)
	}

	h.loadTileRoom(1, ftRoom)
	h.run(ftSettleFrames)
	h.expectSynced("after g2 entered")
	if n := h.erased(1); n != 3 {
		t.Errorf("g2: expected the 3 lifted floor tiles erased; got %d", n)
	}
	if x, y := h.wramU16(1, ftLastX), h.wramU16(1, ftLastY); x != 0x0060 || y != 0x0070 {
		t.Errorf("g2: expected the 3rd floor tile erased at $0060,$0070; got $%04x,$%04x", x, y)
	}
	if h.erased(0) != 0 {
		t.Errorf("g1: expected no floor tiles erased by sync; got %d", h.erased(0))
	}

	// both keep spawning tiles in step:
	h.run(96 * 2)
	h.expectSynced("two tiles later")
	if h.erased(0) != 0 || h.erased(1) != 3 {
		t.Errorf("expected no more floor tiles erased by sync; got %d and %d", h.erased(0), h.erased(1))
	}
}

func TestFlyingTilesScenario_EnteringAfterTheLastTile(t *testing.T) {
	h := newFlyingTilesScenario(t, 2)
	h.loadTileRoom(0, ftRoom)
	h.inRoom(1, ssOtherRoom)
	h.run(128 + 96*21 + 10)
	if a, c, _ := h.state(0); a || c != flyingTilesCount {
		t.Fatalf("g1: expected the overlord done; got active %v with %d tiles", a, c)
	}

	h.loadTileRoom(1, ftRoom)
	h.run(ftSettleFrames * 6)
	h.expectSynced("after g2 entered")
	if n := h.erased(1); n != flyingTilesCount {
		t.Errorf("g2: expected all %d floor tiles erased; got %d", flyingTilesCount, n)
	}
}

func TestFlyingTilesScenario_OnlyWhileTheOverlordIsOnScreen(t *testing.T) {
	h := newFlyingTilesScenario(t, 2)
	h.loadTileRoom(0, ftRoom)
	h.inRoom(1, ssOtherRoom)
	h.run(128 + 96 + 10)

	// g2 enters the room in another quadrant:
	h.loadTileRoom(1, ftRoom)
	h.setWRAMU16(1, 0xE2, 0x0100)
	h.run(ftSettleFrames)
	if _, c, tm := h.state(1); c != 0 || tm != 0 || h.erased(1) != 0 {
		t.Errorf("g2: expected the overlord untouched while off screen; got %d tiles, timer $%02x", c, tm)
	}

	// and walks into the overlord's quadrant:
	h.setWRAMU16(1, 0xE2, 0x0000)
	h.run(ftSettleFrames)
	h.expectSynced("after g2 entered the quadrant")
}

func TestFlyingTilesScenario_EnteringTogether(t *testing.T) {
	h := newFlyingTilesScenario(t, 2)
	h.loadTileRoom(0, ftRoom)
	h.loadTileRoom(1, ftRoom)
	h.run(128 + 96*3)
	for i := range h.gs {
		if _, c, _ := h.state(i); c != 4 || h.erased(i) != 0 {
			t.Errorf("g%d: expected 4 tiles spawned by itself; got %d with %d erased by sync", i+1, c, h.erased(i))
		}
	}
}
