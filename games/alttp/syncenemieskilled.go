package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/alttpo/snes/asm"
	"o2/snes"
)

// Enemies killed: which enemies of a room or an area players in it killed
//
// Enemies themselves are not synced (alttpo's LTTP enemy sync shares their whole state; o2 does not), but the game's
// memory of which enemies were killed is, so that enemies another player in the same room or area killed stay dead
// the next time the game would spawn them. Enemies alive on the local screen are left alone.
//
//   - Underworld: $7FDF80 is a [$128]uint16 array indexed by room ($048E): bit k is set when the room's k-th sprite
//     (sprite_N $0BC0,X) dies (Sprite_ManuallySetDeathFlagUW), and Dungeon_LoadSingleSprite does not spawn it while
//     set. The game clears a room's mask once it falls out of the last 4 rooms visited ($0B80) and all of them when
//     the overworld loads (Sprite_ResetAll).
//   - Overworld: $7FEF80 is a bitmap of the area's sprite positions (blk, from Overworld_LoadProximaSpriteIfAlive; bit
//     $80 >> (blk & 7) of byte blk >> 3) that were loaded. A sprite going off screen clears its bit
//     (Sprite_KillSelf) and so does an overlord; a sprite that dies leaves it set, so it does not spawn again until the
//     area reloads (Sprite_ResetAll). A killed position is thus a bit that is set while no live sprite ($0DD0,X != 0
//     with $0BC0,X as a uint16 blk) nor overlord ($0B00,X != 0 with $0B48,X as a uint16 blk) has it, in two reads in a
//     row (the bitmap and the slots may be read in different frames). Only bytes $00-$BF are used by an area.
//
// Message 0x1F, shared with alttpo (EnemiesKilled.as), is sent in its own broadcastSector packet to the room's or the
// area's sector every 4 frames:
//
//   - `kind u8` = 0 (underworld), `room u16`, `mask u16`: $7FDF80 + room * 2.
//   - `kind u8` = 1 (overworld), `loc u24` (see owLocation), `count u8`, `blk u16` * count: the killed positions.
//
// Applying, in submodule 0 of the dungeon module ($07) or the overworld module ($09), for remote players on the same
// team in the same room or area: the bits they killed that are not set here are set. With Kill Dead Enemies, the live
// sprites they killed are listed in a table for the ROM routine killDeadEnemies (emitKillDeadEnemies), which the update
// routine calls through the SRAM stub, so that the update routine's size does not depend on how many there are.

const (
	enemiesKilledUW        = uint8(0)
	enemiesKilledOW        = uint8(1)
	enemiesKilledRoomMasks = uint32(0x1DF80) // $7FDF80
	enemiesKilledOwLoaded  = uint32(0x1EF80) // $7FEF80
	enemiesKilledOwBytes   = 0xC0
	// the most positions sent and the most bitmap bytes one update routine writes:
	enemiesKilledMaxBlks = 64
	// the game remembers the masks of the last 4 rooms visited ($0B80..$0B87, most recent first, $FFFF for none):
	enemiesKilledRecentRooms = 4
	enemiesKilledRecentAddr  = 0x0B80
	// a received room's mask is used for this long after it was last received; the player's current room is sent every
	// 16 frames and the others every 48:
	enemiesKilledRoomTTL  = 2 * time.Second
	enemiesKilledMaxBytes = 16
)

// enemiesKilledRemote is a remote player's killed enemies as received in message 0x1F.
type enemiesKilledRemote struct {
	valid bool
	kind  uint8
	// id is the room (underworld) or the location (overworld):
	id   uint32
	mask uint16
	blks []uint16
	// uw are the underworld rooms' masks last received, of the player's current room and of the others of the last
	// enemiesKilledRecentRooms rooms the game remembers (see enemiesKilledLocal.recent):
	uw [enemiesKilledRecentRooms]enemiesKilledRemoteRoom
}

type enemiesKilledRemoteRoom struct {
	room, mask uint16
	at         time.Time
}

