package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"io"
	"log"
	"o2/snes"
)

// Room state synchronization: trap doors, chests, keys, key doors and switches of the current room
//
// Several pieces of underworld state only live in WRAM while the room is loaded, so the SRAM room flags ($7EF000) do
// not share them with players in the same room until somebody leaves it:
//
//   - $0468 is the trap door (shutter) state of the current room: 0 = open, 1 = closed. Room tags open or close the
//     shutters by changing it, clearing $068E/$0690 and switching to submodule $05, which animates the doors
//     (OperateShutterDoors). Room loads set it to 1 and an inter-room transition closes the shutters behind Link
//     (Module07_02_0F); an intra-room transition (submodule $01) opens and then closes them again.
//   - $0403 holds the room's chest, key and boss flags (written to $7EF000 on leaving the room). Opening chest i
//     (OpenItemChest) sets bit i (1 << i, chests 0-5). Picking up a small key or big key sprite ($E4/$E5,
//     Absorb_FlagKey) or a bonk key (sprite $3B with $0DC0 = 1, BonkKey_GrantKey, e.g. on top of a Desert Palace
//     torch) sets $40 (key index 0) or $20 (key index 1, the same bit as chest 5), the index being the key's $0CBA. Key
//     sprites remove themselves when their flag is set; bonk keys only check it when the room loads.
//   - $0401 (the high byte of $0400) has a bit set for each door opened in the room: unlocking key door slot k sets
//     $80 >> (k & 7) (UnlockKeyDoor_Main), as do bombed and dashed walls.
//   - $0642 is set by a pull switch when it is pulled (also by Swamp Palace's water switches); the pull switch door
//     ($14), pull switch exploding wall ($28) and water tags ($18-$1A) act on it.
//   - The hold switch door tag ($16, RoomTag_SwitchTrigger_HoldDoor) recomputes $0468 every frame: open while Link
//     stands on a floor switch or star tile, $0646 is set (a Cane of Somaria block on a floor switch) or $0642 is set
//     (a statue on a floor switch), closed otherwise.
//
// Message 0x1A (`room u16`, `flags u8` ($0403), `doorTs u32`, `door u8` ($0468)) and message 0x1B (`room u16`,
// `doors u8` ($0401), `switches u8` (bit 0: pressing a hold switch, bit 1: pulled a switch, bits 2/3: the switch was pulled in the east/south
// quadrant), `pulledTs u32`) are sent
// in a broadcastSector packet to the current room's sector, only once they were sampled in that room. Both are shared
// with alttpo (RoomState.as).
//
//   - Sampling: in the dungeon module only, while the room ($A0) stays the same. A baseline is taken in submodule 0. A
//     change of $0468 seen in submodule 0, $05 (shutters moving) or $17 (pressure plate) is a local change and is
//     timestamped; room tags change it during submodule 0 and switch to $05. Changes of room and intra-room
//     transitions (submodule $01) forget the state, since the game resets the shutters itself.
//   - Key flags ($40, and $20 if chests are synced since it is also chest 5's flag) are OR'ed in from remote players
//     in the same room whose state was sampled there. Bonk keys of the newly set flags are removed.
//   - Chest flags (bits 0-5, if chests are synced) are OR'ed in as well and small chests still closed here are drawn
//     open the way OpenItemChest does (tiles, $2727 attributes so they cannot be opened again, VRAM stripes), without
//     giving the item. Not while a tag that hides the room's chests until it is solved is active, and not bit 3 in
//     water rooms, whose tags use it to remember the water was let out.
//   - Key doors: a door bit set by a remote player whose slot here is a small key or big key door that is still
//     closed is unlocked the way the game does (submodule $04 with $068E pointing at the door's tile attributes),
//     without using a key; the small key sync takes care of the key count.
//   - Trap doors are last writer wins among remote players in the same room with a change made after our baseline
//     was taken (so only changes made while both players are in the room count, never stale ones from an earlier
//     visit). They are not applied in rooms with tags that compute the shutter state every frame from what the local
//     player does (hold switch doors, push block doors, torch puzzle doors); those would close again immediately.
//   - Pull switches: a pull made by a remote player after our baseline sets $0642 here once.
//   - Hold switches: while a remote player presses a hold switch, $0646 is set to $80 (no somaria block sets it to
//     more than 1), which holds the doors open; it is cleared again once nobody does. A player whose $0646 is $80 does
//     not count as pressing the switch itself, so players do not hold each other's doors open forever. A local somaria
//     block recomputes $0646 every frame and wins.

