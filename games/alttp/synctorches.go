package alttp

import (
	"fmt"
	"io"
	"log"

	"github.com/alttpo/snes/asm"
)

// Underworld torch synchronization
//
// The room loader registers the room's lightable torches (RoomDraw_LightableTorch) in the manipulable object slots
// right after the manipulables: torch t is slot $0478/2 + t, with its tilemap position in $0540 (bit 15 set while it is
// lit). Lighting a torch with the lamp or the fire rod (Underworld_LightTorch, $0333 = $C0 + t) sets bit 15, draws the
// lit torch and sets its timer $04F0+t to $C0; Underworld_ProcessTorchesAndDoors decrements the timers every 4 frames
// and extinguishes a torch whose timer reaches 0 (Underworld_ExtinguishTorch). Torches lit by the room loader have a
// timer of 0 and stay lit.
//
// Room tags count the lit torches every frame: RoomTag_TorchPuzzleDoor ($33) opens the trap doors while at least 4 are
// lit and closes them again once fewer are, RoomTag_TorchPuzzleChest ($3E) reveals a chest and the moving wall tags
// ($1E/$1F) move a wall. Sharing the torches (and their timers, so they also go out together) is all it takes for every
// player in the room to see the same doors, e.g. Turtle Rock's room $C7 where 4 torches open the north trap door.
//
// The torches are shared with alttpo (torches.as) in message 0x0A, sent in broadcastSector packets to the current
// room: `count u8`, then per torch `t u8`, `timer u8` for the torches the sender owns. A player owns a torch it lit
// itself; o2 cannot intercept the game's routines like alttpo does, so it takes a torch that turns lit without o2
// lighting it as lit by the local player. The torches of another player in the same room are lit here (unless they
// are about to go out) and their timers are kept in step, and the owner is remembered. A torch whose owner stops
// sending it burns out by itself; one whose owner leaves the room while it is lit becomes ours.

const (
	torchTimers = uint16(0x04F0)
	// torchMaxCount is the number of torch timers; torches also must fit in the 16 manipulable object slots:
	torchMaxCount = 16
	// torchLightMinTimer is the lowest remote timer that lights a torch here. A torch that just burned out here may
	// still be lit with a timer of 1 or 2 for its owner, so lighting it again would only make it go out a second time
	// a few frames later:
	torchLightMinTimer = 8
	// torchTimerSlack is how far a torch's timer may be from its owner's before it is set to the owner's:
	torchTimerSlack = 2
)

// torchesRemote is a remote player's torches as received in message 0x0A.
type torchesRemote struct {
	valid bool
	// owned has bit t set for each torch t the player owns:
	owned  uint16
	timers [torchMaxCount]uint8
}

// torchesLocal tracks the local player's torches in the current room.
type torchesLocal struct {
	// valid is true when the torches were read in the dungeon module for room:
	valid bool
	room  uint16
	// base is the first torch slot ($0478/2) and n the number of torches:
	base int
	n    int

	lit   [torchMaxCount]bool
	timer [torchMaxCount]uint8
	// owner is the player that lit the torch (g.local for us), or nil:
	owner [torchMaxCount]*Player

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *torchesUpdate
}

func (g *Game) resetTorches() {
	tl := &g.torches
	if tl.inflight != nil {
		tl.inflight.cancelled = true
	}
	*tl = torchesLocal{}
}

// torchPeerInRoom reports whether a remote player shares torches with us in room.
func (g *Game) torchPeerInRoom(p *Player, room uint16) bool {
	return p.Team == g.local.Team && p.Module == 0x07 && p.Location&0x010000 != 0 && p.DungeonRoom == room &&
		p.Torches.valid
}

