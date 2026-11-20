package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

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
// team in the same room or area: the bits they killed that are not set here are set.

const (
	enemiesKilledUW        = uint8(0)
	enemiesKilledOW        = uint8(1)
	enemiesKilledRoomMasks = uint32(0x1DF80) // $7FDF80
	enemiesKilledOwLoaded  = uint32(0x1EF80) // $7FEF80
	enemiesKilledOwBytes   = 0xC0
	// the most positions sent and the most bitmap bytes one update routine writes:
	enemiesKilledMaxBlks  = 64
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
}

// enemiesKilledLocal tracks the local player's killed enemies.
type enemiesKilledLocal struct {
	// valid is true when sampled in the room or area id:
	valid bool
	kind  uint8
	id    uint32
	mask  uint16
	// the overworld positions killed (seen twice in a row) and the candidates of the last read:
	killed [enemiesKilledOwBytes]uint8
	cand   [enemiesKilledOwBytes]uint8

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
		return g.readEnqueue(q, 0xF50000+enemiesKilledRoomMasks+room*2, 2, nil)
	case 0x09:
		q = g.readEnqueue(q, 0xF50000+enemiesKilledOwLoaded, enemiesKilledOwBytes, nil)
		// $0B00 overlord types .. $0B48 overlord positions .. $0BC0 sprite positions:
		q = g.readEnqueue(q, 0xF50B00, 0xE0, nil) // [$0B00..$0BDF]
		return g.readEnqueue(q, 0xF50DD0, 0x10, nil)
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
		if ek.valid && ek.kind == enemiesKilledUW && ek.id == uint32(room) && ek.mask == mask {
			return
		}
		if ek.valid && ek.kind == enemiesKilledUW && ek.id == uint32(room) && mask&^ek.mask != 0 {
			log.Printf("alttp: enemies killed: local: room $%03x: $%04x -> $%04x\n", room, ek.mask, mask)
		}
		ek.valid, ek.kind, ek.id, ek.mask = true, enemiesKilledUW, uint32(room), mask

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
	}
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
		var want uint16
		var from *Player
		for _, p := range g.RemotePlayers() {
			r := &p.EnemiesKilled
			if p.Team != local.Team || !r.valid || r.kind != enemiesKilledUW || r.id != ek.id {
				continue
			}
			if p.Module != 0x07 || p.Location&0x010000 == 0 || p.DungeonRoom != room {
				continue
			}
			if r.mask&^have&^want != 0 && from == nil {
				from = p
			}
			want |= r.mask &^ have
		}
		if want == 0 {
			return
		}

		u = &enemiesKilledUpdate{g: g, kind: ek.kind, id: ek.id, from: from}
		a = newEmitter()
		a.Comment(fmt.Sprintf("enemies killed in room $%03x: $%04x from %s:", room, want, from.Name()))
		a.LDA_dp(0x10)
		a.CMP_imm8_b(0x07)
		a.BNE("ek_skip")
		a.LDA_dp(0x11)
		a.BNE("ek_skip")
		a.Comment("still in the same room:")
		a.LDA_abs(0x048E)
		a.CMP_imm8_b(uint8(room))
		a.BNE("ek_skip")
		a.LDA_abs(0x048F)
		a.CMP_imm8_b(uint8(room >> 8))
		a.BNE("ek_skip")
		for b := uint32(0); b < 2; b++ {
			if m := uint8(want >> (b * 8)); m != 0 {
				a.LDA_long(0x7E0000 + o + b)
				a.ORA_imm8_b(m)
				a.STA_long(0x7E0000 + o + b)
			}
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

		u = &enemiesKilledUpdate{g: g, kind: ek.kind, id: ek.id, from: from}
		a = newEmitter()
		a.Comment(fmt.Sprintf("enemies killed in area $%02x from %s:", area, from.Name()))
		a.LDA_dp(0x10)
		a.CMP_imm8_b(0x09)
		a.BNE("ek_skip")
		a.LDA_dp(0x11)
		a.ORA_dp(0x1B)
		a.BNE("ek_skip")
		a.Comment("still in the same area and world:")
		a.LDA_dp(0x8A)
		a.CMP_imm8_b(uint8(area))
		a.BNE("ek_skip")
		a.LDA_dp(0x8B)
		a.CMP_imm8_b(uint8(area >> 8))
		a.BNE("ek_skip")
		a.LDA_abs(0x0FFF)
		a.AND_imm8_b(0x01)
		a.CMP_imm8_b(uint8(loc>>17) & 1)
		a.BNE("ek_skip")
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

// sendEnemiesKilled sends message 0x1F to the room's or the area's sector every 4 frames, once sampled there.
func (g *Game) sendEnemiesKilled() {
	ek := &g.enemiesKilled
	if !ek.valid || g.monotonicFrameTime&3 != 0 {
		return
	}
	m := g.makeSectorMessage()
	if err := g.SerializeEnemiesKilled(m); err != nil {
		panic(err)
	}
	g.send(m)
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
		b = binary.LittleEndian.AppendUint16(b, ek.mask)
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
		p.EnemiesKilled = enemiesKilledRemote{valid: true, kind: kind, id: uint32(m.Room), mask: m.Mask}
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
		p.EnemiesKilled = enemiesKilledRemote{valid: true, kind: kind, id: loc, blks: blks}
	default:
		return fmt.Errorf("error deserializing enemies killed: unknown kind %d", kind)
	}
	return nil
}
