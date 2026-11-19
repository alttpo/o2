package alttp

import "testing"

const (
	rkSlotNW   = uint32(1) // an enemy in the north west quadrant
	rkSlotNE   = uint32(2) // an enemy in the north east quadrant
	rkSlotItem = uint32(6) // a rupee, ignored by the kill check
)

// loadKillRoom puts the player in a freshly loaded room with the kill trigger tag, Link at (x, y) with the camera on
// his quadrant, an enemy in each of the two north quadrants and a rupee.
func (h *skScenario) loadKillRoom(i int, tag uint8, x, y uint16) {
	h.loadRoomState(i, rmRoom, tag, roomFlagKey0)
	e := h.gs[i].e
	e.WRAM[rmOtherSlot+0x0DD0] = 0
	h.moveLink(i, x, y)
	h.placeSprite(i, rkSlotNW, 0x08, 0x0040, 0x0040, 0x00)
	h.placeSprite(i, rkSlotNE, 0x08, 0x0140, 0x0040, 0x00)
	h.placeSprite(i, rkSlotItem, 0xDA, 0x0060, 0x0060, 0x44)
}

func (h *skScenario) moveLink(i int, x, y uint16) {
	e := h.gs[i].e
	setWram16(e.WRAM[:], 0x22, x)
	setWram16(e.WRAM[:], 0x20, y)
	setWram16(e.WRAM[:], 0xE2, x&0xFF00)
	setWram16(e.WRAM[:], 0xE8, y&0xFF00)
}

func (h *skScenario) placeSprite(i int, slot uint32, typ uint8, x, y uint16, flags4 uint8) {
	e := h.gs[i].e
	e.WRAM[0x0DD0+slot] = 0x09
	e.WRAM[0x0E20+slot] = typ
	e.WRAM[0x0D10+slot], e.WRAM[0x0D30+slot] = uint8(x), uint8(x>>8)
	e.WRAM[0x0D00+slot], e.WRAM[0x0D20+slot] = uint8(y), uint8(y>>8)
	e.WRAM[0x0F60+slot] = flags4
	e.WRAM[0x0CAA+slot] = 0
	e.WRAM[0x0BE0+slot] = 0x03 // a prize pack
	e.WRAM[0x0E60+slot] = 0x40
}

// expectSpriteState checks the sprite's state ($0DD0) in each game.
func (h *skScenario) expectSpriteState(when string, slot uint32, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		e := h.gs[i].e
		if actual := e.WRAM[0x0DD0+slot]; actual != x {
			h.t.Errorf("%s: g%d: expected sprite %d state $%02x; got $%02x", when, i+1, slot, x, actual)
		}
		if x == 0x06 && (e.WRAM[0x0BE0+slot] != 0 || e.WRAM[0x0E60+slot] != 0 || e.WRAM[0x0DF0+slot] != 0x0F) {
			h.t.Errorf("%s: g%d: expected sprite %d killed like Sprite_KillFriends", when, i+1, slot)
		}
	}
}

func TestRoomKillsScenario_ChestRevealQuadrant(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadKillRoom(0, 0x29, 0x0050, 0x0050)
	h.loadKillRoom(1, 0x29, 0x0060, 0x0080)
	h.settleRoom(skSettleFrames)
	h.expectSpriteState("initial", rkSlotNW, 0x09, 0x09)

	// g1 kills the north west enemy and the tag reveals the chests:
	h.gs[0].e.WRAM[0x0DD0+rkSlotNW] = 0
	h.gs[0].e.WRAM[0xAE] = 0
	h.settleRoom(skSettleFrames)
	h.expectSpriteState("g1 cleared NW", rkSlotNW, 0x00, 0x06)
	// off the screen and ignored sprites are left alone:
	h.expectSpriteState("g1 cleared NW", rkSlotNE, 0x09, 0x09)
	h.expectSpriteState("g1 cleared NW", rkSlotItem, 0x09, 0x09)

	// the enemy dies here, then nothing more happens:
	h.gs[1].e.WRAM[0x0DD0+rkSlotNW] = 0
	// a try a second later finds nothing left to kill:
	h.settleRoom(90)
	h.expectSpriteState("settled", rkSlotNE, 0x09, 0x09)
	if rs := &h.gs[1].g.roomState; rs.inflight != nil || rs.killsDone&0x01 == 0 && rs.kills&0x01 == 0 {
		t.Errorf("g2: expected the kill to be done; killsDone $%02x kills $%02x", rs.killsDone, rs.kills)
	}
}

func TestRoomKillsScenario_RoomDoors(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadKillRoom(0, 0x0A, 0x0050, 0x0050)
	h.loadKillRoom(1, 0x0A, 0x0050, 0x0050)
	h.settleRoom(skSettleFrames)

	// g1 kills both enemies and the tag opens the trap doors:
	h.gs[0].e.WRAM[0x0DD0+rkSlotNW] = 0
	h.gs[0].e.WRAM[0x0DD0+rkSlotNE] = 0
	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g1 cleared room", 0, 0)
	// the whole room, on the screen or not:
	h.expectSpriteState("g1 cleared room", rkSlotNW, 0x00, 0x06)
	h.expectSpriteState("g1 cleared room", rkSlotNE, 0x00, 0x06)
	h.expectSpriteState("g1 cleared room", rkSlotItem, 0x09, 0x09)
}

func TestRoomKillsScenario_WaitsForQuadrant(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadKillRoom(0, 0x09, 0x0150, 0x0050)
	h.loadKillRoom(1, 0x09, 0x0050, 0x0050)
	h.settleRoom(skSettleFrames)

	// g1 clears the north east quadrant:
	h.gs[0].e.WRAM[0x0DD0+rkSlotNE] = 0
	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	// g2 is in the north west quadrant, whose enemy stays:
	h.expectSpriteState("g1 cleared NE", rkSlotNW, 0x09, 0x09)
	h.expectSpriteState("g1 cleared NE", rkSlotNE, 0x00, 0x09)

	// g2 walks into the north east quadrant:
	h.moveLink(1, 0x0150, 0x0050)
	h.settleRoom(skSettleFrames)
	h.expectSpriteState("g2 in NE", rkSlotNE, 0x00, 0x06)
	h.expectSpriteState("g2 in NE", rkSlotNW, 0x09, 0x09)
}

func TestRoomKillScope(t *testing.T) {
	for _, c := range []struct {
		tag, q, bit uint8
		ok          bool
	}{
		{0x01, 0, 0x01, true}, {0x01, 1, 0, false},
		{0x04, 3, 0x08, true}, {0x2C, 3, 0x08, true},
		{0x05, 2, 0x04, true}, {0x05, 1, 0, false},
		{0x06, 3, 0x08, true}, {0x07, 1, 0x02, true}, {0x08, 1, 0, false},
		{0x09, 2, 0x04, true}, {0x31, 1, 0x02, true},
		{0x0A, 2, roomKillRoom, true}, {0x32, 0, roomKillRoom, true},
		{0x26, 3, 0x08, true}, {0x26, 0, 0, false},
		{0x17, 0, 0, false}, {0x00, 0, 0, false},
	} {
		bit, ok := roomKillScope(c.tag, c.q)
		if ok != c.ok || ok && bit != c.bit {
			t.Errorf("roomKillScope($%02x, %d) = $%02x, %v; expected $%02x, %v", c.tag, c.q, bit, ok, c.bit, c.ok)
		}
	}
}
