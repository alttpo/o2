package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"io"
	"log"
)

// Crystal switch synchronization
//
// $7EC172 holds the state of the dungeon's crystal switches (which color of pegs is raised). It is shared with alttpo
// (LocalGameState.as `crystal`, a SyncableByte) as a single last-writer-wins value in a WRAM syncables message (0x05):
//
//   - $7EC172 is only meaningful in the dungeon module ($07); the overworld uses it as scratch space. Any change seen
//     while in the dungeon module (the player hit a crystal switch) is timestamped with the server time.
//   - The dungeon load (module $06) resets $7EC172, so leaving the dungeon module (other than for the menus in module
//     $0E) forgets the timestamp; the first value read back in the dungeon module is only a baseline.
//   - While in the dungeon module with submodule 0 and no animation running ($0112), take the value of the remote
//     player in the same dungeon ($040C) and in the dungeon module with the latest timestamp (or, on a timestamp tie,
//     a higher value). If it differs from ours, write it and switch to submodule $16 which redraws the pegs, just like
//     the game does when a crystal switch is hit.
//
// Timestamps are milliseconds; alttpo uses its local clock while o2 uses its estimate of the server clock.

const crystalSwitchOffs = uint16(0xC172)

// crystalSwitchLocal tracks the local player's crystal switch state.
type crystalSwitchLocal struct {
	// valid is true when value was read from the SNES in the dungeon module for dungeon:
	valid   bool
	dungeon uint8
	value   uint8
	// ts is the time (msec) of the last change in this dungeon visit; 0 means no change:
	ts uint32

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *crystalSwitchUpdate
}

// resetCrystalSwitch forgets the crystal switch state; it is re-read from the SNES as a baseline.
func (g *Game) resetCrystalSwitch() {
	cs := &g.crystalSwitch
	if cs.inflight != nil {
		cs.inflight.cancelled = true
	}
	*cs = crystalSwitchLocal{}
}

// readCrystalSwitch samples $7EC172 from WRAM and records any change made by the local player.
// Called when WRAM is read from the SNES.
func (g *Game) readCrystalSwitch() {
	local := g.local
	cs := &g.crystalSwitch

	if local.Module == 0x0E {
		// menus don't touch the crystal switch state; keep it:
		return
	}
	if local.Module != 0x07 {
		// left the dungeon module; the next dungeon load resets the state:
		cs.valid = false
		cs.ts = 0
		return
	}
	if cs.inflight != nil {
		// wait for the update to confirm:
		return
	}
	if !g.wramFresh[crystalSwitchOffs] || !g.wramFresh[0x040C] {
		return
	}

	dungeon := g.wram[0x040C]
	v := g.wram[crystalSwitchOffs]
	if !cs.valid || cs.dungeon != dungeon {
		// first observation in this dungeon; nothing to attribute:
		cs.valid = true
		cs.dungeon = dungeon
		cs.value = v
		cs.ts = 0
		return
	}
	if v == cs.value {
		return
	}

	cs.value = v
	cs.ts = timestampFromTime(g.ServerSNESTimestamp())
	if cs.ts == 0 {
		cs.ts = 1
	}
	log.Printf("alttp: crystal: local: $%02x\n", v)
}

// crystalSwitchWinner returns the remote player whose crystal switch state we should take, if any.
func (g *Game) crystalSwitchWinner() (winner *Player, value uint8, ts uint32) {
	local := g.local
	cs := &g.crystalSwitch

	tsCompare := cs.ts
	for _, p := range g.RemotePlayers() {
		if p.Team != local.Team {
			continue
		}
		if p.Module != 0x07 || uint8(p.Dungeon) != cs.dungeon {
			continue
		}
		w, ok := p.WRAM[crystalSwitchOffs]
		if !ok || w == nil {
			continue
		}

		rv, rts := uint8(w.Value), w.Timestamp
		if rts > tsCompare {
			winner, value, ts = p, rv, rts
			tsCompare = rts
		} else if rts == cs.ts && rv > cs.value {
			// same as alttpo's SyncableByte.compareTo; somebody may have a larger value that we overwrote:
			winner, value, ts = p, rv, rts
		}
	}
	return
}

