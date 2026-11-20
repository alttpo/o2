package alttp

import (
	"testing"
)

const (
	rdChestSlot = 2
	rdChestPos  = uint16(0x0A28) // a position in $7E2000
	rdDoorSlot  = 1
	rdDoorPos   = uint16(0x0C1E)
	rdTagPull   = uint8(0x14) // RoomTag_PullSwitchDoor
	rdTagHold   = uint8(0x16) // RoomTag_SwitchTrigger_HoldDoor
)

func wram16(e []byte, offs uint32) uint16 { return uint16(e[offs]) | uint16(e[offs+1])<<8 }

func setWram16(e []byte, offs uint32, v uint16) {
	e[offs] = uint8(v)
	e[offs+1] = uint8(v >> 8)
}

// placeChest puts a closed chest (small, or big when big is set) in chest slot i at pos, as RoomDraw_Chest and
// Underworld_SetChestAttributes would.
func (h *skScenario) placeChest(p int, i int, pos uint16, big bool) {
	e := h.gs[p].e.WRAM[:]
	setWram16(e, uint32(roomChestPos)+uint32(i<<1), pos)
	attr := 0x12000 + uint32(pos>>1)
	v := uint16(0x5858) + uint16(i)*0x0101
	setWram16(e, attr, v)
	setWram16(e, attr+0x40, v)
	if big {
		setWram16(e, attr+0x42, v)
		setWram16(e, attr+0x80, v)
		setWram16(e, attr+0x82, v)
	}
	for _, o := range []uint32{0x00, 0x02, 0x80, 0x82} {
		setWram16(e, uint32(pos)+0x2000+o, 0x1111) // closed chest tiles
	}
}

// openChest does what OpenItemChest does to the room state when the player opens chest i.
func (h *skScenario) openChest(p int, i int) {
	e := h.gs[p].e.WRAM[:]
	e[roomFlagsOffs] |= 1 << i
	pos := wram16(e, uint32(roomChestPos)+uint32(i<<1))
	setWram16(e, 0x12000+uint32(pos>>1), 0x2727)
}

// placeKeyDoor puts a locked small key door in door slot k at pos, as Underworld_LoadSingleDoorAttribute would.
func (h *skScenario) placeKeyDoor(p int, k int, pos uint16) {
	e := h.gs[p].e.WRAM[:]
	o := uint32(k << 1)
	setWram16(e, uint32(roomDoorTypes)+o, 0x001C)
	setWram16(e, uint32(roomDoorPos)+o, pos)
	setWram16(e, uint32(roomDoorDirs)+o, 0x0000)
	v := uint16(0xF0|k) * 0x0101
	setWram16(e, 0x12000+uint32(pos>>1)+0x41, v)
	setWram16(e, 0x12000+uint32(pos>>1)+0x81, v)
}

// placeCrackedWall puts a bombable wall (door type $2E) in door slot k at pos, as Underworld_LoadSingleDoorAttribute
// would.
func (h *skScenario) placeCrackedWall(p int, k int, pos uint16) {
	h.placeKeyDoor(p, k, pos)
	setWram16(h.gs[p].e.WRAM[:], uint32(roomDoorTypes)+uint32(k<<1), 0x002E)
}

// unlockKeyDoor does what UnlockKeyDoor_Main leaves behind once the door slot k is open.
func (h *skScenario) unlockKeyDoor(p int, k int) {
	e := h.gs[p].e.WRAM[:]
	e[roomDoorsOffs] |= roomDoorBit(k)
	pos := wram16(e, uint32(roomDoorPos)+uint32(k<<1))
	setWram16(e, 0x12000+uint32(pos>>1)+0x41, 0)
	setWram16(e, 0x12000+uint32(pos>>1)+0x81, 0)
}

