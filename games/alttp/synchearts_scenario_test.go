package alttp

import (
	"testing"
)

const (
	hpCapacity    = uint16(0xF36C)
	hpPieces      = uint16(0xF36B)
	hpFrozen      = uint16(0x02E4)
	hpArea        = uint16(0x008A)
	hpInDungeon   = uint16(0x001B)
	hpSpriteState = uint16(0x0DD0)
	hpSpriteType  = uint16(0x0E20)
	hpSlot        = uint16(3)
	// a few frames more than skSettleFrames: overworld flags arrive with the SRAM syncables
	hpSettleFrames = 16

	// Kakariko Village and Lost Woods:
	hpAreaA = uint8(0x18)
	hpAreaB = uint8(0x00)
)

// onOverworldWithHearts puts player i in an overworld area with a heart piece sprite in slot hpSlot.
func (h *skScenario) onOverworldWithHearts(i int, area uint8, capacity, pieces uint8) {
	h.onOverworld(i, 0)
	e := h.gs[i].e
	e.WRAM[hpInDungeon] = 0
	e.WRAM[hpArea] = area
	e.WRAM[hpArea+1] = 0
	e.WRAM[hpCapacity] = capacity
	e.WRAM[hpPieces] = pieces
	e.WRAM[hpFrozen] = 0
	e.WRAM[hpSpriteState+hpSlot] = 0x09
	e.WRAM[hpSpriteType+hpSlot] = 0xEB
}

// collectHeartPiece does what Sprite_EB_HeartPiece does when Link touches it, except for adding the heart container
// after the 4th piece (see finishHeartContainer).
func (h *skScenario) collectHeartPiece(i int) {
	e := h.gs[i].e
	area := uint16(e.WRAM[hpArea])
	e.WRAM[hpSpriteState+hpSlot] = 0
	e.WRAM[0xF280+area] |= 0x40
	e.WRAM[hpPieces] = (e.WRAM[hpPieces] + 1) & 3
	if e.WRAM[hpPieces] == 0 {
		// Link holds up the heart container:
		e.WRAM[hpFrozen] = 1
	}
}

// finishHeartContainer adds the heart container once Link stops holding it up.
func (h *skScenario) finishHeartContainer(i int) {
	e := h.gs[i].e
	e.WRAM[hpCapacity] += 8
	e.WRAM[hpFrozen] = 0
}

func (h *skScenario) expectHearts(when string, capacity, pieces uint8) {
	h.t.Helper()
	for i, gs := range h.gs {
		e := gs.e
		if e.WRAM[hpCapacity] != capacity || e.WRAM[hpPieces] != pieces {
			h.t.Errorf(
				"%s: g%d: expected capacity $%02x and %d heart pieces; got $%02x and %d",
				when, i+1, capacity, pieces, e.WRAM[hpCapacity], e.WRAM[hpPieces],
			)
		}
	}
}

func (h *skScenario) expectHeartSprite(when string, i int, present bool) {
	h.t.Helper()
	if actual := h.gs[i].e.WRAM[hpSpriteState+hpSlot] != 0; actual != present {
		h.t.Errorf("%s: g%d: expected heart piece sprite present = %v; got %v", when, i+1, present, actual)
	}
}

func TestHeartsScenario_PieceRemovedForPlayersInTheSameArea(t *testing.T) {
	h := newSKScenario(t, 3)
	h.onOverworldWithHearts(0, hpAreaA, 3<<3, 0)
	h.onOverworldWithHearts(1, hpAreaA, 3<<3, 0)
	h.onOverworldWithHearts(2, hpAreaB, 3<<3, 0)
	h.frames(hpSettleFrames)
	h.expectHearts("initial", 3<<3, 0)

	h.collectHeartPiece(0)
	h.frames(hpSettleFrames)
	h.expectHearts("after g1 collects a heart piece", 3<<3, 1)
	h.expectHeartSprite("after g1 collects a heart piece", 1, false)
	if h.gs[1].e.WRAM[0xF280+uint16(hpAreaA)]&0x40 == 0 {
		t.Errorf("g2: expected the heart piece flag of area $%02x to be set", hpAreaA)
	}
	// g3 is in another area, whose own heart piece stays:
	h.expectHeartSprite("after g1 collects a heart piece", 2, true)
}

