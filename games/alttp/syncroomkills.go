package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"io"
	"log"
)

// Kill triggers: room tags that act once the enemies of a quadrant or of the whole room are dead
//
// Enemies are not synced, so in a room whose tag waits for its enemies to die, a player whose enemies are dead and a
// player whose enemies are still alive disagree about the tag. The tags are (zelda3 kDungTagroutines):
//
//   - $01-$0A: trap doors open (Dung_TagRoutine_TrapdoorsUp: $0468 = 0, $068E/$0690 cleared, SFX3 $1B, submodule $05)
//     once the screen is clear while Link is in the tag's quadrant ($01 NW, $02 NE, $03 SW, $04 SE), half ($05 W, $06
//     E, $07 N, $08 S) or anywhere ($09); $0A once the whole room is clear. The tag stays.
//   - $26 RoomTag_KillRoomBlock: once the screen is clear while Link is in the south east quadrant, the tag is cleared
//     (which lets the room's block be pushed) and SFX3 $1B plays.
//   - $29-$32: like $01-$0A, but they reveal the room's chests (RoomTag_OperateChestReveal), which clears the tag.
//
// A local trigger is seen from what the tag does, without reading the sprite tables: a chest or block tag going to 0,
// or the trap doors opening locally ($0468 1 -> 0, not from sync) in a room with a trap door kill tag that can fire in
// Link's quadrant. It is recorded as bit k for the tag in $AE+k.
//
// Message 0x1E (`room u16`, `fired u8`: bit k = the kill trigger in $AE+k fired during this room visit) is sent last in
// the room's sector packet, after 0x1D, once the room state was sampled there. Shared with alttpo (RoomState.as).
//
// Applying: in submodule 0 of the dungeon module, for a bit fired by a remote player in the same room that did not fire
// here, the tag's effect is applied the way the tag does it, without touching the enemies: trap doors opened (if still
// closed, and only when no other door change is made by the same update), the block tag cleared, or the chests
// revealed (closed small chest tiles in $7E2000, attributes $5858 + $0101 * i in $7F2000, VRAM stripes from $1000 = 0,
// $14 = 1; only while no stripes are pending, $14 = 0) and the tag cleared. Each bit is applied once per visit.
// Enemies stay alive; which enemies are dead is not synced.

const (
	// the update routine reports roomKillNone when the tag fired here already:
	roomKillNone = uint8(0x02)
	// the number of chests ($0496 / 2) revealed at most; RoomTag_OperateChestReveal reveals all of them:
	roomRevealMaxChests = 6
)

// closed small chest tiles (obj149C): top left, bottom left, top right, bottom right:
var roomClosedChestTiles = [4]uint16{0x19E1, 0x19F1, 0x59E1, 0x59F1}

// roomKillsRemote is a remote player's kill triggers as received in message 0x1E.
type roomKillsRemote struct {
	valid bool
	room  uint16
	fired uint8
}

// roomKillTagBase returns the trap door tag ($01-$0A) a kill trigger tag acts like, or 0 if the tag is not one.
func roomKillTagBase(tag uint8) uint8 {
	switch {
	case tag >= 0x01 && tag <= 0x0A:
		return tag
	case tag >= 0x29 && tag <= 0x32: // the chest reveal tags
		return tag - 0x28
	case tag == 0x26: // RoomTag_KillRoomBlock, in the south east quadrant
		return 0x04
	}
	return 0
}

// roomKillTagIsDoor reports whether a kill trigger tag opens the trap doors (and is not cleared when it fires).
func roomKillTagIsDoor(tag uint8) bool { return tag >= 0x01 && tag <= 0x0A }

// roomKillTagIsChest reports whether a kill trigger tag reveals the room's chests.
func roomKillTagIsChest(tag uint8) bool { return tag >= 0x29 && tag <= 0x32 }