const (
	roomFlagsOffs = uint16(0x0403)
	roomDoorOffs  = uint16(0x0468)

	// $0401: doors opened in the current room (high byte of $0400):
	roomDoorsOffs = uint16(0x0401)
	// $0641: toggled by each push of a block on the upper layer; push block door tags set $0468 = $0641 ^ 1:
	roomBlockOffs = uint16(0x0641)
	// $0642: set by pull switches (and statues on floor switches):
	roomPullOffs = uint16(0x0642)
	// $0646: set by a somaria block on a floor switch:
	roomHoldOffs = uint16(0x0646)
	// $06E0: positions of the room's chests in $7E2000 (bit 15: big key lock):
	roomChestPos = uint16(0x06E0)
	// $1980: door types; $19A0: door positions in $7E2000; $19C0: door directions:
	roomDoorTypes = uint16(0x1980)
	roomDoorPos   = uint16(0x19A0)
	roomDoorDirs  = uint16(0x19C0)

	// $0646 value written while a remote player presses a hold switch:
	roomHoldRemote = uint8(0x80)

	// $0403 bits set when the room's keys are taken (KeyRoomFlagMasks):
	roomFlagKey0 = uint8(0x40)
	roomFlagKey1 = uint8(0x20)
	// $0403 bits of chests 0-5 (RoomFlagMask):
	roomFlagChests = uint8(0x3F)
	// $0403 bit the water tags set (DoorFlagMasks+1) once the water was let out:
	roomFlagWater = uint8(0x08)

	// bonk key sprite: sprite $3B (bonk item) with $0DC0 = 1:
	bonkItemSprite = uint8(0x3B)
	bonkKeySubtype = uint8(0x01)

	// most chests drawn open by one update routine:
	roomChestsPerUpdate = 2
	// stop drawing when the VRAM stripe buffer ($1000) is this full:
	roomStripeLimit = uint16(0x0068)
)

// opened small chest tiles (obj14A4): top left, bottom left, top right, bottom right:
var roomOpenChestTiles = [4]uint16{0x19E2, 0x19F2, 0x59E2, 0x59F2}

// roomStateRemote is a remote player's room state as received in message 0x1A.
type roomStateRemote struct {
	valid  bool
	room   uint16
	flags  uint8
	doorTs uint32
	door   uint8
}

// roomDoorsRemote is a remote player's key doors and switches as received in message 0x1B.
type roomDoorsRemote struct {
	valid    bool
	room     uint16
	doors    uint8
	hold     bool
	pulled   bool
	pulledTs uint32
	// pulledQuad is the quadrant the switch was pulled in (roomQuadrant):
	pulledQuad uint8
}

// roomStateLocal tracks the local player's room state.
type roomStateLocal struct {
	// valid is true when the state was read from the SNES in submodule 0 of the dungeon module for room:
	valid bool
	room  uint16
	flags uint8
	door  uint8
	// doorTs is the time (msec) of the last change of door; 0 means no change since the baseline:
	doorTs uint32
	// since is the time (msec) the baseline was taken; only remote door changes after it are applied:
	since uint32

	// doors is $0401; doorsTried are door bits we already tried to open:
	doors      uint8
	doorsTried uint8
	// hold is true while the local player presses a hold switch (not counting $0646 set for remote players):
	hold bool
	// pulled is true once a switch was pulled in this room; pulledTs is when (0 if already pulled at the baseline):
	pulled     bool
	pulledTs   uint32
	pulledQuad uint8
	// holdWritten is true once $0646 was set for remote players; it is not set again while they keep pressing, since a
	// local somaria block clears it every frame:
	holdWritten bool

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *roomStateUpdate
}

// resetRoomState forgets the room state; it is re-read from the SNES as a baseline.
func (g *Game) resetRoomState() {
	rs := &g.roomState
	if rs.inflight != nil {
		rs.inflight.cancelled = true
	}
	*rs = roomStateLocal{}
}

// roomTags returns the room's two tags ($AE, $AF).
func (g *Game) roomTags() [2]uint8 { return [2]uint8{g.wram[0xAE], g.wram[0xAF]} }

func roomHasTag(tags [2]uint8, f func(tag uint8) bool) bool { return f(tags[0]) || f(tags[1]) }

// roomSyncedFlags returns the $0403 bits shared with players in the same room.
func (g *Game) roomSyncedFlags() uint8 {
	if !g.SyncChests {
		// $20 is also chest 5's flag; leave it alone when chests are not synced:
		return roomFlagKey0
	}
	mask := roomFlagKey0 | roomFlagKey1 | roomFlagChests
	tags := g.roomTags()
	if roomHasTag(tags, roomTagHidesChests) {
		// the tag draws the chests closed when it reveals them; wait until it did:
		mask &^= roomFlagChests
	}
	if roomHasTag(tags, roomTagIsWaterGate) {
		// the water gate tags check the bit to know whether the gate was opened already; leave it to them:
		mask &^= roomFlagWater
	}
	return mask
}

// roomTagComputesDoors reports whether a room tag recomputes the shutter state from local conditions every frame.
func roomTagComputesDoors(tag uint8) bool {
	switch {
	case tag >= 0x0B && tag <= 0x13: // RoomTag_OperateBlockDoor (push block doors)
		return true
	case tag == 0x16: // RoomTag_SwitchTrigger_HoldDoor
		return true
	case tag == 0x33: // RoomTag_TorchPuzzleDoor
		return true
	}
	return false
}

