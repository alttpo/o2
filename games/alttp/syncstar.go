package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"io"
	"log"
)

// Star switch synchronization
//
// Star tiles (Misery Mire, Turtle Rock, ...) open and close holes in the current room. Stepping on a star tile
// (RoomTag_TriggerHoles) picks the hole overlay for the tile ($04BA), toggles the star tile state $04BC, swaps the star
// tile graphics (ToggleStarTileGraphics) and switches to submodule $03 which draws the overlay. The state only means
// something for the current room: every room load clears the overlay $04BA (Underworld_LoadRoom), and most of them
// clear $04BC too (ResetStarTileGraphics), but falling through a hole (Module07_11) keeps $04BC. It is shared with
// alttpo (StarSwitch.as) as a last-writer-wins value:
//
//   - It is sent in message 0x19 (`room u16`, `timestamp u32`, `value u8` ($04BC), `overlay u8` ($04BA)) in a
//     broadcastSector packet to the current room's sector, only once it was sampled in that room. The room tells
//     receivers which room the state belongs to, since the sender's location and its last star message can be from
//     different rooms for a while. A receiver also drops a player's star state whenever that player's location changes.
//   - Changes are only sampled in the dungeon module ($07) during submodule 0 or $03 (stepping on a star tile). Room
//     loads happen in other submodules (and still hold the previous room's overlay), and a change of room ($A0) only
//     records a new baseline with timestamp 0.
//   - While in the dungeon module with submodule 0 and no animation running ($0112), take the state of the remote
//     player in the same room and in the dungeon module with the latest nonzero timestamp (or, on a tie with our own
//     timestamp, a higher value). A timestamp of 0 means the player did not step on a star tile in this room visit, so
//     such a state never wins. If it differs from ours, do what stepping on the star tile does: set $04BA and $04BC,
//     swap the star tile graphics and switch to submodule $03.

const starSwitchOffs = uint16(0x04BC)
const starOverlayOffs = uint16(0x04BA)

// starSwitchRemote is a remote player's star switch state as received.
type starSwitchRemote struct {
	valid   bool
	room    uint16
	ts      uint32
	value   uint8
	overlay uint8
}

// starSwitchLocal tracks the local player's star switch state.
type starSwitchLocal struct {
	// valid is true when value was read from the SNES in the dungeon module for room:
	valid bool
	room  uint16
	value uint8
	// overlay is $04BA, the hole overlay drawn for the current star tile state:
	overlay uint8
	// ts is the time (msec) of the last change in this room; 0 means no change:
	ts uint32

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *starSwitchUpdate
}

// resetStarSwitch forgets the star switch state; it is re-read from the SNES as a baseline.
func (g *Game) resetStarSwitch() {
	ss := &g.starSwitch
	if ss.inflight != nil {
		ss.inflight.cancelled = true
	}
	*ss = starSwitchLocal{}
}

// readStarSwitch samples $04BA and $04BC from WRAM and records any change made by the local player.
// Called when WRAM is read from the SNES.
func (g *Game) readStarSwitch() {
	local := g.local
	ss := &g.starSwitch

	if local.Module == 0x0E {
		// menus don't touch the star switch state; keep it:
		return
	}
	if local.Module != 0x07 {
		// left the dungeon module; the next room load resets the state:
		ss.valid = false
		ss.ts = 0
		return
	}
	if !g.wramFresh[0xA0] || !g.wramFresh[0xA1] {
		return
	}
	room := g.wramU16(0xA0)
	if ss.valid && ss.room != room {
		// a room transition started (or happened between reads); the room load resets the state, so it must not be
		// mistaken for a change in the room we last saw:
		ss.valid = false
		ss.ts = 0
	}
	if ss.inflight != nil {
		// wait for the update to confirm:
		return
	}
	// room loads reset the state in other submodules; only sample while playing or right after stepping on a star:
	if local.SubModule != 0x00 && local.SubModule != 0x03 {
		return
	}
	if !g.wramFresh[starOverlayOffs] || !g.wramFresh[starSwitchOffs] {
		return
	}

	v := g.wram[starSwitchOffs]
	overlay := g.wram[starOverlayOffs]
	if !ss.valid || ss.room != room {
		// first observation in this room; nothing to attribute:
		ss.valid = true
		ss.room = room
		ss.value = v
		ss.overlay = overlay
		ss.ts = 0
		return
	}
	ss.overlay = overlay
	if v == ss.value {
		return
	}

	ss.value = v
	ss.ts = timestampFromTime(g.ServerSNESTimestamp())
	if ss.ts == 0 {
		ss.ts = 1
	}
	log.Printf("alttp: star: local: $%02x (overlay $%02x)\n", v, overlay)
}

// starSwitchWinner returns the remote player whose star switch state we should take, if any.
func (g *Game) starSwitchWinner() (winner *Player, value, overlay uint8, ts uint32) {
	local := g.local
	ss := &g.starSwitch

	tsCompare := ss.ts
	for _, p := range g.RemotePlayers() {
		if p.Team != local.Team {
			continue
		}
		if p.Module != 0x07 || p.Location&0x010000 == 0 || p.DungeonRoom != ss.room {
			continue
		}
		rs := &p.Star
		if !rs.valid || rs.room != ss.room || rs.ts == 0 {
			// not from this room, or nobody stepped on a star tile there:
			continue
		}

		if rs.ts > tsCompare {
			winner, value, overlay, ts = p, rs.value, rs.overlay, rs.ts
			tsCompare = rs.ts
		} else if rs.ts == ss.ts && rs.value > ss.value {
			// somebody may have a larger value that we overwrote:
			winner, value, overlay, ts = p, rs.value, rs.overlay, rs.ts
		}
	}
	return
}