// roomKillTagFires reports whether a kill trigger tag checks for its enemies while Link is in quadrant q.
func roomKillTagFires(tag uint8, q uint8) bool {
	base := roomKillTagBase(tag)
	switch {
	case base == 0:
		return false
	case base <= 0x04:
		return q == base-0x01
	case base <= 0x06:
		return q&1 == base-0x05
	case base <= 0x08:
		return q>>1 == base-0x07
	}
	// $09: any quadrant, $0A: the whole room:
	return true
}

// readRoomKills records the kill triggers fired locally. Called from readRoomState in submodule 0, $05 or $17 with the
// door as last sampled and the current one.
func (g *Game) readRoomKills(tags [2]uint8, prevDoor, door uint8) {
	rs := &g.roomState
	q := roomQuadrant(g.local)
	for k, prev := range rs.tags {
		bit := uint8(1) << k
		if prev == 0 || tags[k] != 0 || roomKillTagBase(prev) == 0 || roomKillTagIsDoor(prev) || rs.fired&bit != 0 {
			continue
		}
		// a chest reveal or kill room block tag fired:
		rs.fired |= bit
		log.Printf("alttp: room: local: kill trigger $%02x fired\n", prev)
	}
	rs.tags = tags
	if prevDoor == 0 || door != 0 {
		return
	}
	for k, tag := range tags {
		bit := uint8(1) << k
		if !roomKillTagIsDoor(tag) || !roomKillTagFires(tag, q) || rs.fired&bit != 0 {
			continue
		}
		rs.fired |= bit
		log.Printf("alttp: room: local: kill trigger $%02x opened the trap doors\n", tag)
	}
}

// roomKillWanted picks a kill trigger fired by a remote player in the room to apply here, if any.
func (g *Game) roomKillWanted(remotes []*Player, tags [2]uint8) (k int, from *Player) {
	rs := &g.roomState
	for _, p := range remotes {
		if !p.Kills.valid || p.Kills.room != rs.room {
			continue
		}
		for k = 0; k < 2; k++ {
			bit := uint8(1) << k
			if p.Kills.fired&bit == 0 || rs.fired&bit != 0 || roomKillTagBase(tags[k]) == 0 {
				continue
			}
			return k, p
		}
	}
	return -1, nil
}

// confirmRoomKill handles the result of the kill trigger part of a room state update.
func (g *Game) confirmRoomKill(u *roomStateUpdate, value uint8) {
	rs := &g.roomState
	rs.fired |= 1 << u.killSlot
	if value == roomKillNone {
		log.Printf("alttp: room: update: kill trigger $%02x already fired here\n", u.killTag)
		return
	}
	log.Printf("alttp: room: update: kill trigger $%02x from player '%s'\n", u.killTag, u.killFrom.Name())
	switch {
	case roomKillTagIsDoor(u.killTag):
		// not a local change of the trap doors:
		rs.door = 0
		g.PushNotification(fmt.Sprintf("opened trap doors from %s", u.killFrom.Name()))
	case roomKillTagIsChest(u.killTag):
		g.PushNotification(fmt.Sprintf("revealed chests from %s", u.killFrom.Name()))
	}
}