// roomTagIsBlockDoor reports whether a room tag is a push block door (RoomTag_OperateBlockDoor), which sets $0468 from
// $0641.
func roomTagIsBlockDoor(tag uint8) bool { return tag >= 0x0B && tag <= 0x13 }

// roomTagIsHold reports whether a room tag is RoomTag_SwitchTrigger_HoldDoor.
func roomTagIsHold(tag uint8) bool { return tag == 0x16 }

// roomTagIsWater reports whether a room tag is one of the water tags (RoomTag_WaterOff/On/Gate, RoomTag_DamGate).
func roomTagIsWater(tag uint8) bool { return tag >= 0x18 && tag <= 0x1B }

// roomTagIsWaterGate reports whether a room tag is RoomTag_WaterGate or RoomTag_DamGate, which use $0403 bit 3 to
// remember the gate was opened. The fill and drain tags set it too but only act on $0642; with the bit set the room
// loads filled or drained (RoomDraw_WaterOverlay*, RoomDraw_EmptyWaterFace).
func roomTagIsWaterGate(tag uint8) bool { return tag == 0x1A || tag == 0x1B }

// roomTagIsPull reports whether a room tag acts once on $0642 set by a pull switch.
func roomTagIsPull(tag uint8) bool {
	switch tag {
	case 0x14, // RoomTag_PullSwitchDoor
		0x18, 0x19, 0x1A, // RoomTag_WaterOff, RoomTag_WaterOn, RoomTag_WaterGate
		0x28: // RoomTag_PullSwitchExplodingWall
		return true
	}
	return false
}

// roomTagHidesChests reports whether a room tag hides the room's chests until it reveals them (RoomDraw_Chest).
func roomTagHidesChests(tag uint8) bool {
	return tag == 0x27 || tag == 0x3C || tag == 0x3E || (tag >= 0x29 && tag < 0x33)
}

// roomQuadrant returns a player's quadrant in the room: bit 0 east (bit 8 of X), bit 1 south (bit 8 of Y).
func roomQuadrant(p *Player) uint8 {
	return uint8(p.X>>8)&1 | (uint8(p.Y>>8)&1)<<1
}

// roomIsKeyDoorType reports whether a door type ($1980 & $FE) is a small key or big key door (see
// Underworld_ProcessTorchesAndDoors).
func roomIsKeyDoorType(t uint8) bool {
	t &= 0xFE
	return t >= 0x1C && t < 0x2C && t != 0x28 && t != 0x2A
}

// roomDoorBit returns the $0401 bit for door slot k.
func roomDoorBit(k int) uint8 { return 0x80 >> (k & 7) }

// enqueueRoomReads adds the WRAM read of $0641..$06EB needed in the underworld.
func (g *Game) enqueueRoomReads(q []snes.Read) []snes.Read {
	if !g.SyncUnderworld || g.local.Module != 0x07 {
		return q
	}
	return g.readEnqueue(q, 0xF50000+uint32(roomBlockOffs), uint8(roomChestPos+12-roomBlockOffs), nil)
}