// settleSubmodules runs frames, finishing submodules $04 (key door) and $05 (shutters) like the game would and
// optionally running a hold switch tag stub.
func (h *skScenario) settleSubmodules(n int, holdTag func(i int)) {
	for f := 0; f < n; f++ {
		h.frame()
		for i, gs := range h.gs {
			e := gs.e.WRAM[:]
			if e[skModule] == 0x07 && (e[skSubModule] == 0x04 || e[skSubModule] == 0x05) {
				e[skSubModule] = 0x00
			}
			if holdTag != nil {
				holdTag(i)
			}
		}
	}
}

func TestRoomDoorsScenario_ChestOpened(t *testing.T) {
	h := newSSScenario(t, 2)
	for i := range h.gs {
		h.loadRoomState(i, rmRoom, 0, 0)
		h.placeChest(i, rdChestSlot, rdChestPos, false)
	}
	h.settleSubmodules(skSettleFrames, nil)

	stripes := wram16(h.gs[1].e.WRAM[:], 0x1000)
	h.openChest(0, rdChestSlot)
	h.settleSubmodules(skSettleFrames, nil)

	e := h.gs[1].e.WRAM[:]
	if f := e[roomFlagsOffs]; f != 1<<rdChestSlot {
		t.Errorf("g2: expected chest flag $%02x; got $%02x", 1<<rdChestSlot, f)
	}
	attr := 0x12000 + uint32(rdChestPos>>1)
	if a, b := wram16(e, attr), wram16(e, attr+0x40); a != 0x2727 || b != 0x2727 {
		t.Errorf("g2: expected chest attributes $2727 so it cannot be opened again; got $%04x $%04x", a, b)
	}
	for k, o := range []uint32{0x00, 0x80, 0x02, 0x82} {
		if v := wram16(e, uint32(rdChestPos)+0x2000+o); v != roomOpenChestTiles[k] {
			t.Errorf("g2: expected open chest tile $%04x at +$%02x; got $%04x", roomOpenChestTiles[k], o, v)
		}
	}
	if n := wram16(e, 0x1000); n != stripes+0x18 {
		t.Errorf("g2: expected stripe buffer to grow by $18; $%04x -> $%04x", stripes, n)
	}
	x := 0x1000 + uint32(stripes)
	if hdr, want := wram16(e, x+2), roomStripeAddr(rdChestPos); hdr != want {
		t.Errorf("g2: expected first stripe header $%04x; got $%04x", want, hdr)
	}
	if end := wram16(e, x+0x1A); end != 0xFFFF {
		t.Errorf("g2: expected stripe terminator; got $%04x", end)
	}
	if e[0x14] != 1 {
		t.Errorf("g2: expected $14 = 1 to upload the stripes")
	}
}

func TestRoomDoorsScenario_BigChestNotDrawn(t *testing.T) {
	h := newSSScenario(t, 2)
	for i := range h.gs {
		h.loadRoomState(i, rmRoom, 0, 0)
		h.placeChest(i, rdChestSlot, rdChestPos, true)
	}
	h.settleSubmodules(skSettleFrames, nil)

	h.openChest(0, rdChestSlot)
	h.settleSubmodules(skSettleFrames, nil)

	e := h.gs[1].e.WRAM[:]
	if f := e[roomFlagsOffs]; f != 1<<rdChestSlot {
		t.Errorf("g2: expected chest flag $%02x; got $%02x", 1<<rdChestSlot, f)
	}
	if v := wram16(e, uint32(rdChestPos)+0x2000); v != 0x1111 {
		t.Errorf("g2: expected big chest not to be drawn; got tile $%04x", v)
	}
}

func TestRoomDoorsScenario_HiddenChestsWait(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, 0, 0)
	h.loadRoomState(1, rmRoom, 0x27, 0) // RoomTag_TriggerChest: chests hidden until revealed
	h.placeChest(0, rdChestSlot, rdChestPos, false)
	h.placeChest(1, rdChestSlot, rdChestPos, false)
	h.settleSubmodules(skSettleFrames, nil)

	h.openChest(0, rdChestSlot)
	h.settleSubmodules(skSettleFrames, nil)
	if f := h.gs[1].e.WRAM[roomFlagsOffs]; f != 0 {
		t.Errorf("g2: expected chest flags to wait for the chests to be revealed; got $%02x", f)
	}

	// revealed:
	h.gs[1].e.WRAM[0xAE] = 0
	h.settleSubmodules(skSettleFrames, nil)
	if f := h.gs[1].e.WRAM[roomFlagsOffs]; f != 1<<rdChestSlot {
		t.Errorf("g2: expected chest flag $%02x once revealed; got $%02x", 1<<rdChestSlot, f)
	}
}

