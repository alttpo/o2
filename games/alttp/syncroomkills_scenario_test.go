package alttp

import "testing"

const (
	rkSlotEnemy = uint32(1)      // an enemy still alive
	rkChestPos  = uint16(0x0A1C) // a hidden chest's position in $7E2000
	rkTagChest  = uint8(0x29)    // reveal chests once the north west quadrant is clear
	rkTagDoors  = uint8(0x0A)    // trap doors open once the room is clear
	rkTagBlock  = uint8(0x26)    // RoomTag_KillRoomBlock
	rkFloorTile = uint16(0x0123) // the floor under the hidden chest
	rkFloorAttr = uint16(0x0000)
)

// loadKillRoom puts the player in a freshly loaded room with the kill trigger tag, Link at (x, y), an enemy that stays
// alive and a hidden chest.
func (h *skScenario) loadKillRoom(i int, tag uint8, x, y uint16) {
	h.loadRoomState(i, rmRoom, tag, roomFlagKey0)
	e := h.gs[i].e
	e.WRAM[rmOtherSlot+0x0DD0] = 0
	setWram16(e.WRAM[:], 0x22, x)
	setWram16(e.WRAM[:], 0x20, y)
	e.WRAM[0x0DD0+rkSlotEnemy] = 0x09
	e.WRAM[0x0E20+rkSlotEnemy] = 0x08
	setWram16(e.WRAM[:], 0x0496, 2)
	setWram16(e.WRAM[:], uint32(roomChestPos), rkChestPos)
	for _, o := range roomChestTileOffs {
		setWram16(e.WRAM[:], 0x2000+uint32(rkChestPos+o), rkFloorTile)
	}
	setWram16(e.WRAM[:], 0x12000+uint32(rkChestPos>>1), rkFloorAttr)
	e.WRAM[0x14] = 0
	setWram16(e.WRAM[:], 0x1000, 0)
}

// revealChest does what RoomTag_OperateChestReveal does to the WRAM it checks.
func (h *skScenario) revealChest(i int) {
	e := h.gs[i].e
	e.WRAM[0xAE] = 0
	for t, o := range roomChestTileOffs {
		setWram16(e.WRAM[:], 0x2000+uint32(rkChestPos+o), roomClosedChestTiles[t])
	}
}

func (h *skScenario) expectChest(when string, expected ...bool) {
	h.t.Helper()
	for i, x := range expected {
		e := h.gs[i].e
		revealed := true
		for t, o := range roomChestTileOffs {
			if wram16(e.WRAM[:], 0x2000+uint32(rkChestPos+o)) != roomClosedChestTiles[t] {
				revealed = false
			}
		}
		if revealed != x {
			h.t.Errorf("%s: g%d: expected chest revealed = %v; got %v", when, i+1, x, revealed)
		}
	}
}

func (h *skScenario) expectTag(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		if actual := h.gs[i].e.WRAM[0xAE]; actual != x {
			h.t.Errorf("%s: g%d: expected tag $%02x; got $%02x", when, i+1, x, actual)
		}
	}
}

func (h *skScenario) expectEnemiesAlive(when string) {
	h.t.Helper()
	for i, gs := range h.gs {
		if gs.e.WRAM[0x0DD0+rkSlotEnemy] != 0x09 {
			h.t.Errorf("%s: g%d: expected the enemy to be left alone", when, i+1)
		}
	}
}

func TestRoomKillsScenario_ChestReveal(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadKillRoom(0, rkTagChest, 0x0050, 0x0050)
	h.loadKillRoom(1, rkTagChest, 0x0150, 0x0150)
	h.gs[1].e.WRAM[0x0DD0+rkSlotEnemy] = 0x09
	h.settleRoom(skSettleFrames)
	h.expectChest("initial", false, false)

	// g1's enemies in the north west quadrant are dead and the tag reveals the chest:
	h.revealChest(0)
	h.settleRoom(skSettleFrames)
	h.expectTag("g1 revealed", 0, 0)
	h.expectChest("g1 revealed", true, true)
	h.expectEnemiesAlive("g1 revealed")
	e := h.gs[1].e
	if a := wram16(e.WRAM[:], 0x12000+uint32(rkChestPos>>1)); a != 0x5858 {
		t.Errorf("g2: expected chest 0's attributes $5858; got $%04x", a)
	}
	if n, s := wram16(e.WRAM[:], 0x1000), wram16(e.WRAM[:], 0x1002); n != 0x18 || s != roomStripeAddr(rkChestPos) {
		t.Errorf("g2: expected one chest's VRAM stripes; got $1000 = $%04x, header $%04x", n, s)
	}

	// nothing more happens:
	h.settleRoom(skSettleFrames * 4)
	if rs := &h.gs[1].g.roomState; rs.inflight != nil || rs.fired != 0x01 {
		t.Errorf("g2: expected the kill trigger applied once; fired $%02x", rs.fired)
	}
}

func TestRoomKillsScenario_DoorsWhenEnteringLater(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadKillRoom(0, rkTagDoors, 0x0050, 0x0050)
	h.loadKillRoom(1, rkTagDoors, 0x0050, 0x0050)
	h.inRoom(1, rmOtherRoom)
	h.settleRoom(skSettleFrames)

	// g1 clears the room and the tag opens the trap doors:
	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)

	// g2 enters afterwards; the trap door change is older than its visit, but the kill trigger fired:
	h.loadKillRoom(1, rkTagDoors, 0x0050, 0x0050)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g2 entered", 0, 0)
	h.expectTag("g2 entered", rkTagDoors, rkTagDoors)
	h.expectEnemiesAlive("g2 entered")
}

func TestRoomKillsScenario_Block(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadKillRoom(0, rkTagBlock, 0x0150, 0x0150)
	h.loadKillRoom(1, rkTagBlock, 0x0050, 0x0050)
	h.settleRoom(skSettleFrames)

	h.gs[0].e.WRAM[0xAE] = 0
	h.settleRoom(skSettleFrames)
	h.expectTag("g1 cleared", 0, 0)
	h.expectChest("g1 cleared", false, false)
	h.expectEnemiesAlive("g1 cleared")
}

func TestRoomKillsScenario_DoorsInAnotherQuadrant(t *testing.T) {
	h := newSSScenario(t, 2)
	// the north west tag; g1 is in the north east quadrant, where the tag does not check:
	h.loadKillRoom(0, 0x01, 0x0150, 0x0050)
	h.loadKillRoom(1, 0x01, 0x0050, 0x0050)
	h.settleRoom(skSettleFrames)
	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	if rs := &h.gs[0].g.roomState; rs.fired != 0 {
		t.Errorf("g1: expected no kill trigger; fired $%02x", rs.fired)
	}
}

func TestRoomKillTagFires(t *testing.T) {
	for _, c := range []struct {
		tag, q uint8
		ok     bool
	}{
		{0x01, 0, true}, {0x01, 1, false},
		{0x04, 3, true}, {0x2C, 3, true},
		{0x05, 2, true}, {0x05, 1, false},
		{0x06, 3, true}, {0x07, 1, true}, {0x08, 1, false},
		{0x09, 2, true}, {0x31, 1, true},
		{0x0A, 2, true}, {0x32, 0, true},
		{0x26, 3, true}, {0x26, 0, false},
		{0x17, 0, false}, {0x00, 0, false},
	} {
		if ok := roomKillTagFires(c.tag, c.q); ok != c.ok {
			t.Errorf("roomKillTagFires($%02x, %d) = %v; expected %v", c.tag, c.q, ok, c.ok)
		}
	}
}