// readRoomState samples the room state from WRAM and records changes made by the local player.
// Called when WRAM is read from the SNES.
func (g *Game) readRoomState() {
	local := g.local
	rs := &g.roomState

	if local.Module == 0x0E {
		// menus don't touch the room state; keep it:
		return
	}
	if local.Module != 0x07 {
		// left the dungeon module:
		rs.valid = false
		return
	}
	if !g.wramFresh[0xA0] || !g.wramFresh[0xA1] {
		return
	}
	room := g.wramU16(0xA0)
	if rs.valid && (rs.room != room || local.SubModule == 0x01) {
		// a room or intra-room transition; the game resets the shutters:
		rs.valid = false
	}
	if rs.inflight != nil {
		// wait for the update to confirm:
		return
	}
	if !g.wramFresh[roomFlagsOffs] || !g.wramFresh[roomDoorOffs] || !g.wramFresh[roomDoorsOffs] {
		return
	}

	flags := g.wram[roomFlagsOffs]
	door := g.wram[roomDoorOffs]
	tags := g.roomTags()
	switchesFresh := g.wramFresh[roomPullOffs] && g.wramFresh[roomHoldOffs]
	if !rs.valid {
		// the room load is done once back in submodule 0:
		if local.SubModule != 0x00 || !switchesFresh {
			return
		}
		*rs = roomStateLocal{
			valid: true,
			room:  room,
			flags: flags,
			door:  door,
			doors: g.wram[roomDoorsOffs],
			since: timestampFromTime(g.ServerSNESTimestamp()),
		}
		rs.pulled = roomHasTag(tags, roomTagIsPull) && g.wram[roomPullOffs] != 0
		rs.pulledQuad = roomQuadrant(local)
		rs.hold = g.localHolds(tags, door)
		return
	}

	if switchesFresh && !rs.pulled && roomHasTag(tags, roomTagIsPull) {
		// water tags take $0642 back the frame after it was set and switch to submodule $0B, $0C or $0D:
		water := roomHasTag(tags, roomTagIsWater) && local.SubModule >= 0x0B && local.SubModule <= 0x0D
		if g.wram[roomPullOffs] != 0 || water {
			rs.pulled = true
			rs.pulledTs = g.nowTs()
			rs.pulledQuad = roomQuadrant(local)
			log.Printf("alttp: room: local: pulled switch\n")
		}
	}

	// room tags run in submodule 0 and switch to $05 (or $17 for a pressure plate) to move the shutters:
	switch local.SubModule {
	case 0x00, 0x05, 0x17:
	default:
		return
	}

	if flags&^rs.flags != 0 {
		log.Printf("alttp: room: local: flags $%02x -> $%02x\n", rs.flags, flags)
	}
	rs.flags = flags
	if doors := g.wram[roomDoorsOffs]; doors != rs.doors {
		log.Printf("alttp: room: local: doors $%02x -> $%02x\n", rs.doors, doors)
		rs.doors = doors
	}
	if switchesFresh {
		if hold := g.localHolds(tags, door); hold != rs.hold {
			log.Printf("alttp: room: local: hold switch pressed = %v\n", hold)
			rs.hold = hold
		}
	}
	if door == rs.door {
		return
	}
	rs.door = door
	rs.doorTs = g.nowTs()
	log.Printf("alttp: room: local: trap doors $%02x\n", door)
}

// localHolds reports whether the local player holds the doors of a hold switch room open by itself.
func (g *Game) localHolds(tags [2]uint8, door uint8) bool {
	return roomHasTag(tags, roomTagIsHold) && door == 0 && g.wram[roomHoldOffs] != roomHoldRemote
}

// nowTs returns the current server time in msec, never 0.
func (g *Game) nowTs() uint32 {
	ts := timestampFromTime(g.ServerSNESTimestamp())
	if ts == 0 {
		ts = 1
	}
	return ts
}

// roomRemotes returns the remote players in the same room as the local player with a room state sampled there.
func (g *Game) roomRemotes() (remotes []*Player) {
	local := g.local
	rs := &g.roomState
	for _, p := range g.RemotePlayers() {
		if p.Team != local.Team {
			continue
		}
		if p.Module != 0x07 || p.Location&0x010000 == 0 || p.DungeonRoom != rs.room {
			continue
		}
		if !p.Room.valid || p.Room.room != rs.room {
			continue
		}
		remotes = append(remotes, p)
	}
	return
}

// roomDoorWinner returns the remote player whose trap door state we should take, if any.
func (g *Game) roomDoorWinner(remotes []*Player) (winner *Player, door uint8, ts uint32) {
	rs := &g.roomState
	tsCompare := rs.doorTs
	if tsCompare < rs.since {
		tsCompare = rs.since
	}
	for _, p := range remotes {
		if p.Room.doorTs > tsCompare {
			winner, door, ts = p, p.Room.door, p.Room.doorTs
			tsCompare = ts
		}
	}
	return
}

// roomKeyDoor is a key door to unlock.
type roomKeyDoor struct {
	slot int
	pos  uint16
	dir  uint8
}

// roomKeyDoorFor finds the still locked key door here for a $0401 bit set by a remote player.
func (g *Game) roomKeyDoorFor(bit uint8) (d roomKeyDoor, ok bool) {
	for k := 0; k < 16; k++ {
		if roomDoorBit(k) != bit {
			continue
		}
		o := uint32(k << 1)
		if !roomIsKeyDoorType(g.wram[uint32(roomDoorTypes)+o]) {
			continue
		}
		pos := g.wramU16(uint32(roomDoorPos) + o)
		if pos == 0 {
			continue
		}
		return roomKeyDoor{slot: k, pos: pos, dir: g.wram[uint32(roomDoorDirs)+o] & 3}, true
	}
	return
}

// roomStateUpdate is a generated update routine for the room state awaiting execution confirmation.
type roomStateUpdate struct {
	g         *Game
	room      uint16
	flags     uint8 // flags OR'ed into $0403
	chests    uint8 // chests drawn open
	door      uint8
	setDoor   bool
	ts        uint32
	flagsFrom *Player
	doorFrom  *Player

	keyDoorBit  uint8 // $0401 bit of the key door unlocked, if any
	keyDoorFrom *Player

	pull     bool // $0642 set
	pullTs   uint32
	pullFrom *Player

	blockDoor bool // push block door opened from $0641

	hold     bool // $0646 set to roomHoldRemote
	holdOff  bool // $0646 cleared
	holdFrom *Player

	cancelled bool
}

