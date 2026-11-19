package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/alttpo/snes/asm"
	"o2/snes"
)

// Underworld manipulable object synchronization
//
// The underworld has no change log like the overworld. Instead the room loader registers up to 16 manipulable objects
// (RoomDraw_SinglePot, RoomDraw_PushableBlock, ...) in slots of these arrays:
//
//   - $7E0500 [$10]uint16: type and state. $0000-$0005 and $FFFF are push blocks (stationary, triggered, moving, done
//     moving, falling, on a switch; $FFFF once it can no longer move), $1010-$1212 pots, $2020-$2323 the four quarters
//     of a big gray rock, $3030-$3333 the four quarters of a bombable floor and $4040 hammer pegs.
//   - $7E0520 [$10]uint16: the object's offset in the room data, which is the same for every client. For push blocks it
//     is the index into the dungeon's push block table at $7EF940 ([$63] of `room u16`, `pos u16`, loaded from ROM at
//     the dungeon entrance; a block that falls into a pit records the room it falls into and its position there).
//   - $7E0540 [$10]uint16: the tilemap position (offset into $7E2000, $2000 set for the upper layer).
//   - $7E0560/$0580/$05A0/$05C0 [$10]uint16: the tiles drawn when the object goes away (top left, bottom left, top
//     right, bottom right).
//
// Slots [0, $0478/2) are manipulables; the slots after them hold the room's torches, which alttpo syncs on its own.
// Each manipulable except bombable floors gets the tile attribute $70+slot in $7F2000 so the game can find the slot
// from the tile Link interacts with. Lifting a pot, pounding a peg or lifting a big rock calls RoomDraw_16x16Single,
// which draws the replacement tiles into $7E2000, their attributes into $7F2000 and a stripe for VRAM. $0500 does not
// change for those, so the only trace is the attribute. Pushing a block draws its replacement tiles, moves it 16
// pixels and then either draws it at its new position (attribute $27, state $FFFF), toggles a switch under it or
// lets it fall into a pit.
//
// The state is room specific and reset by every room load, so it is shared with players in the same room (alttpo's
// Manipulables.as) in message 0x18 in broadcastSector packets: `room u16`, `count u8`, then per slot `type u16`,
// `objx u16`, `pos u16`, `flags u8` (bit 0: revealed, bit 1: four replacement tiles `u16` follow). o2 polls the
// attributes of a few slots per frame to see which objects were revealed.
//
// Slots are matched by their room data offset (and position, except for push blocks) since their order may differ
// between players. A remote player's revealed object is revealed locally by calling RoomDraw_16x16Single with the
// remote's replacement tiles (a pot can hide a hole or a switch). If a pot was lifted while the local player was in the
// room (the sender's previous message had it still there), the item hidden under it is revealed first like lifting it
// does (see syncsecrets.go), so every player in the room gets it. A remote
// player's push block that finished moving is moved locally in one step: draw what was under it, then do what
// PushBlock_CheckForPit does at the new position.

const (
	manipCount         = uint16(0x0478)
	manipType          = uint16(0x0500)
	manipObjX          = uint16(0x0520)
	manipPos           = uint16(0x0540)
	manipTiles         = uint16(0x0560)
	manipSlots         = 16
	manipProbesPerRead = 2
	manipAsmSkipped    = 0xFF
	manipAttrBase      = uint8(0x70)
	pushBlockTable     = uint32(0x7EF940)

	manipFlagRevealed = 0x01
	manipFlagTiles    = 0x02
)

type manipSlot struct {
	Type  uint16
	ObjX  uint16
	Pos   uint16
	Tiles [4]uint16
	// OrigPos is the position at the room load:
	OrigPos uint16
	// present is set once the slot's attribute $70+slot was seen at Pos:
	present bool
	// revealed is set once the object went away after it was seen present:
	revealed bool
}

// manipLocal tracks the local player's manipulable objects in the current room.
type manipLocal struct {
	// valid is true when the slots were read in the dungeon module for room:
	valid bool
	room  uint16
	n     int
	slots [manipSlots]manipSlot

	probeNext int
	// skipReason is the last reason logged for not moving a push block like a remote player:
	skipReason [manipSlots]string

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *manipUpdate
}