// starSwitchUpdate is a generated update routine for the star switch state awaiting execution confirmation.
type starSwitchUpdate struct {
	g         *Game
	room      uint16
	value     uint8
	overlay   uint8
	ts        uint32
	from      *Player
	cancelled bool
}

func (u *starSwitchUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	ss := &g.starSwitch
	if u.cancelled || ss.inflight != u {
		return
	}
	ss.inflight = nil

	if value == crystalSwitchSkipped {
		// conditions were not right; try again later:
		log.Printf("alttp: star: update to $%02x skipped\n", u.value)
		return
	}
	if !ss.valid || ss.room != u.room {
		return
	}

	ss.value = u.value
	ss.overlay = u.overlay
	ss.ts = u.ts
	if value == crystalSwitchWritten {
		log.Printf("alttp: star: update: $%02x (overlay $%02x) from player '%s'\n", u.value, u.overlay, u.from.Name())
		g.PushNotification(fmt.Sprintf("toggled star tiles from %s", u.from.Name()))
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *starSwitchUpdate) AbortAsm() {
	ss := &u.g.starSwitch
	if u.cancelled || ss.inflight != u {
		return
	}
	ss.inflight = nil
	// we cannot know whether the routine executed; re-read the state as a new baseline:
	ss.valid = false
	ss.ts = 0
}

// generateStarSwitchUpdate generates an update routine to take the latest star switch state of the remote players in
// the same room. It has no side effects; the caller must set inflight if the routine is emitted.
func (g *Game) generateStarSwitchUpdate(newEmitter func() *asm.Emitter, index uint32) (u *starSwitchUpdate, a *asm.Emitter) {
	ss := &g.starSwitch
	local := g.local
	if !ss.valid || ss.inflight != nil {
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 || g.wramU16(0x0112) != 0 {
		return
	}
	fnToggle, ok := g.romFunctions[fnToggleStarTileGraphics]
	if !ok {
		return
	}

	winner, value, overlay, ts := g.starSwitchWinner()
	if winner == nil {
		return
	}
	if value == ss.value {
		// already in agreement; keep the latest timestamp:
		ss.ts = ts
		return
	}
	if overlay == 0 {
		// no overlay to draw; the remote state is not from stepping on a star tile:
		return
	}

	u = &starSwitchUpdate{g: g, room: ss.room, value: value, overlay: overlay, ts: ts, from: winner}

	a = newEmitter()
	a.Comment(fmt.Sprintf("star tiles = $%02x, overlay $%02x from %s:", value, overlay, winner.Name()))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("ss_skip")
	a.LDA_dp(0x11)
	a.BNE("ss_skip")
	a.Comment("no animation running:")
	a.LDA_abs(0x0112)
	a.ORA_long(0x7E0113)
	a.BNE("ss_skip")
	a.Comment("still in the same room:")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(ss.room))
	a.BNE("ss_skip")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(ss.room >> 8))
	a.BNE("ss_skip")
	a.LDA_abs(starSwitchOffs)
	a.CMP_imm8_b(value)
	a.BEQ("ss_same")
	a.Comment("do what stepping on the star tile does (RoomTag_TriggerHoles):")
	a.LDA_imm8_b(overlay)
	a.STA_abs(starOverlayOffs)
	a.STZ_dp(0xBA)
	a.STZ_dp(0xBB)
	a.STZ_dp(0xB0)
	a.LDA_imm8_b(0x1B) // SFX3.1B
	a.STA_abs(0x012F)
	a.LDA_imm8_b(0x03)
	a.STA_dp(0x11)
	a.LDA_imm8_b(value)
	a.STA_abs(starSwitchOffs)
	a.JSL(fnToggle)
	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("ss_report")
	a.Label("ss_same")
	a.LDA_imm8_b(crystalSwitchSame)
	a.BRA("ss_report")
	a.Label("ss_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("ss_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}

// SerializeStarSwitch writes message 0x19 with the star switch state, if it was sampled in the current room.
func (g *Game) SerializeStarSwitch(w io.Writer) (err error) {
	ss := &g.starSwitch
	if !ss.valid {
		return
	}
	b := []byte{uint8(MsgStarSwitch), uint8(ss.room), uint8(ss.room >> 8)}
	b = binary.LittleEndian.AppendUint32(b, ss.ts)
	b = append(b, ss.value, ss.overlay)
	_, err = w.Write(b)
	return
}

func (g *Game) DeserializeStarSwitch(p *Player, r io.Reader) (err error) {
	var m struct {
		Room    uint16
		Ts      uint32
		Value   uint8
		Overlay uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
		return fmt.Errorf("error deserializing star switch: %w", err)
	}
	p.Star = starSwitchRemote{valid: true, room: m.Room, ts: m.Ts, value: m.Value, overlay: m.Overlay}
	return nil
}