func (u *roomStateUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	rs := &g.roomState
	if u.cancelled || rs.inflight != u {
		return
	}
	rs.inflight = nil

	if value == crystalSwitchSkipped {
		// conditions were not right; try again later:
		log.Printf("alttp: room: update skipped\n")
		return
	}
	if !rs.valid || rs.room != u.room {
		return
	}

	if u.flags != 0 {
		rs.flags |= u.flags
		log.Printf("alttp: room: update: flags |= $%02x from player '%s'\n", u.flags, u.flagsFrom.Name())
		if u.flags&^roomFlagChests != 0 {
			g.PushNotification(fmt.Sprintf("took keys in this room from %s", u.flagsFrom.Name()))
		}
		if u.chests != 0 {
			g.PushNotification(fmt.Sprintf("opened chests in this room from %s", u.flagsFrom.Name()))
		}
	}
	if u.keyDoorBit != 0 {
		rs.doorsTried |= u.keyDoorBit
		log.Printf("alttp: room: update: unlocked door $%02x from player '%s'\n", u.keyDoorBit, u.keyDoorFrom.Name())
		g.PushNotification(fmt.Sprintf("unlocked door from %s", u.keyDoorFrom.Name()))
	}
	if u.pull {
		rs.pulled = true
		rs.pulledTs = u.pullTs
		rs.pulledQuad = u.pullFrom.Doors.pulledQuad
		log.Printf("alttp: room: update: pulled switch from player '%s'\n", u.pullFrom.Name())
		g.PushNotification(fmt.Sprintf("pulled switch from %s", u.pullFrom.Name()))
	}
	if u.blockDoor {
		log.Printf("alttp: room: update: opened push block door\n")
	}
	if u.hold {
		rs.holdWritten = true
		log.Printf("alttp: room: update: hold switch pressed by player '%s'\n", u.holdFrom.Name())
	}
	if u.holdOff {
		log.Printf("alttp: room: update: hold switch released\n")
	}
	if u.setDoor {
		rs.door = u.door
		rs.doorTs = u.ts
		log.Printf("alttp: room: update: trap doors $%02x from player '%s'\n", u.door, u.doorFrom.Name())
		if u.door == 0 {
			g.PushNotification(fmt.Sprintf("opened trap doors from %s", u.doorFrom.Name()))
		} else {
			g.PushNotification(fmt.Sprintf("closed trap doors from %s", u.doorFrom.Name()))
		}
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *roomStateUpdate) AbortAsm() {
	rs := &u.g.roomState
	if u.cancelled || rs.inflight != u {
		return
	}
	rs.inflight = nil
	// we cannot know whether the routine executed; re-read the state as a new baseline:
	rs.valid = false
}

// generateRoomStateUpdate generates an update routine to take the room state of the remote players in the same room.
// It has no side effects other than adopting timestamps of agreeing remote door states; the caller must set inflight
// if the routine is emitted.
func (g *Game) generateRoomStateUpdate(newEmitter func() *asm.Emitter, index uint32) (u *roomStateUpdate, a *asm.Emitter) {
	rs := &g.roomState
	local := g.local
	if !rs.valid || rs.inflight != nil {
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 {
		return
	}

	remotes := g.roomRemotes()
	tags := g.roomTags()
	u = &roomStateUpdate{g: g, room: rs.room}

	// chest and key flags:
	mask := g.roomSyncedFlags()
	for _, p := range remotes {
		if f := p.Room.flags & mask &^ rs.flags &^ u.flags; f != 0 {
			u.flags |= f
			u.flagsFrom = p
		}
	}
	var chests []int
	for i := 0; i < 6 && len(chests) < roomChestsPerUpdate; i++ {
		if u.flags&(1<<i) == 0 {
			continue
		}
		pos := g.wramU16(uint32(roomChestPos) + uint32(i<<1))
		if pos == 0 || pos&0x8000 != 0 {
			// no chest or a big key lock:
			continue
		}
		chests = append(chests, i)
		u.chests |= 1 << i
	}

	// key doors, switches:
	var keyDoor roomKeyDoor
	hasHold := roomHasTag(tags, roomTagIsHold)
	hasPull := roomHasTag(tags, roomTagIsPull)
	wantHold := false
	for _, p := range remotes {
		d := &p.Doors
		if !d.valid || d.room != rs.room {
			continue
		}
		if u.keyDoorBit == 0 {
			for b := uint8(0x80); b != 0; b >>= 1 {
				if d.doors&b == 0 || (rs.doors|rs.doorsTried)&b != 0 {
					continue
				}
				if kd, ok := g.roomKeyDoorFor(b); ok {
					keyDoor, u.keyDoorBit, u.keyDoorFrom = kd, b, p
					break
				}
			}
		}
		if hasPull && !rs.pulled && !u.pull && d.pulled && d.pulledTs > rs.since {
			// the water tags flood or drain the quadrant the camera is in, which only works out when Link is where the
			// switch was pulled (e.g. Underworld_FloodSwampWater_CoverFloor never ends with the camera in another
			// quadrant); wait until Link is in the quadrant the switch was pulled in:
			if !roomHasTag(tags, roomTagIsWater) || roomQuadrant(local) == d.pulledQuad {
				u.pull, u.pullTs, u.pullFrom = true, d.pulledTs, p
			}
		}
		if d.hold && !wantHold {
			wantHold, u.holdFrom = true, p
		}
	}
	if !wantHold {
		rs.holdWritten = false
	}
	if hasHold && g.wramFresh[roomHoldOffs] {
		switch hold := g.wram[roomHoldOffs]; {
		case wantHold && hold == 0 && !rs.holdWritten:
			u.hold = true
		case !wantHold && hold == roomHoldRemote:
			u.holdOff = true
		}
	}

	// push block doors: the tag only opens the doors while Link is in its quadrant, and a block moved by sync may have
	// toggled $0641 while the doors were busy (e.g. closing behind a player entering the room); open them like the tag:
	if u.keyDoorBit == 0 && len(remotes) > 0 && roomHasTag(tags, roomTagIsBlockDoor) && g.wramFresh[roomBlockOffs] &&
		g.wram[roomBlockOffs]&1 != 0 && rs.door != 0 {
		u.blockDoor = true
	}

	// trap doors; a key door being unlocked or a push block door uses the submodule this time:
	if winner, door, ts := g.roomDoorWinner(remotes); winner != nil {
		if door == rs.door {
			// already in agreement; keep the latest timestamp:
			rs.doorTs = ts
		} else if u.keyDoorBit == 0 && !u.blockDoor && !roomHasTag(tags, roomTagComputesDoors) {
			u.setDoor, u.door, u.ts, u.doorFrom = true, door, ts, winner
		}
	}

	if u.flags == 0 && u.keyDoorBit == 0 && !u.blockDoor && !u.pull && !u.hold && !u.holdOff && !u.setDoor {
		u = nil
		return
	}

	a = newEmitter()
	a.Comment(fmt.Sprintf("room $%03x state:", rs.room))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	asmBNEFar(a, "rm_skip", "rm_module_ok")
	a.LDA_dp(0x11)
	asmBNEFar(a, "rm_skip", "rm_submodule_ok")
	a.Comment("still in the same room:")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(rs.room))
	asmBNEFar(a, "rm_skip", "rm_room_lo_ok")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(rs.room >> 8))
	asmBNEFar(a, "rm_skip", "rm_room_hi_ok")
	if u.setDoor && u.keyDoorBit == 0 {
		a.Comment("trap doors not changed locally since read:")
		a.LDA_abs(roomDoorOffs)
		a.CMP_imm8_b(rs.door)
		asmBNEFar(a, "rm_skip", "rm_door_ok")
	}

	if u.flags != 0 {
		a.Comment(fmt.Sprintf("chests opened and keys taken by %s:", u.flagsFrom.Name()))
		a.LDA_abs(roomFlagsOffs)
		a.ORA_imm8_b(u.flags)
		a.STA_abs(roomFlagsOffs)
	}
	if keys := u.flags & (roomFlagKey0 | roomFlagKey1); keys != 0 {
		a.Comment("remove bonk keys of those flags (they only check $0403 when the room loads):")
		a.LDX_imm8_b(0x0F)
		a.Label("rm_next")
		a.LDA_abs_x(0x0DD0)
		a.BEQ("rm_cont")
		a.LDA_abs_x(0x0E20)
		a.CMP_imm8_b(bonkItemSprite)
		a.BNE("rm_cont")
		a.LDA_abs_x(0x0DC0)
		a.CMP_imm8_b(bonkKeySubtype)
		a.BNE("rm_cont")
		a.LDA_abs_x(0x0CBA)
		switch keys {
		case roomFlagKey0:
			a.BNE("rm_cont")
		case roomFlagKey1:
			a.CMP_imm8_b(0x01)
			a.BNE("rm_cont")
		default:
			a.CMP_imm8_b(0x02)
			a.BCS("rm_cont")
		}
		a.STZ_abs_x(0x0DD0)
		a.Label("rm_cont")
		a.DEX()
		a.BPL("rm_next")
	}
	if len(chests) > 0 {
		a.REP(0x30)
		for _, i := range chests {
			g.emitOpenChest(a, i, g.wramU16(uint32(roomChestPos)+uint32(i<<1)))
		}
		a.SEP(0x30)
	}

	if u.pull {
		a.Comment(fmt.Sprintf("switch pulled by %s:", u.pullFrom.Name()))
		if roomHasTag(tags, roomTagIsWater) {
			q := u.pullFrom.Doors.pulledQuad
			a.Comment("Link still in the quadrant the switch was pulled in:")
			a.LDA_dp(0x23)
			a.AND_imm8_b(0x01)
			a.CMP_imm8_b(q & 1)
			asmBNEFar(a, "rm_skip", "rm_pull_x_ok")
			a.LDA_dp(0x21)
			a.AND_imm8_b(0x01)
			a.CMP_imm8_b(q >> 1)
			asmBNEFar(a, "rm_skip", "rm_pull_y_ok")
		}
		a.LDA_abs(roomPullOffs)
		a.BNE("rm_pulled")
		a.LDA_imm8_b(0x01)
		a.STA_abs(roomPullOffs)
		a.Label("rm_pulled")
	}
	if u.hold {
		a.Comment(fmt.Sprintf("hold switch pressed by %s:", u.holdFrom.Name()))
		a.LDA_abs(roomHoldOffs)
		a.BNE("rm_held")
		a.LDA_imm8_b(roomHoldRemote)
		a.STA_abs(roomHoldOffs)
		a.Label("rm_held")
	}
	if u.holdOff {
		a.Comment("hold switch released by remote players:")
		a.LDA_abs(roomHoldOffs)
		a.CMP_imm8_b(roomHoldRemote)
		a.BNE("rm_released")
		a.STZ_abs(roomHoldOffs)
		a.Label("rm_released")
	}

	if u.keyDoorBit != 0 {
		a.Comment(fmt.Sprintf("door slot %d unlocked by %s:", keyDoor.slot, u.keyDoorFrom.Name()))
		a.REP(0x30)
		emitUnlockDoor(a, keyDoor, "rm_door")
		a.SEP(0x30)
	} else if u.blockDoor {
		a.Comment("open the push block door like RoomTag_OperateBlockDoor once the block was pushed:")
		a.LDA_abs(roomBlockOffs)
		a.AND_imm8_b(0x01)
		a.BEQ("rm_block_door")
		a.LDA_abs(roomDoorOffs)
		a.BEQ("rm_block_door")
		a.STZ_abs(roomDoorOffs)
		a.STZ_abs(roomDoorOffs + 1)
		a.STZ_abs(0x068E)
		a.STZ_abs(0x068F)
		a.STZ_abs(0x0690)
		a.STZ_abs(0x0691)
		a.LDA_imm8_b(0x25) // SFX3.25
		a.STA_abs(0x012F)
		a.LDA_imm8_b(0x05)
		a.STA_dp(0x11)
		a.Label("rm_block_door")
	} else if u.setDoor {
		a.Comment(fmt.Sprintf("trap doors = $%02x from %s:", u.door, u.doorFrom.Name()))
		a.LDA_imm8_b(u.door)
		a.STA_abs(roomDoorOffs)
		a.STZ_abs(roomDoorOffs + 1)
		a.STZ_abs(0x068E)
		a.STZ_abs(0x068F)
		a.STZ_abs(0x0690)
		a.STZ_abs(0x0691)
		a.LDA_imm8_b(0x25) // SFX3.25
		a.STA_abs(0x012F)
		a.Comment("move the shutters (OperateShutterDoors):")
		a.LDA_imm8_b(0x05)
		a.STA_dp(0x11)
	}

	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("rm_report")
	a.Label("rm_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("rm_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}

// roomStripeAddr returns the VRAM stripe header (as stored, big endian) for the tile at $7E2000 offset v
// (RoomTag_BuildChestStripes).
func roomStripeAddr(v uint16) uint16 {
	a := (v&0x0040)<<4 | (v&0x303F)>>1 | (v&0x0F80)>>2
	return a<<8 | a>>8
}

// emitOpenChest draws small chest i at pos open if it is still closed here, the way OpenItemChest does, without giving
// its item. 16-bit registers.
func (g *Game) emitOpenChest(a *asm.Emitter, i int, pos uint16) {
	done := fmt.Sprintf("rm_chest_%d", i)
	tile := pos & 0x7FFF
	attr := uint32(tile >> 1)
	closed := uint16(0x5858) + uint16(i)*0x0101

	a.Comment(fmt.Sprintf("draw chest %d at $%04x open if it is a small chest still closed:", i, pos))
	a.LDA_abs(roomChestPos + uint16(i<<1))
	a.CMP_imm16_w(pos)
	asmBNEFar(a, done, done+"_pos")
	a.LDA_long(0x7F2000 + attr)
	a.CMP_imm16_w(closed)
	asmBNEFar(a, done, done+"_closed")
	a.Comment("big chests are 4 tiles wide:")
	a.LDA_long(0x7F2042 + attr)
	a.CMP_imm16_w(closed)
	a.BNE(done + "_small")
	a.JMP_abs(done)
	a.Label(done + "_small")
	a.LDA_abs(0x1000)
	a.CMP_imm16_w(roomStripeLimit)
	a.BCC(done + "_room")
	a.JMP_abs(done)
	a.Label(done + "_room")
	a.TAX()

	offs := [4]uint16{0x0000, 0x0080, 0x0002, 0x0082}
	for t := 0; t < 4; t++ {
		a.LDA_imm16_w(roomOpenChestTiles[t])
		a.STA_long(0x7E2000 + uint32(tile+offs[t]))
		a.STA_abs_x(0x1006 + uint16(t)*6)
		a.LDA_imm16_w(roomStripeAddr(tile + offs[t]))
		a.STA_abs_x(0x1002 + uint16(t)*6)
		a.LDA_imm16_w(0x0100)
		a.STA_abs_x(0x1004 + uint16(t)*6)
	}
	a.LDA_imm16_w(0xFFFF)
	a.STA_abs_x(0x101A)
	a.LDA_imm16_w(0x2727)
	a.STA_long(0x7F2000 + attr)
	a.STA_long(0x7F2040 + attr)
	a.TXA()
	a.CLC()
	asmADC_imm16(a, 0x0018)
	a.STA_abs(0x1000)
	a.SEP(0x20)
	a.LDA_imm8_b(0x01)
	a.STA_dp(0x14)
	a.REP(0x20)
	a.Label(done)
}

// emitUnlockDoor unlocks a key door if it is still closed here, the way Underworld_ProcessTorchesAndDoors does once
// the key was used: point $068E at the door's tile attributes ($F0 + slot, see Underworld_LoadSingleDoorAttribute),
// clear $0690 and switch to submodule $04 (UnlockKeyDoor_Main), which sets the door's bit in $0400. 16-bit registers.
func emitUnlockDoor(a *asm.Emitter, d roomKeyDoor, label string) {
	attr := uint16(d.pos>>1) + 0x41
	a.Comment(fmt.Sprintf("unlock door slot %d at $%04x if still locked:", d.slot, d.pos))
	a.LDA_abs(roomDoorPos + uint16(d.slot<<1))
	a.CMP_imm16_w(d.pos)
	a.BNE(label)
	a.LDA_long(0x7F2000 + uint32(attr))
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x00F0 | uint16(d.slot))
	a.BNE(label)
	a.LDA_imm16_w(attr)
	a.STA_abs(0x068E)
	a.STZ_abs(0x0690)
	a.SEP(0x20)
	a.LDA_imm8_b(0x04)
	a.STA_dp(0x11)
	a.LDA_imm8_b(0x14 | [4]uint8{0x00, 0x00, 0x80, 0x40}[d.dir]) // SFX3.14 with DoorOpenSFXPan
	a.STA_abs(0x012F)
	a.REP(0x20)
	a.Label(label)
}

