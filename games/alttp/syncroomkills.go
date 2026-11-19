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
// player whose enemies are still alive disagree about the tag. The trap doors these tags open are shared through the
// room state (message 0x1A), but the other outcomes were not: a chest the tag reveals stays hidden, and the block of
// the kill room block tag cannot be pushed. The tags are (zelda3 kDungTagroutines):
//
//   - $01-$0A: trap doors open (Dung_TagRoutine_TrapdoorsUp) once the screen is clear while Link is in the tag's
//     quadrant ($01 NW, $02 NE, $03 SW, $04 SE), half ($05 W, $06 E, $07 N, $08 S) or anywhere ($09); $0A once the
//     whole room is clear.
//   - $26 RoomTag_KillRoomBlock: once the screen is clear while Link is in the south east quadrant, the tag is cleared
//     (which lets the room's block be pushed) and SFX2 $1B plays.
//   - $29-$32: like $01-$0A, but they reveal the room's chests (RoomTag_OperateChestReveal), which clears the tag.
//
// The screen is clear (Sprite_CheckIfScreenIsClear) when no sprite with a state and without $0F60 bit 6 has its
// position on the camera's 256x256 screen and no invisible stalfos ($18) or tile room ($14) overlord is left; the room
// is clear (Sprite_CheckIfRoomIsClear) when no such sprite is left anywhere.
//
// A local trigger is seen from what the tag does, without reading the sprite tables: a chest or block tag going to 0,
// or the trap doors opening locally ($0468 1 -> 0, not from sync) in a room with a trap door kill tag. It is recorded
// as a kill bit: bit q (1 << roomQuadrant) for the quadrant Link was in, or roomKillRoom for a whole room tag.
//
// Message 0x85 (o2-only: `room u16`, `kills u8`) is sent in its own packet to the room's sector once the room state was
// sampled there.
//
// Applying: in submodule 0 of the dungeon module, for a kill bit of a remote player in the same room that we do not
// have, while Link is in that quadrant (or for roomKillRoom anywhere), the enemies still alive here that the tag's
// check counts are killed the way Sprite_KillFriends does (state 6 with the poof, no prize drop; key-carrying enemies
// still drop their keys, which the key flags remove if they were taken). The game's own tag then fires here. For a
// quadrant, the routine kills the sprites on the screen, like the check counts them. Sprites in states below 9
// (spawning, falling, dying, ...) are left for another try a second later; a try that finds nothing left to kill ends
// it. Overlords are left alone (the tile room overlord is synced on its own, see synctiles.go).

const (
	// roomKillRoom is the kill bit of a whole room tag ($0A, $32); bits 0-3 are the quadrants (roomQuadrant):
	roomKillRoom = uint8(0x10)
	// roomKillRetryMs is how long to wait before killing again in the same quadrant after enemies were left:
	roomKillRetryMs = 1000

	// the update routine reports roomKillNone when there was nothing left to kill:
	roomKillNone = uint8(0x02)

	// sprite tables:
	spriteYLo      = uint16(0x0D00)
	spriteXLo      = uint16(0x0D10)
	spriteYHi      = uint16(0x0D20)
	spriteXHi      = uint16(0x0D30)
	spriteState    = uint16(0x0DD0)
	spriteDelay    = uint16(0x0DF0)
	spriteType     = uint16(0x0E20)
	spriteFlags2   = uint16(0x0E40)
	spriteFlags3   = uint16(0x0E60)
	spriteFlags4   = uint16(0x0F60)
	spriteFlags5   = uint16(0x0BE0)
	spriteDeflBits = uint16(0x0CAA)

	// sprite types never killed: Agahnim (skipped by Sprite_KillFriends), the evil barrier, bonk items:
	agahnimSprite     = uint8(0x7A)
	evilBarrierSprite = uint8(0x40)
)