func TestRoomDoorsScenario_KeyDoorUnlocked(t *testing.T) {
	h := newSSScenario(t, 2)
	for i := range h.gs {
		h.loadRoomState(i, rmRoom, 0, 0)
		h.placeKeyDoor(i, rdDoorSlot, rdDoorPos)
		h.gs[i].e.WRAM[skCurrentKeys] = 1
	}
	h.settleSubmodules(skSettleFrames, nil)

	h.unlockKeyDoor(0, rdDoorSlot)
	h.gs[0].e.WRAM[skCurrentKeys] = 0
	unlocked := false
	for f := 0; f < skSettleFrames && !unlocked; f++ {
		h.frame()
		e := h.gs[1].e.WRAM[:]
		if e[skSubModule] == 0x04 {
			unlocked = true
			if v, want := wram16(e, 0x068E), (rdDoorPos>>1)+0x41; v != want {
				t.Errorf("g2: expected $068E = $%04x; got $%04x", want, v)
			}
			if v := wram16(e, 0x0690); v != 0 {
				t.Errorf("g2: expected $0690 = 0; got $%04x", v)
			}
			// what UnlockKeyDoor_Main does:
			h.unlockKeyDoor(1, rdDoorSlot)
			e[skSubModule] = 0
		}
	}
	if !unlocked {
		t.Fatalf("g2: expected the key door to be unlocked in submodule $04")
	}

	// nothing more happens afterwards; the small key sync spends g2's key:
	h.settleSubmodules(skSettleFrames*2, nil)
	if s := h.gs[1].e.WRAM[skSubModule]; s != 0 {
		t.Errorf("g2: expected no more door updates; submodule $%02x", s)
	}
	h.expectKeys("door unlocked", 0, 0)
}

func TestRoomDoorsScenario_CrackedWallOpened(t *testing.T) {
	h := newSSScenario(t, 2)
	for i := range h.gs {
		h.loadRoomState(i, rmRoom, 0, 0)
		h.placeCrackedWall(i, rdDoorSlot, rdDoorPos)
		h.gs[i].e.WRAM[skCurrentKeys] = 1
	}
	h.settleSubmodules(skSettleFrames, nil)

	// g1 bombs the wall open (OpenCrackedDoor leaves the same state behind as UnlockKeyDoor_Main):
	h.unlockKeyDoor(0, rdDoorSlot)
	opened := false
	for f := 0; f < skSettleFrames && !opened; f++ {
		h.frame()
		e := h.gs[1].e.WRAM[:]
		if e[skSubModule] == 0x04 {
			t.Fatalf("g2: expected a cracked wall not to be unlocked like a key door")
		}
		if e[skSubModule] == 0x09 {
			opened = true
			if v, want := wram16(e, 0x068E), (rdDoorPos>>1)+0x41; v != want {
				t.Errorf("g2: expected $068E = $%04x; got $%04x", want, v)
			}
			if v := e[0x012F]; v != 0x1B {
				t.Errorf("g2: expected SFX3 $1B; got $%02x", v)
			}
			// what OpenCrackedDoor does:
			h.unlockKeyDoor(1, rdDoorSlot)
			e[skSubModule] = 0
		}
	}
	if !opened {
		t.Fatalf("g2: expected the cracked wall to be opened in submodule $09")
	}

	h.settleSubmodules(skSettleFrames*2, nil)
	if s := h.gs[1].e.WRAM[skSubModule]; s != 0 {
		t.Errorf("g2: expected no more door updates; submodule $%02x", s)
	}
	// no key is used for a wall:
	h.expectKeys("wall opened", 1, 1)
}