// manipPushBlockResting reports whether a push block state is one a block ends up in after being pushed: $0000 (also
// never pushed), $0005 (on a floor switch) or $FFFF (older versions of the sync).
func manipPushBlockResting(t uint16) bool { return t == 0x0000 || t == 0x0005 || t == 0xFFFF }

// objectAt returns the slot of a manipulable object still at pos that a push block cannot move onto, or -1.
func (m *manipLocal) objectAt(pos uint16) int {
	for k := 0; k < m.n; k++ {
		s := &m.slots[k]
		if s.Pos == pos && manipIsLiftable(s.Type) && !s.revealed {
			return k
		}
	}
	return -1
}

// skip logs why a remote player's push block move is not applied here, once per change of reason.
func (m *manipLocal) skip(slot int, p *Player, reason string) {
	if m.skipReason[slot] == reason {
		return
	}
	m.skipReason[slot] = reason
	if reason != "" {
		log.Printf("alttp: manip: room $%03x slot %d: not moving block like player '%s': %s\n", m.room, slot, p.Name(), reason)
	}
}

type manipRemoteSlot struct {
	Type  uint16
	ObjX  uint16
	Pos   uint16
	Flags uint8
	Tiles [4]uint16
	// live is set when the object was seen revealed after it was seen still there while the local player was in the
	// room, so the secret under it is revealed here too:
	live bool
}

// manipRemote is a remote player's manipulable objects as received.
type manipRemote struct {
	room  uint16
	n     int
	slots [manipSlots]manipRemoteSlot
}

func (r *manipRemote) reset() {
	r.room = 0
	r.n = 0
}

func manipIsPushBlock(t uint16) bool { return t < 0x0010 || t == 0xFFFF }

// manipIsLiftable is true for pots, big gray rocks and hammer pegs, which are revealed by RoomDraw_16x16Single.
func manipIsLiftable(t uint16) bool {
	k := t & 0x00F0
	return t != 0xFFFF && (k == 0x10 || k == 0x20 || k == 0x40)
}

// manipAttrIndex returns the index into the tile attributes at $7F2000 for a tilemap position.
func manipAttrIndex(pos uint16) uint32 { return uint32(pos&0x3FFF) >> 1 }

func (g *Game) resetManip() {
	m := &g.manip
	if m.inflight != nil {
		m.inflight.cancelled = true
	}
	*m = manipLocal{}
}

// enqueueManipReads reads the manipulable slots and the attributes of a few slots not yet seen revealed.
func (g *Game) enqueueManipReads(q []snes.Read) []snes.Read {
	if !g.SyncUnderworld || g.local.Module != 0x07 {
		return q
	}
	// $0500..$05DF:
	q = g.readEnqueue(q, 0xF50000+uint32(manipType), 0xE0, nil)

	m := &g.manip
	if !m.valid || m.n == 0 {
		return q
	}
	probes := 0
	start := m.probeNext
	for k := 0; k < m.n && probes < manipProbesPerRead; k++ {
		i := (start + k) % m.n
		s := &m.slots[i]
		if !manipIsLiftable(s.Type) || s.revealed {
			continue
		}
		q = g.readEnqueue(q, 0xF62000+manipAttrIndex(s.Pos), 1, nil)
		probes++
		m.probeNext = (i + 1) % m.n
	}
	return q
}

func (g *Game) readManipSlot(i int) (s manipSlot) {
	o := uint32(i << 1)
	s.Type = g.wramU16(uint32(manipType) + o)
	s.ObjX = g.wramU16(uint32(manipObjX) + o)
	s.Pos = g.wramU16(uint32(manipPos) + o)
	for t := uint32(0); t < 4; t++ {
		s.Tiles[t] = g.wramU16(uint32(manipTiles) + t<<5 + o)
	}
	s.OrigPos = s.Pos
	return
}

