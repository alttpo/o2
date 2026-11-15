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
// tile graphics (ToggleStarTileGraphics) and switches to submodule $03 which draws the overlay. The state is reset
// whenever a room is loaded, so it only means something for the current room. It is shared with alttpo
// (LocalGameState.as `star`) as a last-writer-wins value:
//
//   - It is sent in a WRAM syncables message (0x05) for offset $04BC in a broadcastSector packet to the current room's
//     sector. The u16 value carries $04BC in the low byte and the overlay $04BA in the high byte, which a remote player
//     needs to draw the same holes.
//   - Changes are only sampled in the dungeon module ($07) during submodule 0 or $03 (stepping on a star tile). Room
//     loads happen in other submodules, and a change of room ($A0) only records a new baseline.
//   - While in the dungeon module with submodule 0 and no animation running ($0112), take the state of the remote
//     player in the same room and in the dungeon module with the latest timestamp (or, on a timestamp tie, a higher
//     value). If it differs from ours, do what stepping on the star tile does: set $04BA and $04BC, swap the star tile
//     graphics and switch to submodule $03.

const starSwitchOffs = uint16(0x04BC)
const starOverlayOffs = uint16(0x04BA)

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
		w, ok := p.WRAM[starSwitchOffs]
		if !ok || w == nil {
			continue
		}

		rv, rts := uint8(w.Value), w.Timestamp
		if rts > tsCompare {
			winner, value, overlay, ts = p, rv, uint8(w.Value>>8), rts
			tsCompare = rts
		} else if rts == ss.ts && rv > ss.value {
			// same as alttpo's SyncableByte.compareTo; somebody may have a larger value that we overwrote:
			winner, value, overlay, ts = p, rv, uint8(w.Value>>8), rts
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

// SerializeStarSwitch writes the star switch state as a WRAM syncables message (0x05) like alttpo's
// LocalGameState::serialize_star; the value's low byte is $04BC and its high byte is $04BA.
func (g *Game) SerializeStarSwitch(w io.Writer) (err error) {
	ss := &g.starSwitch
	value, ts := uint16(ss.value)|uint16(ss.overlay)<<8, ss.ts
	if !ss.valid {
		value, ts = 0, 0
	}

	if err = binary.Write(w, binary.LittleEndian, uint8(MsgWRAM)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, uint8(1)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, starSwitchOffs); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, ts); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, value); err != nil {
		return
	}
	return
}