// crystalSwitchUpdate is a generated update routine for the crystal switch state awaiting execution confirmation.
type crystalSwitchUpdate struct {
	g         *Game
	dungeon   uint8
	value     uint8
	ts        uint32
	from      *Player
	cancelled bool
}

const (
	crystalSwitchSkipped = 0x00
	crystalSwitchWritten = 0x01
	crystalSwitchSame    = 0x02
)

func (u *crystalSwitchUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	cs := &g.crystalSwitch
	if u.cancelled || cs.inflight != u {
		return
	}
	cs.inflight = nil

	if value == crystalSwitchSkipped {
		// conditions were not right; try again later:
		log.Printf("alttp: crystal: update to $%02x skipped\n", u.value)
		return
	}
	if !cs.valid || cs.dungeon != u.dungeon {
		return
	}

	cs.value = u.value
	cs.ts = u.ts
	if value == crystalSwitchWritten {
		log.Printf("alttp: crystal: update: $%02x from player '%s'\n", u.value, u.from.Name())
		g.PushNotification(fmt.Sprintf("toggled crystal switches from %s", u.from.Name()))
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *crystalSwitchUpdate) AbortAsm() {
	cs := &u.g.crystalSwitch
	if u.cancelled || cs.inflight != u {
		return
	}
	cs.inflight = nil
	// we cannot know whether the routine executed; re-read the state as a new baseline:
	cs.valid = false
	cs.ts = 0
}

// generateCrystalSwitchUpdate generates an update routine to take the latest crystal switch state of the remote
// players in the same dungeon. It has no side effects; the caller must set inflight if the routine is emitted.
func (g *Game) generateCrystalSwitchUpdate(newEmitter func() *asm.Emitter, index uint32) (u *crystalSwitchUpdate, a *asm.Emitter) {
	cs := &g.crystalSwitch
	local := g.local
	if !cs.valid || cs.inflight != nil {
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 || g.wramU16(0x0112) != 0 {
		return
	}

	winner, value, ts := g.crystalSwitchWinner()
	if winner == nil {
		return
	}
	if value == cs.value {
		// already in agreement; keep the latest timestamp:
		cs.ts = ts
		return
	}

	u = &crystalSwitchUpdate{g: g, dungeon: cs.dungeon, value: value, ts: ts, from: winner}

	a = newEmitter()
	a.Comment(fmt.Sprintf("crystal switches = $%02x from %s:", value, winner.Name()))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("cs_skip")
	a.LDA_dp(0x11)
	a.BNE("cs_skip")
	a.Comment("no animation running:")
	a.LDA_abs(0x0112)
	a.ORA_long(0x7E0113)
	a.BNE("cs_skip")
	a.Comment("still in the same dungeon:")
	a.LDA_abs(0x040C)
	a.CMP_imm8_b(cs.dungeon)
	a.BNE("cs_skip")
	a.LDA_long(0x7E0000 + uint32(crystalSwitchOffs))
	a.CMP_imm8_b(value)
	a.BEQ("cs_same")
	a.LDA_imm8_b(value)
	a.STA_long(0x7E0000 + uint32(crystalSwitchOffs))
	a.Comment("redraw the pegs like hitting a crystal switch does:")
	a.LDA_imm8_b(0x16)
	a.STA_dp(0x11)
	a.LDA_imm8_b(0x25) // SFX3.25
	a.STA_abs(0x012F)
	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("cs_report")
	a.Label("cs_same")
	a.LDA_imm8_b(crystalSwitchSame)
	a.BRA("cs_report")
	a.Label("cs_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("cs_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}

// SerializeCrystalSwitch writes the crystal switch state as a WRAM syncables message (0x05) like alttpo's
// LocalGameState::serialize_crystals.
func (g *Game) SerializeCrystalSwitch(w io.Writer) (err error) {
	cs := &g.crystalSwitch
	value, ts := cs.value, cs.ts
	if !cs.valid {
		value, ts = 0, 0
	}

	if err = binary.Write(w, binary.LittleEndian, uint8(MsgWRAM)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, uint8(1)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, crystalSwitchOffs); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, ts); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, uint16(value)); err != nil {
		return
	}
	return
}