// readManip samples the local manipulable objects. Called when WRAM is read from the SNES.
func (g *Game) readManip() {
	local := g.local
	m := &g.manip

	if local.Module == 0x0E {
		// menus don't touch the room:
		return
	}
	if local.Module != 0x07 {
		if m.valid {
			g.resetManip()
		}
		return
	}
	if !g.wramFresh[0xA0] || !g.wramFresh[0xA1] || !g.wramFresh[manipCount] || !g.wramFresh[manipCount+1] {
		return
	}
	for o := manipType; o < manipType+0xE0; o++ {
		if !g.wramFresh[o] {
			return
		}
	}
	room := g.wramU16(0xA0)
	if m.valid && m.room != room {
		g.resetManip()
	}
	// room loads happen in other submodules:
	if local.SubModule != 0 {
		return
	}

	n := int(g.wramU16(uint32(manipCount)))
	if n&1 != 0 || n > manipSlots<<1 {
		return
	}
	n >>= 1

	baseline := !m.valid || m.n != n
	if !baseline {
		for i := 0; i < n; i++ {
			if g.wramU16(uint32(manipObjX)+uint32(i<<1)) != m.slots[i].ObjX {
				baseline = true
				break
			}
		}
	}
	if baseline {
		g.resetManip()
		m.valid = true
		m.room = room
		m.n = n
		for i := 0; i < n; i++ {
			m.slots[i] = g.readManipSlot(i)
			// revealed while the room loaded (see manipstage.go): it counts as revealed once its attribute shows it:
			if s := &m.slots[i]; manipIsLiftable(s.Type) && g.manipStagedRevealed(room, s.ObjX, s.Pos) {
				s.present = true
			}
		}
		return
	}

	for i := 0; i < n; i++ {
		s := &m.slots[i]
		r := g.readManipSlot(i)
		s.Type, s.Pos, s.Tiles = r.Type, r.Pos, r.Tiles

		if !manipIsLiftable(s.Type) || s.revealed {
			continue
		}
		a := 0x12000 + manipAttrIndex(s.Pos)
		if !g.wramFresh[a] {
			continue
		}
		if g.wram[a] == manipAttrBase+uint8(i) {
			s.present = true
		} else if s.present {
			s.revealed = true
			log.Printf("alttp: manip: local: room $%03x slot %d type $%04x at $%04x revealed\n", room, i, s.Type, s.Pos)
		}
	}
}

// SerializeManip writes message 0x18 with the local manipulable objects.
func (g *Game) SerializeManip(w io.Writer) (err error) {
	m := &g.manip
	b := make([]byte, 0, 4+manipSlots*15)
	b = append(b, uint8(MsgManip), uint8(m.room), uint8(m.room>>8), uint8(m.n))
	for i := 0; i < m.n; i++ {
		s := &m.slots[i]
		b = binary.LittleEndian.AppendUint16(b, s.Type)
		b = binary.LittleEndian.AppendUint16(b, s.ObjX)
		b = binary.LittleEndian.AppendUint16(b, s.Pos)
		if s.revealed {
			b = append(b, manipFlagRevealed|manipFlagTiles)
			for _, t := range s.Tiles {
				b = binary.LittleEndian.AppendUint16(b, t)
			}
		} else {
			b = append(b, 0)
		}
	}
	_, err = w.Write(b)
	return
}