func TestRoomDoorsScenario_KeyDoorAlreadyOpen(t *testing.T) {
	h := newSSScenario(t, 2)
	for i := range h.gs {
		h.loadRoomState(i, rmRoom, 0, 0)
		h.placeKeyDoor(i, rdDoorSlot, rdDoorPos)
	}
	h.settleSubmodules(skSettleFrames, nil)

	// g2 opened it by other means (e.g. it was open in its SRAM) but its door bit is not set:
	e := h.gs[1].e.WRAM[:]
	setWram16(e, 0x12000+uint32(rdDoorPos>>1)+0x41, 0)
	h.unlockKeyDoor(0, rdDoorSlot)
	for f := 0; f < skSettleFrames; f++ {
		h.frame()
		if e[skSubModule] == 0x04 {
			t.Fatalf("g2: expected an open door not to be unlocked again")
		}
	}
}

func TestRoomDoorsScenario_PullSwitch(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rdTagPull, 0)
	h.loadRoomState(1, rmRoom, rdTagPull, 0)
	h.settleSubmodules(skSettleFrames, nil)

	h.gs[0].e.WRAM[roomPullOffs] = 1
	h.settleSubmodules(skSettleFrames, nil)
	if v := h.gs[1].e.WRAM[roomPullOffs]; v != 1 {
		t.Errorf("g2: expected $0642 = 1 after g1 pulled the switch; got $%02x", v)
	}

	// the tag takes it back; it is not set again:
	h.gs[1].e.WRAM[roomPullOffs] = 0
	h.settleSubmodules(skSettleFrames, nil)
	if v := h.gs[1].e.WRAM[roomPullOffs]; v != 0 {
		t.Errorf("g2: expected $0642 to be set only once; got $%02x", v)
	}
}

func TestRoomDoorsScenario_PullSwitchBeforeEntering(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, rdTagPull, 0)
	h.loadRoomState(1, rmOtherRoom, rdTagPull, 0)
	h.settleSubmodules(skSettleFrames, nil)

	h.gs[0].e.WRAM[roomPullOffs] = 1
	h.settleSubmodules(skSettleFrames, nil)

	h.loadRoomState(1, rmRoom, rdTagPull, 0)
	h.gs[1].e.WRAM[roomPullOffs] = 0
	h.settleSubmodules(skSettleFrames*2, nil)
	if v := h.gs[1].e.WRAM[roomPullOffs]; v != 0 {
		t.Errorf("g2: expected a pull made before entering the room not to apply; got $%02x", v)
	}
}

func TestRoomDoorsScenario_HoldSwitch(t *testing.T) {
	h := newSSScenario(t, 2)
	for i := range h.gs {
		h.loadRoomState(i, rmRoom, rdTagHold, 0)
		h.gs[i].e.WRAM[roomHoldOffs] = 0
	}
	// RoomTag_SwitchTrigger_HoldDoor stub: doors open while the player presses the switch or $0646 is set:
	pressing := []bool{false, false}
	holdTag := func(i int) {
		e := h.gs[i].e.WRAM[:]
		if pressing[i] || e[roomHoldOffs] != 0 {
			e[roomDoorOffs] = 0
		} else {
			e[roomDoorOffs] = 1
		}
	}
	h.settleSubmodules(skSettleFrames, holdTag)
	h.expectDoors("initial", 1, 1)

	pressing[0] = true
	h.settleSubmodules(skSettleFrames, holdTag)
	h.expectDoors("g1 presses", 0, 0)
	if v := h.gs[1].e.WRAM[roomHoldOffs]; v != roomHoldRemote {
		t.Errorf("g2: expected $0646 = $%02x; got $%02x", roomHoldRemote, v)
	}

	pressing[0] = false
	h.settleSubmodules(skSettleFrames, holdTag)
	h.expectDoors("g1 released", 1, 1)
	if v := h.gs[1].e.WRAM[roomHoldOffs]; v != 0 {
		t.Errorf("g2: expected $0646 cleared; got $%02x", v)
	}

	// g2 presses while being held open by g1, then g1 releases; g2 keeps it open by itself:
	pressing[0] = true
	h.settleSubmodules(skSettleFrames, holdTag)
	pressing[1] = true
	pressing[0] = false
	h.settleSubmodules(skSettleFrames*2, holdTag)
	h.expectDoors("g2 presses", 0, 0)
	pressing[1] = false
	h.settleSubmodules(skSettleFrames*2, holdTag)
	h.expectDoors("both released", 1, 1)
}