// setUW records a room's mask, replacing the room's entry or else the oldest.
func (r *enemiesKilledRemote) setUW(room, mask uint16, now time.Time) {
	k := 0
	for i := range r.uw {
		if !r.uw[i].at.IsZero() && r.uw[i].room == room {
			k = i
			break
		}
		if r.uw[i].at.Before(r.uw[k].at) {
			k = i
		}
	}
	r.uw[k] = enemiesKilledRemoteRoom{room: room, mask: mask, at: now}
}

// uwMask returns the room's mask if received in the last enemiesKilledRoomTTL.
func (r *enemiesKilledRemote) uwMask(room uint16, now time.Time) (mask uint16, ok bool) {
	for i := range r.uw {
		if !r.uw[i].at.IsZero() && r.uw[i].room == room && now.Sub(r.uw[i].at) < enemiesKilledRoomTTL {
			return r.uw[i].mask, true
		}
	}
	return 0, false
}

// enemiesKilledLocal tracks the local player's killed enemies.
type enemiesKilledLocal struct {
	// valid is true when sampled in the room or area id:
	valid bool
	kind  uint8
	id    uint32
	mask  uint16
	// extra are the room's sprites killed that the game has not marked in mask: enemies that carried a key, which became
	// the key drop (see keyDropKilled); kept until the room changes:
	extra uint16
	// the overworld positions killed (seen twice in a row) and the candidates of the last read:
	killed [enemiesKilledOwBytes]uint8
	cand   [enemiesKilledOwBytes]uint8

	// recent are the masks of the last rooms visited, which are shared with players entering those rooms (sampled in
	// the underworld; the first is the current room):
	recent [enemiesKilledRecentRooms]struct {
		valid      bool
		room, mask uint16
	}

	inflight *enemiesKilledUpdate
}

// resetEnemiesKilled forgets the killed enemies; they are re-read from the SNES.
func (g *Game) resetEnemiesKilled() {
	ek := &g.enemiesKilled
	if ek.inflight != nil {
		ek.inflight.cancelled = true
	}
	*ek = enemiesKilledLocal{}
}

// enqueueEnemiesKilledReads reads the room's mask in the underworld, or the area's bitmap and the sprite and overlord
// slots in the overworld.
func (g *Game) enqueueEnemiesKilledReads(q []snes.Read) []snes.Read {
	if !g.SyncEnemiesKilled {
		return q
	}
	switch g.local.Module {
	case 0x07:
		room := uint32(g.wramU16(0x048E))
		if room >= 0x128 {
			return q
		}
		q = g.readEnqueue(q, 0xF50000+enemiesKilledRoomMasks+room*2, 2, nil)
		// the last rooms visited, and the mask of one of the other three each frame:
		q = g.readEnqueue(q, 0xF50000+enemiesKilledRecentAddr, 2*enemiesKilledRecentRooms, nil)
		if r := g.wramU16(enemiesKilledRecentAddr + 2*uint32(g.recentReadSlot())); r < 0x128 {
			q = g.readEnqueue(q, 0xF50000+enemiesKilledRoomMasks+uint32(r)*2, 2, nil)
		}
		// the sprites, for the enemies that became key drops (see keyDropKilled) and to kill (Kill Dead Enemies):
		q = g.readEnqueue(q, 0xF50BC0, 0x10, nil)    // sprite indexes in the room
		q = g.readEnqueue(q, 0xF50DD0, 0x10, nil)    // sprite states
		return g.readEnqueue(q, 0xF50E20, 0x20, nil) // sprite types, and $0E30 with the index of a key drop
	case 0x09:
		q = g.readEnqueue(q, 0xF50000+enemiesKilledOwLoaded, enemiesKilledOwBytes, nil)
		// $0B00 overlord types .. $0B48 overlord positions .. $0BC0 sprite positions:
		q = g.readEnqueue(q, 0xF50B00, 0xE0, nil) // [$0B00..$0BDF]
		q = g.readEnqueue(q, 0xF50DD0, 0x10, nil)
		if g.KillDeadEnemies {
			q = g.readEnqueue(q, 0xF50E20, 0x10, nil) // sprite types
		}
		return q
	}
	return q
}