// emitRoomKill applies the effect of kill trigger tag in $AE+k without touching the enemies, for chests from the
// positions in chestPos. Branches to "rm_kill_none" if it fired here already. Expects 8-bit A and X; a chest reveal must
// be preceded by the check that $14 is 0.
func emitRoomKill(a *asm.Emitter, k int, tag uint8, chestPos []uint16) {
	tagAddr := uint8(0xAE + k)
	switch {
	case roomKillTagIsDoor(tag):
		a.Comment("open the trap doors like Dung_TagRoutine_TrapdoorsUp:")
		a.LDA_abs(roomDoorOffs)
		a.BNE("rk_door")
		a.JMP_abs("rm_kill_none")
		a.Label("rk_door")
		a.STZ_abs(roomDoorOffs)
		a.STZ_abs(roomDoorOffs + 1)
		a.STZ_abs(0x068E)
		a.STZ_abs(0x068F)
		a.STZ_abs(0x0690)
		a.STZ_abs(0x0691)
		a.LDA_imm8_b(0x1B) // SFX3.1B
		a.STA_abs(0x012F)
		a.LDA_imm8_b(0x05)
		a.STA_dp(0x11)
		return
	}

	a.Comment("the tag is still there:")
	a.LDA_dp(tagAddr)
	a.CMP_imm8_b(tag)
	a.BEQ("rk_tag")
	a.JMP_abs("rm_kill_none")
	a.Label("rk_tag")
	a.STZ_dp(tagAddr)
	if !roomKillTagIsChest(tag) {
		a.Comment("RoomTag_KillRoomBlock lets the block be pushed:")
		a.LDA_imm8_b(0x1B) // SFX3.1B
		a.STA_abs(0x012F)
		return
	}

	a.Comment(fmt.Sprintf("reveal the %d chests like RoomTag_OperateChestReveal:", len(chestPos)))
	a.REP(0x30)
	for i, pos := range chestPos {
		tile := pos & 0x3FFF
		for t, o := range roomChestTileOffs {
			a.LDA_imm16_w(roomClosedChestTiles[t])
			a.STA_long(0x7E2000 + uint32(tile+o))
		}
		a.LDA_imm16_w(0x5858 + uint16(i)*0x0101)
		a.STA_long(0x7F2000 + uint32(tile>>1))
		a.STA_long(0x7F2040 + uint32(tile>>1))
		x := uint16(i) * 0x18
		for t, o := range roomChestTileOffs {
			a.LDA_imm16_w(roomStripeAddr(tile + o))
			a.STA_abs(0x1002 + x + uint16(t)*6)
			a.LDA_imm16_w(roomClosedChestTiles[t])
			a.STA_abs(0x1006 + x + uint16(t)*6)
		}
	}
	a.LDA_imm16_w(0x0100)
	for i := range chestPos {
		for t := range roomChestTileOffs {
			a.STA_abs(0x1004 + uint16(i)*0x18 + uint16(t)*6)
		}
	}
	a.LDA_imm16_w(0xFFFF)
	a.STA_abs(0x101A + uint16(len(chestPos)-1)*0x18)
	a.LDA_imm16_w(uint16(len(chestPos)) * 0x18)
	a.STA_abs(0x1000)
	a.SEP(0x30)
	a.LDA_imm8_b(0x1A) // SFX3.1A
	a.STA_abs(0x012F)
	a.LDA_imm8_b(0x01)
	a.STA_dp(0x14)
}

// roomRevealChestPos returns the positions of the chests RoomTag_OperateChestReveal reveals ($06E0, $0496 / 2 of them).
func (g *Game) roomRevealChestPos() (pos []uint16, ok bool) {
	if !g.wramFresh[0x0496] || !g.wramFresh[0x0497] {
		return nil, false
	}
	n := int(g.wramU16(0x0496) >> 1)
	if n == 0 || n > roomRevealMaxChests {
		return nil, false
	}
	for i := 0; i < n; i++ {
		o := uint32(roomChestPos) + uint32(i<<1)
		if !g.wramFresh[o] || !g.wramFresh[o+1] {
			return nil, false
		}
		pos = append(pos, g.wramU16(o))
	}
	return pos, true
}

// SerializeRoomKills writes message 0x1E with the kill triggers fired in the room, if the room state was sampled.
func (g *Game) SerializeRoomKills(w io.Writer) (err error) {
	rs := &g.roomState
	if !rs.valid {
		return
	}
	_, err = w.Write([]byte{uint8(MsgRoomKills), uint8(rs.room), uint8(rs.room >> 8), rs.fired})
	return
}

func (g *Game) DeserializeRoomKills(p *Player, r io.Reader) (err error) {
	var m struct {
		Room  uint16
		Fired uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
		return fmt.Errorf("error deserializing room kills: %w", err)
	}
	p.Kills = roomKillsRemote{valid: true, room: m.Room, fired: m.Fired}
	return nil
}
