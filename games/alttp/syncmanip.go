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
// remote's replacement tiles (a pot can hide a hole or a switch); items hidden under pots are not spawned. A remote
// player's push block that finished moving is moved locally in one step: draw what was under it, then do what
// PushBlock_CheckForPit does at the new position.

const (
	manipCount         = uint16(0x0478)
	manipType          = uint16(0x0500)
	manipObjX          = uint16(0x0520)
	manipPos           = uint16(0x0540)
	manipTiles         = uint16(0x0560)
	manipSlots         = 16
	manipProbesPerRead = 3
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

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *manipUpdate
}

type manipRemoteSlot struct {
	Type  uint16
	ObjX  uint16
	Pos   uint16
	Flags uint8
	Tiles [4]uint16
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
	slot   int
	move   bool
	to     uint16
	tiles  [4]uint16
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
			s.Type = 0xFFFF
			log.Printf("alttp: manip: update: room $%03x slot %d moved to $%04x from player '%s'\n", u.room, it.slot, it.to, it.player)
		} else {
			s.revealed = true
			log.Printf("alttp: manip: update: room $%03x slot %d revealed from player '%s'\n", u.room, it.slot, it.player)
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
	fnDraw, ok1 := g.romFunctions[fnRoomDraw16x16Single]
	fnRTL, ok2 := g.romFunctions[fnBank01RTL]
	if !ok1 || !ok2 {
		return
	}

	items := make([]manipItem, 0, 4)
	queued := [manipSlots]bool{}
	size := 0
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
			cost := 0
			if manipIsPushBlock(rs.Type) {
				if rs.Type != 0 && rs.Type != 0xFFFF {
					// still moving:
					continue
				}
				if s.Type != 0 || s.Pos != s.OrigPos || rs.Pos == s.Pos || !manipIsStep(s.Pos, rs.Pos) {
					continue
				}
				it = manipItem{slot: j, move: true, to: rs.Pos, tiles: s.Tiles}
				cost = 120
			} else {
				if rs.Flags&manipFlagRevealed == 0 || !manipIsLiftable(s.Type) || !s.present || s.revealed {
					continue
				}
				it = manipItem{slot: j, tiles: s.Tiles}
				if rs.Flags&manipFlagTiles != 0 {
					it.tiles = rs.Tiles
				}
				cost = 40
			}
			if size+cost > 160 {
				continue
			}
			it.player = p.Name()
			size += cost
			queued[j] = true
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return
	}

	u = &manipUpdate{g: g, room: m.room, items: items}

	a = newEmitter()
	a.Comment(fmt.Sprintf("manipulables in room $%03x:", m.room))
	a.LDA_dp(0x10)
	a.CMP_imm16_w(0x0007)
	asmBNEFar(a, "mn_skip", "mn_module_ok")
	a.LDA_dp(0xA0)
	a.CMP_imm16_w(m.room)
	asmBNEFar(a, "mn_skip", "mn_room_ok")
	for k, it := range items {
		s := &m.slots[it.slot]
		o := uint16(it.slot << 1)
		next := fmt.Sprintf("mn_next_%d", k)
		ok := fmt.Sprintf("mn_ok_%d", k)

		a.Comment("stop when the VRAM stripe buffer is getting full:")
		a.LDA_abs(0x1000)
		a.CMP_imm16_w(0x0068)
		a.BCC(ok)
		a.LDA_imm16_w(uint16(k))
		a.JMP_abs("mn_report")
		a.Label(ok)

		if !it.move {
			a.Comment(fmt.Sprintf("reveal slot %d if still there:", it.slot))
			a.LDA_long(0x7F2000 + manipAttrIndex(s.Pos))
			a.AND_imm16_w(0x00FF)
			a.CMP_imm16_w(uint16(manipAttrBase) + uint16(it.slot))
			a.BNE(next)
			for t := uint16(0); t < 4; t++ {
				if it.tiles[t] != s.Tiles[t] {
					a.LDA_imm16_w(it.tiles[t])
					a.STA_abs(manipTiles + t<<5 + o)
				}
			}
			a.LDY_imm16_w(o)
			emitJSRBank01(a, fnDraw, fnRTL)
			a.Label(next)
			continue
		}

		notPit := fmt.Sprintf("mn_not_pit_%d", k)
		notSwitch := fmt.Sprintf("mn_not_switch_%d", k)
		setType := fmt.Sprintf("mn_set_type_%d", k)
		attr := 0x7F2000 + manipAttrIndex(it.to)
		a.Comment(fmt.Sprintf("move push block in slot %d from $%04x to $%04x if it did not move:", it.slot, s.Pos, it.to))
		a.LDA_abs(manipType + o)
		asmBNEFar(a, next, fmt.Sprintf("mn_still_%d", k))
		a.LDA_abs(manipPos + o)
		a.CMP_imm16_w(s.Pos)
		asmBNEFar(a, next, fmt.Sprintf("mn_unmoved_%d", k))
		a.Comment("draw what was under it:")
		a.LDY_imm16_w(o)
		emitJSRBank01(a, fnDraw, fnRTL)
		a.LDA_imm16_w(it.to)
		a.STA_abs(manipPos + o)
		a.Comment("do what PushBlock_CheckForPit does:")
		a.LDA_long(attr)
		a.AND_imm16_w(0x00FF)
		a.CMP_imm16_w(0x0020)
		a.BNE(notPit)
		a.Comment("fell into a pit; record where it lands in the room below:")
		a.LDX_imm16_w(s.ObjX)
		a.LDA_long(0x7EC000)
		a.AND_imm16_w(0x00FF)
		asmSTA_long_x(a, pushBlockTable)
		a.LDA_imm16_w(it.to)
		asmSTA_long_x(a, pushBlockTable+2)
		a.BRA(next)
		a.Label(notPit)
		a.CMP_imm16_w(0x0023)
		a.BNE(notSwitch)
		a.Comment("on a switch:")
		a.LDA_abs(0x0468)
		asmEOR_imm16(a, 0x0001)
		a.STA_abs(0x0466)
		a.LDA_imm16_w(0x0000)
		a.BRA(setType)
		a.Label(notSwitch)
		a.LDA_imm16_w(0xFFFF)
		a.Label(setType)
		a.STA_abs(manipType + o)
		a.Comment("draw the block at its new position:")
		for t, tile := range [4]uint16{0x0922, 0x0932, 0x0923, 0x0933} {
			a.LDA_imm16_w(tile)
			a.STA_abs(manipTiles + uint16(t)<<5 + o)
		}
		a.LDY_imm16_w(o)
		emitJSRBank01(a, fnDraw, fnRTL)
		a.LDA_imm16_w(0x2727)
		a.STA_long(attr)
		a.STA_long(attr + 0x40)
		a.Label(next)
	}
	a.LDA_imm16_w(uint16(len(items)))
	a.BRA("mn_report")
	a.Label("mn_skip")
	a.LDA_imm16_w(manipAsmSkipped)
	a.Label("mn_report")
	emitReport(a, index)

	return
}