// readTorches samples the local torches and records which ones the local player lit. Called when WRAM is read from the
// SNES.
func (g *Game) readTorches() {
	local := g.local
	tl := &g.torches

	if local.Module == 0x0E {
		// menus don't touch the room:
		return
	}
	if local.Module != 0x07 {
		if tl.valid {
			g.resetTorches()
		}
		return
	}
	if !g.wramFresh[0xA0] || !g.wramFresh[0xA1] || !g.wramFresh[manipCount] || !g.wramFresh[manipCount+1] {
		return
	}
	for o := manipPos; o < manipPos+manipSlots*2; o++ {
		if !g.wramFresh[o] {
			return
		}
	}
	for o := torchTimers; o < torchTimers+torchMaxCount; o++ {
		if !g.wramFresh[o] {
			return
		}
	}
	room := g.wramU16(0xA0)
	if tl.valid && tl.room != room {
		g.resetTorches()
	}
	// room loads happen in other submodules:
	if local.SubModule != 0 {
		return
	}
	if tl.inflight != nil {
		// wait for the update to confirm:
		return
	}

	nm := int(g.wramU16(uint32(manipCount)))
	if nm&1 != 0 || nm > manipSlots<<1 {
		return
	}
	base := nm >> 1
	n := 0
	for i := base; i < manipSlots && n < torchMaxCount; i++ {
		if g.wramU16(uint32(manipPos)+uint32(i<<1))&0x7FFF == 0 {
			break
		}
		n++
	}

	if !tl.valid || tl.base != base || tl.n != n {
		// first observation in this room; torches lit so far were lit by the room loader or before we looked:
		g.resetTorches()
		tl.valid = true
		tl.room = room
		tl.base = base
		tl.n = n
		for t := 0; t < n; t++ {
			tl.lit[t] = g.wramU16(uint32(manipPos)+uint32((base+t)<<1))&0x8000 != 0
			tl.timer[t] = g.wram[uint32(torchTimers)+uint32(t)]
		}
		return
	}

	for t := 0; t < n; t++ {
		lit := g.wramU16(uint32(manipPos)+uint32((base+t)<<1))&0x8000 != 0
		timer := g.wram[uint32(torchTimers)+uint32(t)]
		if lit && !tl.lit[t] {
			tl.owner[t] = local
			log.Printf("alttp: torches: room $%03x: torch %d lit\n", room, t)
		} else if !lit && tl.lit[t] {
			tl.owner[t] = nil
			log.Printf("alttp: torches: room $%03x: torch %d went out\n", room, t)
		}
		tl.lit[t] = lit
		tl.timer[t] = timer
	}

	// let go of torches whose owners stopped sending them or left the room:
	for t := 0; t < n; t++ {
		p := tl.owner[t]
		if p == nil || p == local {
			continue
		}
		if g.isRemotePlayerActive(p) && g.torchPeerInRoom(p, room) {
			if p.Torches.owned&(1<<t) == 0 {
				// it went out for its owner (or somebody else lit it too); ours burns out by itself:
				tl.owner[t] = nil
			}
			continue
		}
		if tl.lit[t] && tl.timer[t] > 0 {
			// keep it burning for the others in the room:
			tl.owner[t] = local
		} else {
			tl.owner[t] = nil
		}
	}
}

// isRemotePlayerActive reports whether p is still one of the active remote players.
func (g *Game) isRemotePlayerActive(p *Player) bool {
	for _, rp := range g.RemotePlayers() {
		if rp == p {
			return true
		}
	}
	return false
}

// torchesUpdate is a generated update routine for the torches awaiting execution confirmation.
type torchesUpdate struct {
	g    *Game
	room uint16
	// light is the torch lit by the routine, or -1:
	light int
	// timers has bit t set for each torch whose timer is set to timer[t]:
	timers    uint16
	timer     [torchMaxCount]uint8
	owner     [torchMaxCount]*Player
	cancelled bool
}

func (u *torchesUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	tl := &u.g.torches
	if u.cancelled || tl.inflight != u {
		return
	}
	tl.inflight = nil

	if value == crystalSwitchSkipped {
		return
	}
	u.commit()
	if u.light >= 0 {
		p := u.owner[u.light]
		log.Printf("alttp: torches: room $%03x: torch %d lit from player '%s'\n", u.room, u.light, p.Name())
	}
}