func (g *Game) DeserializeManip(p *Player, r io.Reader) (err error) {
	var hdr struct {
		Room uint16
		N    uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("error deserializing manipulables: %w", err)
	}
	if hdr.N > manipSlots {
		return fmt.Errorf("error deserializing manipulables: %d slots", hdr.N)
	}
	rm := manipRemote{room: hdr.Room, n: int(hdr.N)}
	for i := 0; i < rm.n; i++ {
		s := &rm.slots[i]
		var fixed struct {
			Type  uint16
			ObjX  uint16
			Pos   uint16
			Flags uint8
		}
		if err = binary.Read(r, binary.LittleEndian, &fixed); err != nil {
			return fmt.Errorf("error deserializing manipulables: %w", err)
		}
		s.Type, s.ObjX, s.Pos, s.Flags = fixed.Type, fixed.ObjX, fixed.Pos, fixed.Flags
		if s.Flags&manipFlagTiles != 0 {
			if err = binary.Read(r, binary.LittleEndian, &s.Tiles); err != nil {
				return fmt.Errorf("error deserializing manipulables: %w", err)
			}
		}
	}
	// objects revealed since the sender's last message, while we are in the room too, were just lifted:
	m := &g.manip
	here := g.local.Module == 0x07 && m.valid && m.room == rm.room
	if prev := &p.Manip; here && prev.room == rm.room {
		for i := 0; i < rm.n; i++ {
			s := &rm.slots[i]
			if s.Flags&manipFlagRevealed == 0 {
				continue
			}
			for k := 0; k < prev.n; k++ {
				ps := &prev.slots[k]
				if ps.ObjX != s.ObjX || ps.Pos != s.Pos || ps.Type != s.Type {
					continue
				}
				s.live = ps.live || ps.Flags&manipFlagRevealed == 0
				break
			}
		}
	}
	p.Manip = rm
	return nil
}

// manipFindLocal returns the local slot matching a remote slot, or -1.
func (m *manipLocal) findLocal(rs *manipRemoteSlot) int {
	push := manipIsPushBlock(rs.Type)
	for j := 0; j < m.n; j++ {
		s := &m.slots[j]
		if s.ObjX != rs.ObjX || manipIsPushBlock(s.Type) != push {
			continue
		}
		if !push && (s.Pos != rs.Pos || s.Type != rs.Type) {
			continue
		}
		return j
	}
	return -1
}

// manipIsStep is true if the positions are one push (16 pixels) apart.
func manipIsStep(from, to uint16) bool {
	switch to - from {
	case 0xFF00, 0x0100, 0xFFFC, 0x0004:
		return true
	}
	return false
}

type manipItem struct {
	slot  int
	move  bool
	to    uint16
	tiles [4]uint16
	// reveal is set to reveal the item under a pot the remote player just lifted:
	reveal bool
	player string
}

// manipUpdate is a generated update routine applying remote manipulable object changes, awaiting confirmation.
type manipUpdate struct {
	g         *Game
	room      uint16
	items     []manipItem
	cancelled bool
}

