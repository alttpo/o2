package alttp

import (
	"testing"
)

const (
	rmRoom      = uint16(0x0073) // a Desert Palace room
	rmOtherRoom = uint16(0x0074)
	rmTagToggle = uint8(0x17) // RoomTag_SwitchTrigger_ToggleDoor
	rmTagHold   = uint8(0x16) // RoomTag_SwitchTrigger_HoldDoor
	rmBonkSlot  = uint32(3)
	rmOtherSlot = uint32(5)
)

// loadRoomState puts the player in a freshly loaded room with the given room tag, closed trap doors, the given room
// flags and a bonk key (key index 0) in slot rmBonkSlot if not yet taken, plus an unrelated sprite in rmOtherSlot.
func (h *skScenario) loadRoomState(i int, room uint16, tag uint8, flags uint8) {
	h.inRoom(i, room)
	e := h.gs[i].e
	e.WRAM[0xAE] = tag
	e.WRAM[0xAF] = 0
	e.WRAM[roomFlagsOffs] = flags
	e.WRAM[roomDoorOffs] = 1
	e.WRAM[roomDoorOffs+1] = 0
	for k := uint32(0); k < 0x10; k++ {
		e.WRAM[0x0DD0+k] = 0
		e.WRAM[0x0E20+k] = 0
		e.WRAM[0x0DC0+k] = 0
		e.WRAM[0x0CBA+k] = 0
	}
	if flags&roomFlagKey0 == 0 {
		e.WRAM[0x0DD0+rmBonkSlot] = 0x09
		e.WRAM[0x0E20+rmBonkSlot] = bonkItemSprite
		e.WRAM[0x0DC0+rmBonkSlot] = bonkKeySubtype
		e.WRAM[0x0CBA+rmBonkSlot] = 0
	}
	// a key-carrying enemy that must be left alone:
	e.WRAM[0x0DD0+rmOtherSlot] = 0x09
	e.WRAM[0x0E20+rmOtherSlot] = 0x41
	e.WRAM[0x0CBA+rmOtherSlot] = 0x01
}

// operateDoors does what a room tag does when it opens (door = 0) or closes (door = 1) the trap doors.
func (h *skScenario) operateDoors(i int, door uint8) {
	e := h.gs[i].e
	e.WRAM[roomDoorOffs] = door
	e.WRAM[0x068E] = 0
	e.WRAM[0x0690] = 0
	e.WRAM[skSubModule] = 0x05
}

// grantBonkKey does what BonkKey_GrantKey does.
func (h *skScenario) grantBonkKey(i int) {
	e := h.gs[i].e
	e.WRAM[0x0DD0+rmBonkSlot] = 0
	e.WRAM[roomFlagsOffs] |= roomFlagKey0
}

// settleRoom runs frames, finishing the shutter submodule ($05) like the game would.
func (h *skScenario) settleRoom(n int) {
	for f := 0; f < n; f++ {
		h.frame()
		for _, gs := range h.gs {
			if gs.e.WRAM[skModule] == 0x07 && gs.e.WRAM[skSubModule] == 0x05 {
				gs.e.WRAM[skSubModule] = 0x00
			}
		}
	}
}

func (h *skScenario) expectDoors(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		if actual := h.gs[i].e.WRAM[roomDoorOffs]; actual != x {
			h.t.Errorf("%s: g%d: expected trap doors $%02x; got $%02x", when, i+1, x, actual)
		}
	}
}

func (h *skScenario) expectRoomFlags(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		if actual := h.gs[i].e.WRAM[roomFlagsOffs]; actual != x {
			h.t.Errorf("%s: g%d: expected room flags $%02x; got $%02x", when, i+1, x, actual)
		}
	}
}

func (h *skScenario) expectBonkKey(when string, expected ...bool) {
	h.t.Helper()
	for i, x := range expected {
		e := h.gs[i].e
		if actual := e.WRAM[0x0DD0+rmBonkSlot] != 0; actual != x {
			h.t.Errorf("%s: g%d: expected bonk key present = %v; got %v", when, i+1, x, actual)
		}
		if e.WRAM[0x0DD0+rmOtherSlot] == 0 {
			h.t.Errorf("%s: g%d: expected unrelated sprite to be left alone", when, i+1)
		}
	}
}

func TestRoomStateScenario_ToggleDoors(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rmTagToggle, 0)
	h.loadRoomState(1, rmRoom, rmTagToggle, 0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("initial", 1, 1)

	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g1 opened", 0, 0)

	h.operateDoors(1, 1)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g2 closed", 1, 1)

	// nothing flips back and forth afterwards:
	h.settleRoom(skSettleFrames * 4)
	h.expectDoors("settled", 1, 1)
}