// A player entering a push block door room (e.g. Ganon's Tower room $8B) through the trap door gets the solved block
// from the players already there while the doors close behind it; the doors are opened once they are done.
func TestRoomDoorsScenario_BlockDoorReopensAfterEntering(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, 0x0C, 0) // NE block door
	h.loadRoomState(1, rmRoom, 0x0C, 0)
	// g1 pushed the block and its doors are open:
	h.gs[0].e.WRAM[roomBlockOffs] = 1
	h.gs[0].e.WRAM[roomDoorOffs] = 0
	h.settleSubmodules(skSettleFrames, nil)
	h.expectDoors("g2 entered", 0, 1)

	// g2's block was moved by sync while its doors were closing, outside the tag's quadrant:
	h.gs[1].e.WRAM[roomBlockOffs] = 1
	h.settleSubmodules(skSettleFrames, nil)
	h.expectDoors("g2 got the solved block", 0, 0)

	// alone in the room nothing is forced:
	h.loadRoomState(0, rmOtherRoom, 0x0C, 0)
	h.loadRoomState(1, rmRoom, 0x0C, 0)
	h.gs[1].e.WRAM[roomBlockOffs] = 1
	h.settleSubmodules(skSettleFrames*2, nil)
	h.expectDoors("g2 alone", 1, 1)
}

// intraroomTransition does what an intra-room transition through a trap door does: UnderworldTransition_Intraroom_
// TriggerShutters clears $0641 (ResetThenCacheRoomEntryProperties) and closes the shutters behind Link.
func (h *skScenario) intraroomTransition(i int) {
	e := h.gs[i].e
	e.WRAM[skSubModule] = 0x01
	h.settleSubmodules(2, nil)
	e.WRAM[roomBlockOffs] = 0
	e.WRAM[roomDoorOffs] = 1
	e.WRAM[0x068E] = 0
	e.WRAM[0x0690] = 0
	e.WRAM[skSubModule] = 0x05
}

// Coming back through the trap door of a push block door room (e.g. Ganon's Tower room $8B) clears $0641 and closes the
// shutters while the block stays pushed (here by another player in another quadrant), so RoomTag_OperateBlockDoor
// keeps Link locked in; $0641 is restored and the doors are opened again.
func TestRoomDoorsScenario_BlockDoorReopensAfterIntraroomTransition(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, 0x0C, 0) // NE block door
	h.loadRoomState(1, rmRoom, 0x0C, 0)
	for i := range h.gs {
		h.gs[i].e.WRAM[roomBlockOffs] = 1
		h.gs[i].e.WRAM[roomDoorOffs] = 0
	}
	h.settleSubmodules(skSettleFrames, nil)
	h.expectDoors("block pushed", 0, 0)

	h.intraroomTransition(1)
	h.settleSubmodules(skSettleFrames*2, nil)
	h.expectDoors("g2 came back through the trap door", 0, 0)
	if v := h.gs[1].e.WRAM[roomBlockOffs]; v != 1 {
		t.Errorf("g2: expected $0641 = 1; got $%02x", v)
	}

	// also alone in the room, since the block cannot be pushed again:
	h.loadRoomState(0, rmOtherRoom, 0x0C, 0)
	h.intraroomTransition(1)
	h.settleSubmodules(skSettleFrames*2, nil)
	h.expectDoors("g2 alone came back through the trap door", 1, 0)

	// a block that was never pushed is left alone (g2 re-enters the room):
	h.loadRoomState(1, rmOtherRoom+1, 0x0C, 0)
	h.settleSubmodules(skSettleFrames, nil)
	h.loadRoomState(1, rmRoom, 0x0C, 0)
	h.gs[1].e.WRAM[roomBlockOffs] = 0
	h.settleSubmodules(skSettleFrames, nil)
	h.intraroomTransition(1)
	h.settleSubmodules(skSettleFrames*2, nil)
	h.expectDoors("g2 block not pushed", 1, 1)
	if v := h.gs[1].e.WRAM[roomBlockOffs]; v != 0 {
		t.Errorf("g2: expected $0641 = 0; got $%02x", v)
	}
}