// SerializeRoomState writes message 0x1A with the room state, if it was sampled in the current room.
func (g *Game) SerializeRoomState(w io.Writer) (err error) {
	rs := &g.roomState
	if !rs.valid {
		return
	}
	b := []byte{uint8(MsgRoomState), uint8(rs.room), uint8(rs.room >> 8), rs.flags}
	b = binary.LittleEndian.AppendUint32(b, rs.doorTs)
	b = append(b, rs.door)
	_, err = w.Write(b)
	return
}

func (g *Game) DeserializeRoomState(p *Player, r io.Reader) (err error) {
	var m struct {
		Room   uint16
		Flags  uint8
		DoorTs uint32
		Door   uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
		return fmt.Errorf("error deserializing room state: %w", err)
	}
	p.Room = roomStateRemote{valid: true, room: m.Room, flags: m.Flags, doorTs: m.DoorTs, door: m.Door}
	return nil
}

// SerializeRoomDoors writes message 0x1B with the key doors and switches, if the room state was sampled.
func (g *Game) SerializeRoomDoors(w io.Writer) (err error) {
	rs := &g.roomState
	if !rs.valid {
		return
	}
	var switches uint8
	if rs.hold {
		switches |= 0x01
	}
	if rs.pulled {
		switches |= 0x02 | rs.pulledQuad<<2
	}
	b := []byte{uint8(MsgRoomDoors), uint8(rs.room), uint8(rs.room >> 8), rs.doors, switches}
	b = binary.LittleEndian.AppendUint32(b, rs.pulledTs)
	_, err = w.Write(b)
	return
}

func (g *Game) DeserializeRoomDoors(p *Player, r io.Reader) (err error) {
	var m struct {
		Room     uint16
		Doors    uint8
		Switches uint8
		PulledTs uint32
	}
	if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
		return fmt.Errorf("error deserializing room doors: %w", err)
	}
	p.Doors = roomDoorsRemote{
		valid:      true,
		room:       m.Room,
		doors:      m.Doors,
		hold:       m.Switches&0x01 != 0,
		pulled:     m.Switches&0x02 != 0,
		pulledTs:   m.PulledTs,
		pulledQuad: (m.Switches >> 2) & 3,
	}
	return nil
}