// commit records what the routine did: the torches it lit or set the timer of belong to the remote players they came
// from, so they are not taken as lit by the local player.
func (u *torchesUpdate) commit() {
	tl := &u.g.torches
	if !tl.valid || tl.room != u.room {
		return
	}
	for t := 0; t < tl.n; t++ {
		if u.timers&(1<<t) == 0 {
			continue
		}
		tl.owner[t] = u.owner[t]
		tl.timer[t] = u.timer[t]
		if t == u.light {
			tl.lit[t] = true
		}
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *torchesUpdate) AbortAsm() {
	tl := &u.g.torches
	if u.cancelled || tl.inflight != u {
		return
	}
	tl.inflight = nil
	// assume it ran; a torch it did not light reads as unlit and is lit again by a later update:
	u.commit()
}

// generateTorchesUpdate generates an update routine that lights the torches of the remote players in the same room and
// keeps their timers in step. It has no side effects; the caller must set inflight if the routine is emitted.
func (g *Game) generateTorchesUpdate(newEmitter func() *asm.Emitter, index uint32) (u *torchesUpdate, a *asm.Emitter) {
	tl := &g.torches
	local := g.local
	if !tl.valid || tl.inflight != nil || tl.n == 0 {
		return
	}
	if tl.room == 0 {
		// Ganon's room handles its torches on its own:
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 {
		return
	}
	fnLight, ok := g.romFunctions[fnUnderworldLightTorch]
	if !ok {
		return
	}

	u = &torchesUpdate{g: g, room: tl.room, light: -1}
	for t := 0; t < tl.n; t++ {
		for _, p := range g.RemotePlayers() {
			if !g.torchPeerInRoom(p, tl.room) || p.Torches.owned&(1<<t) == 0 {
				continue
			}
			timer := p.Torches.timers[t]
			if timer == 0 {
				continue
			}
			if !tl.lit[t] {
				// light only one torch per frame; they all draw through the same tilemap buffer at $1100:
				if u.light >= 0 || timer < torchLightMinTimer {
					break
				}
				u.light = t
			} else if tl.owner[t] == p && int(timer) >= int(tl.timer[t])-torchTimerSlack && int(timer) <= int(tl.timer[t])+torchTimerSlack {
				// in step:
				break
			}
			u.timers |= 1 << t
			u.timer[t] = timer
			u.owner[t] = p
			break
		}
	}
	if u.timers == 0 {
		return nil, nil
	}

	a = newEmitter()
	a.Comment(fmt.Sprintf("torches in room $%03x:", tl.room))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("tt_skip")
	a.LDA_dp(0x11)
	a.BNE("tt_skip")
	a.Comment("still in the same room:")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(tl.room))
	a.BNE("tt_skip")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(tl.room >> 8))
	a.BNE("tt_skip")
	a.LDA_abs(manipCount)
	a.CMP_imm8_b(uint8(tl.base << 1))
	a.BNE("tt_skip")
	if u.light >= 0 {
		a.Comment(fmt.Sprintf("light torch %d from %s (Underworld_LightTorch):", u.light, u.owner[u.light].Name()))
		a.LDA_imm8_b(0xC0 + uint8(u.light))
		a.STA_abs(0x0333)
		a.JSL(fnLight)
		a.STZ_abs(0x0333)
	}
	for t := 0; t < tl.n; t++ {
		if u.timers&(1<<t) == 0 {
			continue
		}
		a.Comment(fmt.Sprintf("torch %d timer from %s:", t, u.owner[t].Name()))
		a.LDA_imm8_b(u.timer[t])
		a.STA_abs(torchTimers + uint16(t))
	}
	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("tt_report")
	a.Label("tt_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("tt_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}

// SerializeTorches writes message 0x0A with the torches the local player owns in the current room.
func (g *Game) SerializeTorches(w io.Writer) (err error) {
	tl := &g.torches
	b := []byte{uint8(MsgTorches), 0}
	if tl.valid && tl.room != 0 {
		for t := 0; t < tl.n; t++ {
			if tl.owner[t] != g.local || !tl.lit[t] || tl.timer[t] == 0 {
				continue
			}
			b = append(b, uint8(t), tl.timer[t])
			b[1]++
		}
	}
	_, err = w.Write(b)
	return
}

// DeserializeTorches reads message 0x0A: the torches the sender owns in its current room.
func (g *Game) DeserializeTorches(p *Player, r io.Reader) (err error) {
	var count uint8
	if count, err = readU8(r); err != nil {
		return fmt.Errorf("error deserializing torches: %w", err)
	}
	m := torchesRemote{valid: true}
	for i := uint8(0); i < count; i++ {
		var torch [2]byte
		if _, err = io.ReadFull(r, torch[:]); err != nil {
			return fmt.Errorf("error deserializing torches: %w", err)
		}
		if torch[0] >= torchMaxCount {
			continue
		}
		m.owned |= 1 << torch[0]
		m.timers[torch[0]] = torch[1]
	}
	p.Torches = m
	return nil
}