func TestHeartsScenario_ConcurrentPiecesInDifferentAreas(t *testing.T) {
	h := newSKScenario(t, 2)
	h.onOverworldWithHearts(0, hpAreaA, 3<<3, 1)
	h.onOverworldWithHearts(1, hpAreaB, 3<<3, 1)
	h.frames(hpSettleFrames)

	// both players collect a different heart piece in the same frame:
	h.collectHeartPiece(0)
	h.collectHeartPiece(1)
	h.frames(hpSettleFrames)
	h.expectHearts("after concurrent heart pieces", 3<<3, 3)
	h.expectHeartSprite("after concurrent heart pieces", 0, false)
	h.expectHeartSprite("after concurrent heart pieces", 1, false)
}

func TestHeartsScenario_FourthPieceCountsOnce(t *testing.T) {
	h := newSKScenario(t, 2)
	h.onOverworldWithHearts(0, hpAreaA, 3<<3, 3)
	h.onOverworldWithHearts(1, hpAreaB, 3<<3, 3)
	h.frames(hpSettleFrames)

	// g1 collects its 4th piece and holds up the heart container for a while. Meanwhile g2 collects its own 4th piece
	// and gets its heart container, which is added to g1's count while g1's heart container is not added yet:
	h.collectHeartPiece(0)
	h.frames(4)
	h.collectHeartPiece(1)
	h.frames(4)
	h.finishHeartContainer(1)
	h.frames(hpSettleFrames)
	h.finishHeartContainer(0)
	h.frames(hpSettleFrames)

	// 3 + 1 + 1 pieces = 1 heart container and 1 piece:
	h.expectHearts("after two 4th pieces", 4<<3, 1)
}

func TestHeartsScenario_ReloadAdoptsHigherCount(t *testing.T) {
	h := newSKScenario(t, 2)
	h.onOverworldWithHearts(0, hpAreaA, 3<<3, 0)
	h.onOverworldWithHearts(1, hpAreaB, 3<<3, 0)
	h.frames(hpSettleFrames)

	h.collectHeartPiece(0)
	h.frames(hpSettleFrames)
	h.expectHearts("after g1 collects a heart piece", 3<<3, 1)

	// g2 save & quits to the file select screen and loads a save file with fewer hearts:
	h.gs[1].e.WRAM[skModule] = 0x01
	h.frames(4)
	h.onOverworldWithHearts(1, hpAreaB, 3<<3, 0)
	h.frames(hpSettleFrames)
	h.expectHearts("after g2 reloads", 3<<3, 1)
}

// TestHeartsScenario_FourthPieceCountsOnceWhileUnfrozen is like TestHeartsScenario_FourthPieceCountsOnce, but Link is
// not frozen ($02E4) while the heart container is not added yet, so g1 receives g2's heart container meanwhile.
func TestHeartsScenario_FourthPieceCountsOnceWhileUnfrozen(t *testing.T) {
	h := newSKScenario(t, 2)
	h.onOverworldWithHearts(0, hpAreaA, 3<<3, 3)
	h.onOverworldWithHearts(1, hpAreaB, 3<<3, 3)
	h.frames(hpSettleFrames)

	h.collectHeartPiece(0)
	h.gs[0].e.WRAM[hpFrozen] = 0
	h.frames(4)
	h.collectHeartPiece(1)
	h.frames(4)
	h.finishHeartContainer(1)
	h.frames(hpSettleFrames)
	if h.gs[0].e.WRAM[hpPieces] != 1 {
		t.Fatalf("g1: expected g2's heart container to be added before g1's; got %d heart pieces", h.gs[0].e.WRAM[hpPieces])
	}
	h.finishHeartContainer(0)
	h.frames(hpSettleFrames)

	h.expectHearts("after two 4th pieces", 4<<3, 1)
}