func TestRoomStateScenario_ShuttersAnimate(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rmTagToggle, 0)
	h.loadRoomState(1, rmRoom, rmTagToggle, 0)
	h.settleRoom(skSettleFrames)

	h.gs[1].e.WRAM[0x068E] = 0x12
	h.gs[1].e.WRAM[0x0690] = 0x34
	h.operateDoors(0, 0)
	for f := 0; f < skSettleFrames; f++ {
		h.frame()
		if h.gs[0].e.WRAM[skSubModule] == 0x05 {
			h.gs[0].e.WRAM[skSubModule] = 0
		}
		if h.gs[1].e.WRAM[skSubModule] == 0x05 {
			break
		}
	}
	e := h.gs[1].e
	if e.WRAM[skSubModule] != 0x05 || e.WRAM[0x068E] != 0 || e.WRAM[0x0690] != 0 || e.WRAM[roomDoorOffs] != 0 {
		t.Errorf("g2: expected shutters to open in submodule $05; submodule $%02x, $068E $%02x, $0690 $%02x, $0468 $%02x",
			e.WRAM[skSubModule], e.WRAM[0x068E], e.WRAM[0x0690], e.WRAM[roomDoorOffs])
	}
}

func TestRoomStateScenario_HoldSwitchRoomNotSynced(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rmTagHold, 0)
	h.loadRoomState(1, rmRoom, rmTagHold, 0)
	h.settleRoom(skSettleFrames)

	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g1 holds switch", 0, 1)
}

func TestRoomStateScenario_OtherRoom(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rmTagToggle, 0)
	h.loadRoomState(1, rmOtherRoom, rmTagToggle, 0)
	h.settleRoom(skSettleFrames)

	h.operateDoors(0, 0)
	h.grantBonkKey(0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("other room", 0, 1)
	h.expectRoomFlags("other room", roomFlagKey0, 0)
	h.expectBonkKey("other room", false, true)
	if h.gs[1].g.players[0].Room.valid {
		t.Errorf("g2: expected room state only to be sent to players in the same room")
	}
}

func TestRoomStateScenario_EnterRoomAfterDoorsOpened(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rmTagToggle, 0)
	h.loadRoomState(1, rmOtherRoom, rmTagToggle, 0)
	h.settleRoom(skSettleFrames)

	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)

	// g2 enters g1's room later; the doors were opened before g2 got there and g2's room load closed its own:
	h.loadRoomState(1, rmRoom, rmTagToggle, 0)
	h.settleRoom(skSettleFrames * 2)
	h.expectDoors("g2 entered", 0, 1)

	// changes made while both are in the room are shared:
	h.operateDoors(0, 1)
	h.settleRoom(skSettleFrames)
	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g1 toggled again", 0, 0)
}

func TestRoomStateScenario_IntraroomTransitionForgets(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rmTagToggle, 0)
	h.loadRoomState(1, rmRoom, rmTagToggle, 0)
	h.settleRoom(skSettleFrames)

	h.operateDoors(0, 0)
	h.settleRoom(skSettleFrames)
	h.expectDoors("g1 opened", 0, 0)

	// g2 moves to another quadrant; the game opens and closes the shutters behind it:
	e := h.gs[1].e
	e.WRAM[skSubModule] = 0x01
	h.settleRoom(2)
	e.WRAM[roomDoorOffs] = 1
	e.WRAM[skSubModule] = 0x05
	h.settleRoom(skSettleFrames * 2)
	h.expectDoors("g2 changed quadrant", 0, 1)
}

func TestRoomStateScenario_BonkKey(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, 0, 0)
	h.loadRoomState(1, rmRoom, 0, 0)
	h.settleRoom(skSettleFrames)
	h.expectBonkKey("initial", true, true)

	h.grantBonkKey(0)
	h.settleRoom(skSettleFrames)
	h.expectRoomFlags("g1 took key", roomFlagKey0, roomFlagKey0)
	h.expectBonkKey("g1 took key", false, false)
	h.expectDoors("g1 took key", 1, 1)
}

func TestRoomStateScenario_KeyFlagsMerge(t *testing.T) {
	h := newSSScenario(t, 3)
	h.loadRoomState(0, rmRoom, 0, 0x01)
	h.loadRoomState(1, rmRoom, 0, 0x02)
	h.loadRoomState(2, rmRoom, 0, 0)
	h.settleRoom(skSettleFrames)

	h.gs[0].e.WRAM[roomFlagsOffs] |= roomFlagKey0
	h.gs[1].e.WRAM[roomFlagsOffs] |= roomFlagKey1
	h.settleRoom(skSettleFrames * 2)
	// chest flags are left alone:
	h.expectRoomFlags("both keys taken", 0x61, 0x62, 0x60)
}

func TestRoomStateScenario_ChestsNotSynced(t *testing.T) {
	h := newSSScenario(t, 2)
	for _, gs := range h.gs {
		gs.g.SyncChests = false
	}
	h.loadRoomState(0, rmRoom, 0, 0)
	h.loadRoomState(1, rmRoom, 0, 0)
	h.settleRoom(skSettleFrames)

	h.gs[0].e.WRAM[roomFlagsOffs] |= roomFlagKey0 | roomFlagKey1
	h.settleRoom(skSettleFrames)
	h.expectRoomFlags("keys taken", 0x60, roomFlagKey0)
}