// Swamp Palace room $37 fills with water when its switch is pulled (RoomTag_WaterOn). The flood only finishes with the
// camera in the quadrant of the switch, so a pull is only replayed once Link is in the quadrant of the player who
// pulled it; otherwise the game hangs in submodule $0C.
func TestRoomDoorsScenario_WaterSwitchWaitsForQuadrant(t *testing.T) {
	h := newSSScenario(t, 2)
	h.loadRoomState(0, rmRoom, 0, 0)
	h.loadRoomState(1, rmRoom, 0, 0)
	for i := range h.gs {
		h.gs[i].e.WRAM[0xAF] = 0x19 // RoomTag_WaterOn
	}
	// g1 at the switch in the north half; g2 in the south half:
	h.setScroll(0, 0x0E78, 0x0C80, 0x0E00, 0x0C00)
	h.setScroll(1, 0x0E78, 0x0D80, 0x0E00, 0x0D00)
	h.settleSubmodules(skSettleFrames, nil)

	h.gs[0].e.WRAM[roomPullOffs] = 1
	h.settleSubmodules(skSettleFrames*2, nil)
	if v := h.gs[1].e.WRAM[roomPullOffs]; v != 0 {
		t.Errorf("g2: expected the pull to wait while in another quadrant; $0642 = $%02x", v)
	}

	// g1 walks down to g2; what counts is where the switch was pulled, not where g1 is now:
	h.setScroll(0, 0x0E78, 0x0D80, 0x0E00, 0x0D00)
	h.settleSubmodules(skSettleFrames*2, nil)
	if v := h.gs[1].e.WRAM[roomPullOffs]; v != 0 {
		t.Errorf("g2: expected the pull to wait for the switch's quadrant, not g1's; $0642 = $%02x", v)
	}

	// g2 walks up into the switch's quadrant:
	h.setScroll(1, 0x0E78, 0x0CC0, 0x0E00, 0x0C00)
	h.settleSubmodules(skSettleFrames*2, nil)
	if v := h.gs[1].e.WRAM[roomPullOffs]; v != 1 {
		t.Errorf("g2: expected the pull once in the same quadrant; $0642 = $%02x", v)
	}
}

// The fill and drain tags set $0403 bit 3 when done; sharing it makes the room load filled (or drained) for the other
// players next time. The water gate tags check the bit to decide whether to open, so it is not shared there.
func TestRoomDoorsScenario_WaterFlag(t *testing.T) {
	for _, tc := range []struct {
		tag  uint8
		want uint8
	}{
		{0x19, roomFlagWater}, // RoomTag_WaterOn
		{0x18, roomFlagWater}, // RoomTag_WaterOff
		{0x1A, 0},             // RoomTag_WaterGate
	} {
		h := newSSScenario(t, 2)
		h.loadRoomState(0, rmRoom, 0, 0)
		h.loadRoomState(1, rmRoom, 0, 0)
		for i := range h.gs {
			h.gs[i].e.WRAM[0xAF] = tc.tag
		}
		h.settleSubmodules(skSettleFrames, nil)
		h.gs[0].e.WRAM[roomFlagsOffs] |= roomFlagWater
		h.gs[0].e.WRAM[0xAF] = 0
		h.settleSubmodules(skSettleFrames, nil)
		if f := h.gs[1].e.WRAM[roomFlagsOffs]; f != tc.want {
			t.Errorf("tag $%02x: g2: expected flags $%02x; got $%02x", tc.tag, tc.want, f)
		}
	}
}