// roomKillsRemote is a remote player's kill triggers as received in message 0x85.
type roomKillsRemote struct {
	valid bool
	room  uint16
	kills uint8
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

// roomKillScope returns the kill bit a kill trigger tag sets when it fires while Link is in quadrant q, if it can fire
// there.
func roomKillScope(tag uint8, q uint8) (bit uint8, ok bool) {
	base := roomKillTagBase(tag)
	switch {
	case base == 0:
		return 0, false
	case base == 0x0A:
		return roomKillRoom, true
	case base <= 0x04:
		ok = q == base-0x01
	case base <= 0x06:
		ok = q&1 == base-0x05
	case base <= 0x08:
		ok = q>>1 == base-0x07
	default: // $09: any quadrant
		ok = true
	}
	return 1 << q, ok
}

// readRoomKills records the kill triggers fired locally. Called from readRoomState in submodule 0, $05 or $17 with the
// room state's door before ($0468 as last sampled) and the current one.
func (g *Game) readRoomKills(tags [2]uint8, prevDoor, door uint8) {
	rs := &g.roomState
	q := roomQuadrant(g.local)
	for k, prev := range rs.tags {
		if prev == 0 || tags[k] != 0 || roomKillTagIsDoor(prev) {
			continue
		}
		// a chest reveal or kill room block tag fired:
		if bit, ok := roomKillScope(prev, q); ok && rs.kills&bit == 0 {
			rs.kills |= bit
			log.Printf("alttp: room: local: kill trigger $%02x fired, kills $%02x\n", prev, rs.kills)
		}
	}
	rs.tags = tags
	if prevDoor == 0 || door != 0 {
		return
	}
	for _, tag := range tags {
		if !roomKillTagIsDoor(tag) {
			continue
		}
		if bit, ok := roomKillScope(tag, q); ok && rs.kills&bit == 0 {
			rs.kills |= bit
			log.Printf("alttp: room: local: kill trigger $%02x opened the trap doors, kills $%02x\n", tag, rs.kills)
		}
	}
}

// roomKillWanted picks a kill bit of a remote player in the room to apply here, if any.
func (g *Game) roomKillWanted(remotes []*Player) (bit uint8, from *Player) {
	rs := &g.roomState
	if g.nowTs() < rs.killRetry {
		return
	}
	// only the quadrant Link is in, where the tag's own check looks:
	want := roomKillRoom | 1<<roomQuadrant(g.local)
	for _, p := range remotes {
		if !p.Kills.valid || p.Kills.room != rs.room {
			continue
		}
		b := p.Kills.kills & want &^ rs.kills &^ rs.killsDone
		if b == 0 {
			continue
		}
		if b&roomKillRoom != 0 {
			return roomKillRoom, p
		}
		return b, p
	}
	return
}

// confirmRoomKill handles the result of the kill part of a room state update.
func (g *Game) confirmRoomKill(u *roomStateUpdate, value uint8) {
	rs := &g.roomState
	if value == roomKillNone {
		rs.killsDone |= u.kill
		log.Printf("alttp: room: update: no enemies left for kills $%02x from player '%s'\n", u.kill, u.killFrom.Name())
		return
	}
	rs.killRetry = g.nowTs() + roomKillRetryMs
	log.Printf("alttp: room: update: killed enemies for kills $%02x from player '%s'\n", u.kill, u.killFrom.Name())
	g.PushNotification(fmt.Sprintf("enemies defeated by %s", u.killFrom.Name()))
}

// emitRoomKill kills the enemies still alive that the kill trigger's check counts, the way Sprite_KillFriends does: the
// whole room for roomKillRoom, otherwise the sprites on the screen. Y counts the sprites killed. Expects 8-bit A and X.
func emitRoomKill(a *asm.Emitter, bit uint8) {
	a.Comment("kill the enemies left that the kill trigger counts (like Sprite_KillFriends):")
	a.LDY_imm8_b(0x00)
	a.LDX_imm8_b(0x0F)
	a.Label("rk_next")
	a.LDA_abs_x(spriteState)
	a.CMP_imm8_b(0x09)
	a.BCC("rk_cont")
	a.LDA_abs_x(spriteFlags4)
	a.AND_imm8_b(0x40)
	a.BNE("rk_cont")
	a.LDA_abs_x(spriteDeflBits)
	a.AND_imm8_b(0x02)
	a.BNE("rk_cont")
	a.LDA_abs_x(spriteType)
	a.CMP_imm8_b(agahnimSprite)
	a.BEQ("rk_cont")
	a.CMP_imm8_b(evilBarrierSprite)
	a.BEQ("rk_cont")
	a.CMP_imm8_b(bonkItemSprite)
	a.BEQ("rk_cont")
	if bit != roomKillRoom {
		a.Comment("on the screen (Sprite_CheckIfScreenIsClear):")
		a.LDA_abs_x(spriteXLo)
		a.SEC()
		a.SBC_dp(0xE2)
		a.LDA_abs_x(spriteXHi)
		a.SBC_dp(0xE3)
		a.BNE("rk_cont")
		a.LDA_abs_x(spriteYLo)
		a.SEC()
		a.SBC_dp(0xE8)
		a.LDA_abs_x(spriteYHi)
		a.SBC_dp(0xE9)
		a.BNE("rk_cont")
	}
	a.LDA_imm8_b(0x06)
	a.STA_abs_x(spriteState)
	a.LDA_imm8_b(0x0F)
	a.STA_abs_x(spriteDelay)
	a.STZ_abs_x(spriteFlags3)
	a.STZ_abs_x(spriteFlags5)
	a.LDA_imm8_b(0x03)
	a.STA_abs_x(spriteFlags2)
	a.INY()
	a.Label("rk_cont")
	a.DEX()
	a.BPL("rk_next")
}

// SerializeRoomKills writes message 0x85 with the kill triggers fired in the room, if the room state was sampled.
func (g *Game) SerializeRoomKills(w io.Writer) (err error) {
	rs := &g.roomState
	if !rs.valid {
		return
	}
	_, err = w.Write([]byte{uint8(MsgRoomKills), uint8(rs.room), uint8(rs.room >> 8), rs.kills})
	return
}

func (g *Game) DeserializeRoomKills(p *Player, r io.Reader) (err error) {
	var m struct {
		Room  uint16
		Kills uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
		return fmt.Errorf("error deserializing room kills: %w", err)
	}
	p.Kills = roomKillsRemote{valid: true, room: m.Room, kills: m.Kills}
	return nil
}