func (u *manipUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	m := &g.manip
	if u.cancelled || m.inflight != u {
		return
	}
	m.inflight = nil

	if value == manipAsmSkipped {
		log.Printf("alttp: manip: update skipped\n")
		return
	}
	if !m.valid || m.room != u.room {
		return
	}
	n := int(value)
	if n > len(u.items) {
		n = len(u.items)
	}
	for _, it := range u.items[:n] {
		s := &m.slots[it.slot]
		if it.move {
			// the next read tells the actual state:
			s.Pos = it.to
			s.Type = 0x0000
			log.Printf("alttp: manip: update: room $%03x slot %d moved to $%04x from player '%s'\n", u.room, it.slot, it.to, it.player)
		} else {
			s.revealed = true
			log.Printf("alttp: manip: update: room $%03x slot %d revealed from player '%s' (item: %v)\n", u.room, it.slot, it.player, it.reveal)
		}
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed; re-read the room as a baseline.
func (u *manipUpdate) AbortAsm() {
	m := &u.g.manip
	if u.cancelled || m.inflight != u {
		return
	}
	m.inflight = nil
}

// generateManipUpdate generates an update routine to reveal the objects and move the push blocks that the remote
// players in the same room revealed or moved. The caller must set inflight if the routine is emitted.
func (g *Game) generateManipUpdate(newEmitter func() *asm.Emitter, index uint32) (u *manipUpdate, a *asm.Emitter) {
	m := &g.manip
	local := g.local
	if !m.valid || m.inflight != nil {
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 {
		return
	}
	_, ok1 := g.romFunctions[fnRoomDraw16x16Single]
	_, ok2 := g.romFunctions[fnBank01RTL]
	if !ok1 || !ok2 {
		return
	}
	_, ok3 := g.romFunctions[fnRevealPotItem]
	_, ok4 := g.romFunctions[fnSpawnThrowableTerrainSilently]
	canReveal := ok3 && ok4

	items := make([]manipItem, 0, 4)
	queued := [manipSlots]bool{}
	for _, p := range g.RemotePlayers() {
		// compare rooms like the star switch sync; o2 and alttpo set the dark world bit differently in dungeons:
		if p.Team != local.Team || p.Module != 0x07 || p.Location&0x010000 == 0 || p.DungeonRoom != m.room {
			continue
		}
		rm := &p.Manip
		if rm.room != m.room {
			continue
		}
		for i := 0; i < rm.n; i++ {
			rs := &rm.slots[i]
			j := m.findLocal(rs)
			if j < 0 || queued[j] {
				continue
			}
			s := &m.slots[j]
			var it manipItem
			if manipIsPushBlock(rs.Type) {
				if rs.Pos == s.Pos {
					m.skip(j, p, "")
					continue
				}
				if !manipPushBlockResting(rs.Type) {
					m.skip(j, p, fmt.Sprintf("remote block still moving (state $%04x)", rs.Type))
					continue
				}
				if s.Type != 0 || s.Pos != s.OrigPos {
					m.skip(j, p, fmt.Sprintf("local block state $%04x at $%04x (from $%04x), remote at $%04x", s.Type, s.Pos, s.OrigPos, rs.Pos))
					continue
				}
				if !manipIsStep(s.Pos, rs.Pos) {
					m.skip(j, p, fmt.Sprintf("remote block at $%04x is not one step from $%04x", rs.Pos, s.Pos))
					continue
				}
				if k := m.objectAt(rs.Pos); k >= 0 {
					// e.g. a hammer peg the remote player pounded that is still up here; reveal it first:
					m.skip(j, p, fmt.Sprintf("slot %d still at $%04x", k, rs.Pos))
					continue
				}
				m.skip(j, p, "")
				it = manipItem{slot: j, move: true, to: rs.Pos, tiles: s.Tiles}
			} else {
				if rs.Flags&manipFlagRevealed == 0 || !manipIsLiftable(s.Type) || !s.present || s.revealed {
					continue
				}
				it = manipItem{slot: j, tiles: s.Tiles}
				if rs.Flags&manipFlagTiles != 0 {
					it.tiles = rs.Tiles
				}
				// pots hide items:
				if rs.live && canReveal && s.Type&0x00F0 == 0x0010 {
					it.reveal = true
				}
			}
			if len(items) >= manipMaxPerUpdate {
				continue
			}
			it.player = p.Name()
			queued[j] = true
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return
	}

	// the table for manipUpdate (see emitManipUpdate):
	data := make([]byte, 0, 2+len(items)*manipUpdEntry+2)
	data = binary.LittleEndian.AppendUint16(data, m.room)
	for _, it := range items {
		s := &m.slots[it.slot]
		var e [manipUpdEntry / 2]uint16
		e[1] = uint16(it.slot << 1)
		e[2] = s.Pos
		if it.move {
			e[0] = manipUpdMove
			if it.to&0x4000 == 0 && roomHasTag(g.roomTags(), roomTagIsBlockDoor) {
				e[0] = manipUpdMoveDoor
			}
			e[3] = it.to
			e[4] = s.ObjX
		} else {
			e[0] = manipUpdReveal
			if it.reveal {
				e[0] = manipUpdRevealItem
			}
			e[3], e[4] = potCoords(m.room, s.Pos)
			for t := 0; t < 4; t++ {
				if it.tiles[t] != s.Tiles[t] {
					e[0] |= 0x0100 << t
				}
				e[5+t] = it.tiles[t]
			}
		}
		for _, w := range e {
			data = binary.LittleEndian.AppendUint16(data, w)
		}
	}
	data = binary.LittleEndian.AppendUint16(data, updateTableEnd)
	table, ok := g.updData.add(data)
	if !ok {
		return nil, nil
	}

	u = &manipUpdate{g: g, room: m.room, items: items}

	a = newEmitter()
	a.Comment(fmt.Sprintf("manipulables in room $%03x, table at $%06x:", m.room, table))
	emitCallUpdateROM(a, manipUpdateStubAddr, table)
	emitReport(a, index)

	return
}

// the entries of the table for manipUpdate, 9 words each:
//
//	dw kind     ; manipUpd*; for reveals, bits 8-11 are set for the replacement tiles 0-3 to write
//	dw slot*2
//	dw pos      ; the object's position; for moves, the block's position before the move
//	dw x / to   ; reveals: the pot's coordinates for the item; moves: the block's position after the move
//	dw y / objx ; moves: the block's room data offset, for the pit table
//	dw tiles[4] ; reveals: the replacement tiles
const (
	manipUpdEntry = 18
	// manipMaxPerUpdate is the most entries in a table:
	manipMaxPerUpdate = 8

	manipUpdReveal     = 0
	manipUpdRevealItem = 1
	manipUpdMove       = 2
	// a move on the upper layer in a room with a push block door tag:
	manipUpdMoveDoor = 3
)

// emitManipUpdate emits the routine in ROM which applies a table of remote manipulable object changes. It is called by
// generated manip update routines in 16-bit mode (REP #$30) with JSL through the SRAM stub at manipUpdateStubAddr,
// with A = address of the table in updateDataBank:
//
//	dw room              ; $A0 the changes are for
//	entries ...          ; see manipUpdEntry
//	dw updateTableEnd
//
// Revealed objects get their replacement tiles and are drawn with RoomDraw_16x16Single if still there (marked
// $70+slot); for manipUpdRevealItem, the item under the pot is revealed first like lifting it does. Push blocks still
// where they were are moved like PushBlock_CheckForPit does. It returns A = the number of entries handled (it stops
// early when the VRAM stripe buffer is getting full), or manipAsmSkipped if the player is no longer in the room in
// the dungeon module. fnDraw is RoomDraw_16x16Single and fnRTL an RTL in its bank; fnRevealPot is RevealPotItem and
// fnSpawn Sprite_SpawnThrowableTerrain_silently, without which items are not revealed.
func emitManipUpdate(a *asm.Emitter, fnDraw, fnRTL, fnRevealPot, fnSpawn uint32) {
	bank := uint32(updateDataBank)
	ptr, count, tmp := updScratchPtr, updScratchCount, updScratchTmp
	canReveal := fnRevealPot != 0 && fnSpawn != 0 && fnRTL != 0
	draw := func() {
		if fnDraw != 0 {
			a.JSRBanked(fnDraw, fnRTL)
		}
		a.REP(0x30)
	}
	// loadPtr: X = the entry:
	loadPtr := func() {
		a.LDA_long(ptr)
		a.TAX()
	}
	// attrIndex: X = the attribute index of the position in A:
	attrIndex := func() {
		a.AND_imm16_w(0x3FFF)
		a.LSR()
		a.TAX()
	}

	a.AssumeREP(0x30)
	a.Label("manipUpdate")
	var spawnSub uint16
	if canReveal {
		a.Comment("the subroutine that spawns the items precedes the code:")
		a.JMP_abs("mu_start")
		spawnSub = emitSpawnSecretSub(a, fnSpawn)
		a.Label("mu_start")
	}
	a.Comment("in the dungeon module, in the table's room:")
	a.TAX()
	a.LDA_dp(0x10)
	a.CMP_imm16_w(0x0007)
	a.BNE_far("mu_skip")
	a.LDA_dp(0xA0)
	a.CMP_long_x(bank)
	a.BNE_far("mu_skip")
	a.INX()
	a.INX()
	a.TXA()
	a.STA_long(ptr)
	a.LDA_imm16_w(0x0000)
	a.STA_long(count)

	a.Label("mu_loop")
	a.Comment("stop when the VRAM stripe buffer is getting full or at the end of the table:")
	a.LDA_abs(0x1000)
	a.CMP_imm16_w(0x0068)
	a.BCC("mu_room")
	a.JMP_abs("mu_done")
	a.Label("mu_room")
	loadPtr()
	a.LDA_long_x(bank + 0)
	a.CMP_imm16_w(updateTableEnd)
	a.BNE("mu_entry")
	a.JMP_abs("mu_done")
	a.Label("mu_entry")
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(manipUpdMove)
	a.BCC("mu_reveal")
	a.JMP_abs("mu_move")

	a.Label("mu_reveal")
	a.Comment("reveal the object if still there:")
	a.LDA_long_x(bank + 4)
	attrIndex()
	a.LDA_long_x(0x7F2000)
	a.AND_imm16_w(0x00FF)
	a.STA_long(tmp)
	loadPtr()
	a.LDA_long_x(bank + 2)
	a.LSR()
	a.CLC()
	a.ADC_imm16_w(uint16(manipAttrBase))
	a.CMP_long(tmp)
	a.BEQ("mu_reveal_ok")
	a.JMP_abs("mu_next")
	a.Label("mu_reveal_ok")
	if canReveal {
		a.LDA_long_x(bank + 0)
		a.AND_imm16_w(0x00FF)
		a.CMP_imm16_w(manipUpdRevealItem)
		a.BNE("mu_tiles")
		a.Comment("reveal the item under it like lifting it does:")
		a.LDA_abs(0x0B9C)
		a.PHA()
		a.LDA_long_x(bank + 4)
		a.AND_imm16_w(0x3FFF)
		a.LSR()
		a.STA_dp(0x06)
		a.LDA_long_x(bank + 4)
		a.JSRBanked(fnRevealPot, fnRTL)
		a.REP(0x30)
		loadPtr()
		a.LDA_long_x(bank + 6)
		a.STA_dp(0x00)
		a.LDA_long_x(bank + 8)
		a.STA_dp(0x02)
		a.Comment("a pot:")
		a.LDY_imm16_w(0x0000)
		a.JSR_abs(spawnSub)
		a.PLA()
		a.STA_abs(0x0B9C)
		loadPtr()
	}
	a.Label("mu_tiles")
	a.Comment("the replacement tiles that differ, then draw it:")
	a.LDA_long_x(bank + 2)
	a.TAY()
	for t := uint16(0); t < 4; t++ {
		skip := fmt.Sprintf("mu_tile_%d", t)
		a.LDA_long_x(bank + 0)
		a.AND_imm16_w(0x0100 << t)
		a.BEQ(skip)
		a.LDA_long_x(bank + 10 + 2*uint32(t))
		a.STA_abs_y(manipTiles + t<<5)
		a.Label(skip)
	}
	draw()
	a.JMP_abs("mu_next")

	a.Label("mu_move")
	a.Comment("move the push block if it did not move:")
	a.LDA_long_x(bank + 2)
	a.TAY()
	a.LDA_abs_y(manipType)
	a.BEQ("mu_move_1")
	a.JMP_abs("mu_next")
	a.Label("mu_move_1")
	a.LDA_abs_y(manipPos)
	a.CMP_long_x(bank + 4)
	a.BEQ("mu_move_2")
	a.JMP_abs("mu_next")
	a.Label("mu_move_2")
	a.Comment("nothing else there (e.g. a hammer peg still up):")
	a.LDA_long_x(bank + 6)
	attrIndex()
	a.LDA_long_x(0x7F2000)
	a.AND_imm16_w(0x00F0)
	a.CMP_imm16_w(uint16(manipAttrBase))
	a.BNE("mu_move_3")
	a.JMP_abs("mu_next")
	a.Label("mu_move_3")
	a.Comment("draw what was under it:")
	draw()
	loadPtr()
	a.LDA_long_x(bank + 2)
	a.TAY()
	a.LDA_long_x(bank + 6)
	a.STA_abs_y(manipPos)
	a.Comment("do what PushBlock_CheckForPit does:")
	a.AND_imm16_w(0x4000)
	a.BNE("mu_attr")
	a.Comment("upper layer block pushed; push block door ($0B-$13) and chest ($3C) tags act on $0641:")
	a.LDA_abs(0x0641)
	a.EOR_imm16_w(0x0001)
	a.STA_abs(0x0641)
	a.LDA_long_x(bank + 0)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(manipUpdMoveDoor)
	a.BNE("mu_attr")
	emitOperateBlockDoor(a, "mu_block_door")
	a.Label("mu_attr")
	loadPtr()
	a.LDA_long_x(bank + 6)
	attrIndex()
	a.LDA_long_x(0x7F2000)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x0020)
	a.BNE("mu_not_pit")
	a.Comment("fell into a pit; record where it lands in the room below:")
	loadPtr()
	a.LDA_long_x(bank + 6)
	a.STA_long(tmp)
	a.LDA_long_x(bank + 8)
	a.TAX()
	a.LDA_long(0x7EC000)
	a.AND_imm16_w(0x00FF)
	a.STA_long_x(pushBlockTable)
	a.LDA_long(tmp)
	a.STA_long_x(pushBlockTable + 2)
	a.JMP_abs("mu_next")
	a.Label("mu_not_pit")
	a.CMP_imm16_w(0x0023)
	a.BNE("mu_not_switch")
	a.Comment("on a switch:")
	a.LDA_abs(0x0468)
	a.EOR_imm16_w(0x0001)
	a.STA_abs(0x0466)
	a.Comment("PushBlock_CheckForPit leaves $0004 and PushBlock_Main increments it:")
	a.LDA_imm16_w(0x0005)
	a.BRA("mu_set_type")
	a.Label("mu_not_switch")
	a.Comment("PushBlock_CheckForPit leaves $FFFF and PushBlock_Main increments it:")
	a.LDA_imm16_w(0x0000)
	a.Label("mu_set_type")
	a.STA_long(tmp)
	loadPtr()
	a.LDA_long_x(bank + 2)
	a.TAY()
	a.LDA_long(tmp)
	a.STA_abs_y(manipType)
	a.Comment("draw the block at its new position:")
	for t, tile := range [4]uint16{0x0922, 0x0932, 0x0923, 0x0933} {
		a.LDA_imm16_w(tile)
		a.STA_abs_y(manipTiles + uint16(t)<<5)
	}
	draw()
	loadPtr()
	a.LDA_long_x(bank + 6)
	attrIndex()
	a.LDA_imm16_w(0x2727)
	a.STA_long_x(0x7F2000)
	a.STA_long_x(0x7F2040)

	a.Label("mu_next")
	a.LDA_long(count)
	a.INC()
	a.STA_long(count)
	a.LDA_long(ptr)
	a.CLC()
	a.ADC_imm16_w(manipUpdEntry)
	a.STA_long(ptr)
	a.JMP_abs("mu_loop")
	a.Label("mu_done")
	a.Comment("return how many entries were handled:")
	a.LDA_long(count)
	a.RTL()
	a.Label("mu_skip")
	a.LDA_imm16_w(manipAsmSkipped)
	a.RTL()
}

// emitOperateBlockDoor does what RoomTag_OperateBlockDoor does ($0468 = $0641 ^ 1, then move the shutters), unless
// another submodule is running. The tag itself only runs while Link is in the quadrant it is for, which the player who
// pushed the block always is but other players usually are not. 16-bit registers.
func emitOperateBlockDoor(a *asm.Emitter, label string) {
	a.Comment("operate the push block door like RoomTag_OperateBlockDoor:")
	a.SEP(0x20)
	a.LDA_dp(0x11)
	a.BNE(label)
	a.LDA_abs(0x0641)
	a.EOR_imm8_b(0x01)
	a.CMP_long(0x7E0468)
	a.BEQ(label)
	a.STA_abs(0x0468)
	a.LDA_imm8_b(0x25) // SFX3.25
	a.STA_abs(0x012F)
	a.LDA_imm8_b(0x05)
	a.STA_dp(0x11)
	a.REP(0x20)
	a.STZ_abs(0x068E)
	a.STZ_abs(0x0690)
	a.SEP(0x20)
	a.Label(label)
	a.REP(0x20)
}
