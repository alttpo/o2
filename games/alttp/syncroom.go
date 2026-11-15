package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"io"
	"log"
)

// Room state synchronization: trap doors and key flags of the current room
//
// Two pieces of underworld state only live in WRAM while the room is loaded, so the SRAM room flags ($7EF000) do not
// share them with players in the same room until somebody leaves it:
//
//   - $0468 is the trap door (shutter) state of the current room: 0 = open, 1 = closed. Room tags open or close the
//     shutters by changing it, clearing $068E/$0690 and switching to submodule $05, which animates the doors
//     (OperateShutterDoors). Room loads set it to 1 and an inter-room transition closes the shutters behind Link
//     (Module07_02_0F); an intra-room transition (submodule $01) opens and then closes them again.
//   - $0403 holds the room's chest, key and boss flags (written to $7EF000 on leaving the room). Picking up a small key
//     or big key sprite ($E4/$E5, Absorb_FlagKey) or a bonk key (sprite $3B with $0DC0 = 1, BonkKey_GrantKey, e.g. on
//     top of a Desert Palace torch) sets $40 (key index 0) or $20 (key index 1), the index being the key's $0CBA. Key
//     sprites remove themselves when their flag is set; bonk keys only check it when the room loads.
//
// Both are sent in message 0x1A (`room u16`, `flags u8` ($0403), `doorTs u32`, `door u8` ($0468)) in a broadcastSector
// packet to the current room's sector, only once they were sampled in that room. It is shared with alttpo (RoomState.as).
//
//   - Sampling: in the dungeon module only, while the room ($A0) stays the same. A baseline is taken in submodule 0. A
//     change of $0468 seen in submodule 0, $05 (shutters moving) or $17 (pressure plate) is a local change and is
//     timestamped; room tags change it during submodule 0 and switch to $05. Changes of room and intra-room
//     transitions (submodule $01) forget the state, since the game resets the shutters itself.
//   - Key flags ($40, and $20 if chests are synced since it is also chest 5's flag) are OR'ed in from remote players
//     in the same room whose state was sampled there. Bonk keys of the newly set flags are removed.
//   - Trap doors are last writer wins among remote players in the same room with a change made after our baseline
//     was taken (so only changes made while both players are in the room count, never stale ones from an earlier
//     visit). They are not applied in rooms with tags that compute the shutter state every frame from what the local
//     player does (hold switch doors, push block doors, torch puzzle doors); those would close again immediately.

const (
	roomFlagsOffs = uint16(0x0403)
	roomDoorOffs  = uint16(0x0468)

	// $0403 bits set when the room's keys are taken (KeyRoomFlagMasks):
	roomFlagKey0 = uint8(0x40)
	roomFlagKey1 = uint8(0x20)

	// bonk key sprite: sprite $3B (bonk item) with $0DC0 = 1:
	bonkItemSprite = uint8(0x3B)
	bonkKeySubtype = uint8(0x01)
)

// roomStateRemote is a remote player's room state as received.
type roomStateRemote struct {
	valid  bool
	room   uint16
	flags  uint8
	doorTs uint32
	door   uint8
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

// roomSyncedFlags returns the $0403 bits shared with players in the same room.
func (g *Game) roomSyncedFlags() uint8 {
	if g.SyncChests {
		return roomFlagKey0 | roomFlagKey1
	}
	// $20 is also chest 5's flag; leave it alone when chests are not synced:
	return roomFlagKey0
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

// readRoomState samples $0403 and $0468 from WRAM and records any door change made by the local player.
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
	if !g.wramFresh[roomFlagsOffs] || !g.wramFresh[roomDoorOffs] {
		return
	}

	flags := g.wram[roomFlagsOffs]
	door := g.wram[roomDoorOffs]
	if !rs.valid {
		// the room load is done once back in submodule 0:
		if local.SubModule != 0x00 {
			return
		}
		rs.valid = true
		rs.room = room
		rs.flags = flags
		rs.door = door
		rs.doorTs = 0
		rs.since = timestampFromTime(g.ServerSNESTimestamp())
		return
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
	if door == rs.door {
		return
	}
	rs.door = door
	rs.doorTs = timestampFromTime(g.ServerSNESTimestamp())
	if rs.doorTs == 0 {
		rs.doorTs = 1
	}
	log.Printf("alttp: room: local: trap doors $%02x\n", door)
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

// roomStateUpdate is a generated update routine for the room state awaiting execution confirmation.
type roomStateUpdate struct {
	g         *Game
	room      uint16
	flags     uint8 // flags OR'ed into $0403
	door      uint8
	setDoor   bool
	ts        uint32
	flagsFrom *Player
	doorFrom  *Player
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
		g.PushNotification(fmt.Sprintf("took keys in this room from %s", u.flagsFrom.Name()))
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

// generateRoomStateUpdate generates an update routine to take the key flags and trap door state of the remote
// players in the same room. It has no side effects other than adopting timestamps of agreeing remote door states; the
// caller must set inflight if the routine is emitted.
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
	if len(remotes) == 0 {
		return
	}

	u = &roomStateUpdate{g: g, room: rs.room}

	// key flags:
	mask := g.roomSyncedFlags()
	for _, p := range remotes {
		if f := p.Room.flags & mask &^ rs.flags &^ u.flags; f != 0 {
			u.flags |= f
			u.flagsFrom = p
		}
	}

	// trap doors:
	if winner, door, ts := g.roomDoorWinner(remotes); winner != nil {
		if door == rs.door {
			// already in agreement; keep the latest timestamp:
			rs.doorTs = ts
		} else if !roomTagComputesDoors(g.wram[0xAE]) && !roomTagComputesDoors(g.wram[0xAF]) {
			u.setDoor, u.door, u.ts, u.doorFrom = true, door, ts, winner
		}
	}

	if u.flags == 0 && !u.setDoor {
		u = nil
		return
	}

	a = newEmitter()
	a.Comment(fmt.Sprintf("room $%03x state:", rs.room))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("rm_skip")
	a.LDA_dp(0x11)
	a.BNE("rm_skip")
	a.Comment("still in the same room:")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(rs.room))
	a.BNE("rm_skip")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(rs.room >> 8))
	a.BNE("rm_skip")
	if u.setDoor {
		a.Comment("trap doors not changed locally since read:")
		a.LDA_abs(roomDoorOffs)
		a.CMP_imm8_b(rs.door)
		a.BNE("rm_skip")
	}

	if u.flags != 0 {
		a.Comment(fmt.Sprintf("keys taken from %s:", u.flagsFrom.Name()))
		a.LDA_abs(roomFlagsOffs)
		a.ORA_imm8_b(u.flags)
		a.STA_abs(roomFlagsOffs)

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
		switch u.flags & (roomFlagKey0 | roomFlagKey1) {
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

	if u.setDoor {
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