// readEnemiesKilled samples the killed enemies of the local player's room or area. Called when WRAM is read.
func (g *Game) readEnemiesKilled() {
	ek := &g.enemiesKilled
	local := g.local
	if !g.SyncEnemiesKilled || local.SubModule != 0 {
		return
	}

	switch local.Module {
	case 0x07:
		g.readEnemiesKilledRecent()
		if !g.wramFresh[0x048E] || !g.wramFresh[0x048F] || !g.wramFresh[0xA0] || !g.wramFresh[0xA1] {
			return
		}
		room := g.wramU16(0x048E)
		if room != g.wramU16(0xA0) || room >= 0x128 {
			return
		}
		o := enemiesKilledRoomMasks + uint32(room)*2
		if !g.wramFresh[o] || !g.wramFresh[o+1] {
			return
		}
		mask := g.wramU16(o)
		same := ek.valid && ek.kind == enemiesKilledUW && ek.id == uint32(room)
		if !same {
			ek.extra = 0
		}
		extra := ek.extra | g.keyDropKilled()
		if same && ek.mask == mask && ek.extra == extra {
			return
		}
		if same && (mask&^ek.mask != 0 || extra&^ek.extra != 0) {
			log.Printf("alttp: enemies killed: local: room $%03x: $%04x -> $%04x (key drops $%04x)\n", room, ek.mask, mask, extra)
		}
		ek.valid, ek.kind, ek.id, ek.mask, ek.extra = true, enemiesKilledUW, uint32(room), mask, extra

	case 0x09:
		if g.wram[0x1B] != 0 {
			return
		}
		loc, ok := g.owLocation()
		if !ok {
			return
		}
		if !g.wramFresh[enemiesKilledOwLoaded] || !g.wramFresh[enemiesKilledOwLoaded+enemiesKilledOwBytes-1] ||
			!g.wramFresh[0x0B00] || !g.wramFresh[0x0BDF] || !g.wramFresh[0x0DD0] || !g.wramFresh[0x0DDF] {
			return
		}
		if !ek.valid || ek.kind != enemiesKilledOW || ek.id != loc {
			*ek = enemiesKilledLocal{valid: true, kind: enemiesKilledOW, id: loc, inflight: ek.inflight}
		}

		// the positions of the live sprites and overlords:
		var cand [enemiesKilledOwBytes]uint8
		copy(cand[:], g.wram[enemiesKilledOwLoaded:enemiesKilledOwLoaded+enemiesKilledOwBytes])
		alive := func(blk uint16) {
			if i := int(blk >> 3); i < len(cand) {
				cand[i] &^= 0x80 >> (blk & 7)
			}
		}
		for k := uint32(0); k < 0x10; k++ {
			if g.wram[0x0DD0+k] != 0 {
				alive(g.wramU16(0x0BC0 + k*2))
			}
		}
		for k := uint32(0); k < 8; k++ {
			if g.wram[0x0B00+k] != 0 {
				alive(g.wramU16(0x0B48 + k*2))
			}
		}
		for i := range cand {
			killed := cand[i] & ek.cand[i]
			if killed&^ek.killed[i] != 0 {
				log.Printf("alttp: enemies killed: local: area $%02x: blks $%03x mask $%02x\n", owLocationArea(loc), i<<3, killed&^ek.killed[i])
			}
			ek.killed[i] = killed
		}
		ek.cand = cand

	default:
		ek.valid = false
		for i := range ek.recent {
			ek.recent[i].valid = false
		}
	}
}

// recentReadSlot is the one of the other recent rooms (1..3) whose mask is read this frame.
func (g *Game) recentReadSlot() int {
	return 1 + int(g.monotonicFrameTime)%(enemiesKilledRecentRooms-1)
}

// readEnemiesKilledRecent samples the masks of the last rooms visited: the game's list of them, and the mask of the
// rooms whose masks were read this frame (the first is the current room).
func (g *Game) readEnemiesKilledRecent() {
	ek := &g.enemiesKilled
	for i := range ek.recent {
		o := uint32(enemiesKilledRecentAddr + 2*i)
		rc := &ek.recent[i]
		if !g.wramFresh[o] || !g.wramFresh[o+1] {
			continue
		}
		room := g.wramU16(o)
		if room >= 0x128 {
			rc.valid = false
			continue
		}
		if rc.room != room {
			rc.room, rc.valid = room, false
		}
		m := enemiesKilledRoomMasks + uint32(room)*2
		if g.wramFresh[m] && g.wramFresh[m+1] {
			rc.mask, rc.valid = g.wramU16(m), true
		}
	}
}

// keyDropKilled returns the room sprite indexes of the enemies that carried a key and were killed, though the game
// has not marked them dead: MeetYourDemise turns a key carrier into the key drop (sprite $E4 or $E5) in its slot, with
// $0BC0,X = $FF so that no death flag is set, and moves its index to $0E30,X. The flag is never set; the key drop
// just goes away once the room's key is obtained.
func (g *Game) keyDropKilled() (mask uint16) {
	for x := uint32(0); x < 0x10; x++ {
		if !g.wramFresh[0x0DD0+x] || !g.wramFresh[0x0E20+x] || !g.wramFresh[0x0BC0+x] || !g.wramFresh[0x0E30+x] {
			continue
		}
		if t := g.wram[0x0E20+x]; (t == 0xE4 || t == 0xE5) && g.wram[0x0DD0+x] == 0x09 && g.wram[0x0BC0+x] == 0xFF {
			if k := g.wram[0x0E30+x]; k < 16 {
				mask |= 1 << k
			}
		}
	}
	return
}

// owKilledBlks lists the killed positions of a bitmap.
func owKilledBlks(bitmap *[enemiesKilledOwBytes]uint8) (blks []uint16) {
	for i, v := range bitmap {
		for j := 0; j < 8 && v != 0; j++ {
			if v&(0x80>>j) != 0 {
				blks = append(blks, uint16(i<<3|j))
			}
		}
	}
	return
}

// enemiesKilledUpdate is a generated update routine for the killed enemies awaiting execution confirmation.
type enemiesKilledUpdate struct {
	g         *Game
	kind      uint8
	id        uint32
	from      *Player
	cancelled bool
}

func (u *enemiesKilledUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	ek := &u.g.enemiesKilled
	if u.cancelled || ek.inflight != u {
		return
	}
	ek.inflight = nil
	if value == crystalSwitchWritten {
		log.Printf("alttp: enemies killed: update: from player '%s'\n", u.from.Name())
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed; the bits are set again if
// still missing.
func (u *enemiesKilledUpdate) AbortAsm() {
	ek := &u.g.enemiesKilled
	if u.cancelled || ek.inflight != u {
		return
	}
	ek.inflight = nil
}

// killableSprite is true for a live enemy: state ($0DD0,X) initializing ($08), active ($09) or stunned ($0B), and a
// type ($0E20,X) that is not an item. A killed enemy's slot becomes its drop (hearts, rupees, refills, fairy, keys
// $D8-$E5, and prizes and pieces $EA-$EC) with the same room index, which must be left alone. Sprites already
// dying, falling or carried are left alone too.
func killableSprite(st, typ uint8) bool {
	if typ >= 0xD8 && typ <= 0xEC {
		return false
	}
	return st == 0x08 || st == 0x09 || st == 0x0B
}

// enemyKill is a live local sprite to kill: its slot, and the state, type and room key it was read with, which the ROM
// routine checks again before killing it (the game ran since). The key is the sprite's room index in the underworld
// ($0BC0,X, a byte), the position in the overworld ($0BC0,X*2, a word).
type enemyKill struct {
	slot, state, typ uint8
	key              uint16
}

// killDeadEnemies* describe the table that killDeadEnemies (emitKillDeadEnemies) processes:
//
//	db mode            ; killDeadEnemiesUW or killDeadEnemiesOW: how the key is compared
//	db count           ; how many entries follow
//	entries: db slot, db state, db type, dw key
const (
	killDeadEnemiesUW   = 0
	killDeadEnemiesOW   = 1
	killDeadEnemyEntry  = 5
	killDeadEnemiesHdr  = 2
	killDeadEnemiesMax  = 16
	updKillEnemyCycles  = 120
	killDeadEnemyPoof   = 0x06 // the death state, which draws the poof
	killDeadEnemyTimer  = 0x1F // its timer, as PrepareSpriteToDie sets
	killDeadEnemyInit   = 0x08 // an enemy that is still initializing is freed instead
	killDeadEnemyActive = 0x09
	killDeadEnemyStun   = 0x0B
)

// enemyKillTable adds the table of kills to the update data; ok is false if it does not fit.
func (g *Game) enemyKillTable(mode uint8, kills []enemyKill) (table uint32, ok bool) {
	data := []byte{mode, uint8(len(kills))}
	for _, k := range kills {
		data = append(data, k.slot, k.state, k.typ, uint8(k.key), uint8(k.key>>8))
	}
	return g.updData.add(data, updTableBaseCycles+len(kills)*updKillEnemyCycles)
}

// emitKillDeadEnemies emits the routine in ROM which kills the enemies listed in a table (see killDeadEnemies*), like
// the player had: an enemy still initializing is freed, an active or stunned one gets the death state with the poof
// timer (and drops its item, if any). An entry is skipped unless the slot still has the state, type and key of the
// entry. It is called by generated enemies killed update routines in 16-bit mode (REP #$30) with JSL through the SRAM
// stub at killEnemiesStubAddr, with A = address of the table in updateDataBank. It calls no game routine, and so uses
// no scratch but the mode and count bytes. Returns with 16-bit registers.
func emitKillDeadEnemies(a *asm.Emitter) {
	bank := uint32(updateDataBank)
	mode, count := updScratchTmp, updScratchCount

	a.AssumeREP(0x30)
	a.Label("killDeadEnemies")
	a.TAX()
	a.SEP(0x20)
	a.LDA_long_x(bank + 0)
	a.STA_long(mode)
	a.LDA_long_x(bank + 1)
	a.STA_long(count)
	a.INX()
	a.INX()
	a.Label("kd_loop")
	a.SEP(0x20)
	a.LDA_long(count)
	a.BEQ("kd_done")
	a.DEC()
	a.STA_long(count)
	a.Comment("Y = slot:")
	a.REP(0x20)
	a.LDA_long_x(bank + 0)
	a.AND_imm16_w(0x00FF)
	a.TAY()
	a.SEP(0x20)
	a.Comment("still the same state and type:")
	a.LDA_long_x(bank + 1)
	a.CMP_abs_y(0x0DD0)
	a.BNE("kd_next")
	a.LDA_long_x(bank + 2)
	a.CMP_abs_y(0x0E20)
	a.BNE("kd_next")
	a.Comment("still the same room index (underworld) or position (overworld):")
	a.LDA_long(mode)
	a.BNE("kd_ow")
	a.LDA_long_x(bank + 3)
	a.CMP_abs_y(0x0BC0)
	a.BNE("kd_next")
	a.BRA("kd_kill")
	a.Label("kd_ow")
	a.REP(0x20)
	a.TYA()
	a.ASL()
	a.TAY()
	a.LDA_long_x(bank + 3)
	a.CMP_abs_y(0x0BC0)
	a.SEP(0x20)
	a.BNE("kd_next")
	a.Comment("Y = slot again:")
	a.REP(0x20)
	a.LDA_long_x(bank + 0)
	a.AND_imm16_w(0x00FF)
	a.TAY()
	a.SEP(0x20)
	a.Label("kd_kill")
	a.LDA_long_x(bank + 1)
	a.CMP_imm8_b(killDeadEnemyInit)
	a.BNE("kd_poof")
	a.Comment("not initialized yet: free the slot:")
	a.LDA_imm8_b(0x00)
	a.STA_abs_y(0x0DD0)
	a.BRA("kd_next")
	a.Label("kd_poof")
	a.Comment("the death state with the poof timer, like PrepareSpriteToDie:")
	a.LDA_imm8_b(killDeadEnemyTimer)
	a.STA_abs_y(0x0DF0)
	a.LDA_imm8_b(killDeadEnemyPoof)
	a.STA_abs_y(0x0DD0)
	a.Label("kd_next")
	a.REP(0x30)
	for i := 0; i < killDeadEnemyEntry; i++ {
		a.INX()
	}
	a.BRA("kd_loop")
	a.Label("kd_done")
	a.REP(0x30)
	a.RTL()
}

// generateEnemiesKilledUpdate generates an update routine setting the killed enemy bits of the remote players in the
// same room or area that are not set here. It has no side effects; the caller must set inflight if it is emitted.
func (g *Game) generateEnemiesKilledUpdate(newEmitter func() *asm.Emitter, index uint32) (u *enemiesKilledUpdate, a *asm.Emitter) {
	ek := &g.enemiesKilled
	local := g.local
	if !ek.valid || ek.inflight != nil || local.SubModule != 0 {
		return
	}

	switch {
	case ek.kind == enemiesKilledUW && local.Module == 0x07:
		room := uint16(ek.id)
		o := enemiesKilledRoomMasks + uint32(room)*2
		if !g.wramFresh[o] || !g.wramFresh[o+1] {
			return
		}
		have := g.wramU16(o)
		var want, dead uint16
		var from *Player
		now := time.Now()
		for _, p := range g.RemotePlayers() {
			// the remote player need not be in the room: it also sends the masks of the last rooms it visited
			rm, ok := p.EnemiesKilled.uwMask(room, now)
			if p.Team != local.Team || !ok {
				continue
			}
			if rm&^have&^want != 0 && from == nil {
				from = p
			}
			want |= rm &^ have
			dead |= rm
		}
		// the live sprites of the room that the remote players killed:
		var kills []enemyKill
		if g.KillDeadEnemies && dead != 0 {
			for x := uint32(0); x < 0x10; x++ {
				if !g.wramFresh[0x0BC0+x] || !g.wramFresh[0x0DD0+x] || !g.wramFresh[0x0E20+x] || !killableSprite(g.wram[0x0DD0+x], g.wram[0x0E20+x]) {
					continue
				}
				if k := g.wram[0x0BC0+x]; k < 16 && dead&(1<<k) != 0 {
					kills = append(kills, enemyKill{uint8(x), g.wram[0x0DD0+x], g.wram[0x0E20+x], uint16(k)})
					if from == nil {
						for _, p := range g.RemotePlayers() {
							if rm, ok := p.EnemiesKilled.uwMask(room, now); p.Team == local.Team && ok && rm&(1<<k) != 0 {
								from = p
								break
							}
						}
					}
				}
			}
		}
		if want == 0 && len(kills) == 0 {
			return
		}
		var killTable uint32
		if len(kills) != 0 {
			var ok bool
			if killTable, ok = g.enemyKillTable(killDeadEnemiesUW, kills); !ok {
				return
			}
		}

		u = &enemiesKilledUpdate{g: g, kind: ek.kind, id: ek.id, from: from}
		a = newEmitter()
		a.Comment(fmt.Sprintf("enemies killed in room $%03x: $%04x, killing %d live from %s:", room, want, len(kills), from.Name()))
		a.LDA_dp(0x10)
		a.CMP_imm8_b(0x07)
		a.BNE_far("ek_skip")
		a.LDA_dp(0x11)
		a.BNE_far("ek_skip")
		a.Comment("still in the same room:")
		a.LDA_abs(0x048E)
		a.CMP_imm8_b(uint8(room))
		a.BNE_far("ek_skip")
		a.LDA_abs(0x048F)
		a.CMP_imm8_b(uint8(room >> 8))
		a.BNE_far("ek_skip")
		for b := uint32(0); b < 2; b++ {
			if m := uint8(want >> (b * 8)); m != 0 {
				a.LDA_long(0x7E0000 + o + b)
				a.ORA_imm8_b(m)
				a.STA_long(0x7E0000 + o + b)
			}
		}
		if len(kills) != 0 {
			a.Comment(fmt.Sprintf("kill %d live sprites, table at $%06x:", len(kills), killTable))
			a.REP(0x30)
			emitCallUpdateROM(a, killEnemiesStubAddr, killTable)
			a.SEP(0x30)
		}

	case ek.kind == enemiesKilledOW && local.Module == 0x09:
		if g.wram[0x1B] != 0 {
			return
		}
		loc, ok := g.owLocation()
		if !ok || loc != ek.id {
			return
		}
		area := owLocationArea(loc)
		have := g.wram[enemiesKilledOwLoaded : enemiesKilledOwLoaded+enemiesKilledOwBytes]
		var want [enemiesKilledOwBytes]uint8
		var from *Player
		var kills []enemyKill
		for _, p := range g.RemotePlayers() {
			r := &p.EnemiesKilled
			if p.Team != local.Team || !r.valid || r.kind != enemiesKilledOW || r.id != loc {
				continue
			}
			if !isOverworldModule(p.Module) || p.Location&0x010000 != 0 || p.OverworldArea != area {
				continue
			}
			for _, blk := range r.blks {
				i := int(blk >> 3)
				if i >= len(want) {
					continue
				}
				if g.KillDeadEnemies {
					// the live sprites at a position the remote player killed:
					for x := uint32(0); x < 0x10; x++ {
						if g.wramFresh[0x0DD0+x] && g.wramFresh[0x0E20+x] && g.wramFresh[0x0BC0+x*2] && g.wramFresh[0x0BC1+x*2] &&
							killableSprite(g.wram[0x0DD0+x], g.wram[0x0E20+x]) && g.wramU16(0x0BC0+x*2) == blk {
							dup := false
							for _, k := range kills {
								dup = dup || uint32(k.slot) == x
							}
							if !dup {
								kills = append(kills, enemyKill{uint8(x), g.wram[0x0DD0+x], g.wram[0x0E20+x], blk})
								if from == nil {
									from = p
								}
							}
						}
					}
				}
				bit := uint8(0x80) >> (blk & 7)
				if have[i]&bit != 0 {
					continue
				}
				if want[i]&bit == 0 && from == nil {
					from = p
				}
				want[i] |= bit
			}
		}
		if from == nil {
			return
		}
		var killTable uint32
		if len(kills) != 0 {
			var ok bool
			if killTable, ok = g.enemyKillTable(killDeadEnemiesOW, kills); !ok {
				return
			}
		}

		u = &enemiesKilledUpdate{g: g, kind: ek.kind, id: ek.id, from: from}
		a = newEmitter()
		a.Comment(fmt.Sprintf("enemies killed in area $%02x, killing %d live from %s:", area, len(kills), from.Name()))
		a.LDA_dp(0x10)
		a.CMP_imm8_b(0x09)
		a.BNE_far("ek_skip")
		a.LDA_dp(0x11)
		a.ORA_dp(0x1B)
		a.BNE_far("ek_skip")
		a.Comment("still in the same area and world:")
		a.LDA_dp(0x8A)
		a.CMP_imm8_b(uint8(area))
		a.BNE_far("ek_skip")
		a.LDA_dp(0x8B)
		a.CMP_imm8_b(uint8(area >> 8))
		a.BNE_far("ek_skip")
		a.LDA_abs(0x0FFF)
		a.AND_imm8_b(0x01)
		a.CMP_imm8_b(uint8(loc>>17) & 1)
		a.BNE_far("ek_skip")
		n := 0
		for i, m := range want {
			if m == 0 {
				continue
			}
			if n++; n > enemiesKilledMaxBytes {
				break
			}
			a.LDA_long(0x7E0000 + enemiesKilledOwLoaded + uint32(i))
			a.ORA_imm8_b(m)
			a.STA_long(0x7E0000 + enemiesKilledOwLoaded + uint32(i))
		}
		if len(kills) != 0 {
			a.Comment(fmt.Sprintf("kill %d live sprites, table at $%06x:", len(kills), killTable))
			a.REP(0x30)
			emitCallUpdateROM(a, killEnemiesStubAddr, killTable)
			a.SEP(0x30)
		}

	default:
		return
	}

	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("ek_report")
	a.Label("ek_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("ek_report")
	a.STA_long(a.GetBase() + 0x02 + index)
	return
}

// sendEnemiesKilledRecent sends the masks of the other rooms the game remembers, one every enemiesKilledRefresh frames,
// each to its room's sector so that a player entering that room gets them. The room or area the player is in goes in
// the sector packet (see sendPackets).
func (g *Game) sendEnemiesKilledRecent() {
	ek := &g.enemiesKilled
	if !ek.valid || ek.kind != enemiesKilledUW || g.local.Module != 0x07 {
		return
	}
	if g.monotonicFrameTime%enemiesKilledRefresh != enemiesKilledRefresh/2 {
		return
	}
	st := &g.sendState
	st.enemiesKilledRecent = st.enemiesKilledRecent%(enemiesKilledRecentRooms-1) + 1
	if rc := ek.recent[st.enemiesKilledRecent]; rc.valid && rc.mask != 0 && uint32(rc.room) != ek.id {
		m := g.makeSectorMessageTo(0x010000 | uint32(rc.room))
		b := []byte{uint8(MsgEnemiesKilled), enemiesKilledUW}
		b = binary.LittleEndian.AppendUint16(b, rc.room)
		b = binary.LittleEndian.AppendUint16(b, rc.mask)
		m.addBytes(b)
		g.send(m)
	}
}

// SerializeEnemiesKilled writes message 0x1F with the killed enemies of the room or area, if sampled there.
func (g *Game) SerializeEnemiesKilled(w io.Writer) (err error) {
	ek := &g.enemiesKilled
	if !ek.valid {
		return
	}
	b := []byte{uint8(MsgEnemiesKilled), ek.kind}
	if ek.kind == enemiesKilledUW {
		b = binary.LittleEndian.AppendUint16(b, uint16(ek.id))
		b = binary.LittleEndian.AppendUint16(b, ek.mask|ek.extra)
	} else {
		b = append(b, uint8(ek.id), uint8(ek.id>>8), uint8(ek.id>>16))
		blks := owKilledBlks(&ek.killed)
		if len(blks) > enemiesKilledMaxBlks {
			blks = blks[:enemiesKilledMaxBlks]
		}
		b = append(b, uint8(len(blks)))
		for _, blk := range blks {
			b = binary.LittleEndian.AppendUint16(b, blk)
		}
	}
	_, err = w.Write(b)
	return
}

func (g *Game) DeserializeEnemiesKilled(p *Player, r io.Reader) (err error) {
	var kind uint8
	if err = binary.Read(r, binary.LittleEndian, &kind); err != nil {
		return fmt.Errorf("error deserializing enemies killed: %w", err)
	}
	switch kind {
	case enemiesKilledUW:
		var m struct {
			Room uint16
			Mask uint16
		}
		if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
			return fmt.Errorf("error deserializing enemies killed: %w", err)
		}
		uw := p.EnemiesKilled.uw
		p.EnemiesKilled = enemiesKilledRemote{valid: true, kind: kind, id: uint32(m.Room), mask: m.Mask, uw: uw}
		p.EnemiesKilled.setUW(m.Room, m.Mask, time.Now())
	case enemiesKilledOW:
		var loc uint32
		if loc, err = readU24(r); err != nil {
			return fmt.Errorf("error deserializing enemies killed: %w", err)
		}
		var count uint8
		if err = binary.Read(r, binary.LittleEndian, &count); err != nil {
			return fmt.Errorf("error deserializing enemies killed: %w", err)
		}
		blks := make([]uint16, count)
		if err = binary.Read(r, binary.LittleEndian, blks); err != nil {
			return fmt.Errorf("error deserializing enemies killed: %w", err)
		}
		p.EnemiesKilled = enemiesKilledRemote{valid: true, kind: kind, id: loc, blks: blks, uw: p.EnemiesKilled.uw}
	default:
		return fmt.Errorf("error deserializing enemies killed: unknown kind %d", kind)
	}
	return nil
}
