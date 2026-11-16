package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/alttpo/snes/asm"
	"o2/snes"
)

// Overworld map16 change synchronization
//
// The game records every persistent map16 change it makes in the current overworld area (lifted bushes and rocks,
// bombed walls, opened entrances, ...) in a log so that it can restore them after a mirror bonk
// (MirrorBonk_RecoverChangedTiles):
//
//   - $7E04AC is the byte index of the next free log entry. Every area load resets it to 0.
//   - $7EF800 is a [$A0]uint16 array of the tilemap offsets ($7E2000,X) of the changed tiles.
//   - $7EFA00 is a [$A0]uint16 array of the map16 tiles written there.
//
// Overworld_MemorizeMap16Change appends to the log (except for cut grass and dirt patches, which the game never
// restores) and Overworld_DoMapUpdate32x32 appends four entries at once. Entries are written before $04AC is advanced,
// so entries [0, $04AC/2) are always complete.
//
// The log is shared with the players in the same area (alttpo's Map16Changes.as) in message 0x17 in broadcastSector
// packets: `area u16`, `count u8` (entries in the sender's log), `start u8`, `len u8`, then `len` entries of
// `pos u16`, `tile u16` for log indices [start, start+len). New entries are sent as soon as they are seen and the
// whole log is sent every 16 frames so players entering the area catch up.
//
// Each receiver remembers how far into each remote player's log it has applied and only applies newly added entries.
// Applying an entry skips it if $7E2000,X already holds the tile, otherwise writes it, appends it to the local log
// (so it survives a mirror bonk and is passed on to players who enter the area later) and draws it with
// Overworld_DrawMap16_Persist if VRAM holds it; other tiles are drawn by the game as it scrolls:
//
//   - Small areas ($0712 = 0) are entirely in VRAM since the area load and the game never draws tiles while
//     scrolling there, so changes are always drawn.
//   - Large areas keep a 32x32 map16 ring in VRAM around $84, which scrolling moves along: the area load draws rows
//     -7..+24 and columns -8..+23 of $84, and scrolling draws the next row or column (-8 or +24) from $7E2000 before
//     moving $84, even if that row was already in VRAM. Rows and columns -7..+23 are always in VRAM and are drawn;
//     anything else is drawn by the game when scrolled to. (Drawing outside that range would overwrite the ring slot
//     of a row that is in VRAM, and leaving a tile inside it undrawn would show the old tile until it scrolls away
//     and back.)

const (
	map16LogIndex     = uint16(0x04AC)
	map16LogPos       = uint32(0x7EF800)
	map16LogTile      = uint32(0x7EFA00)
	map16LogMax       = 0xA0 // entries
	map16ReadWindow   = 0x10 // entries read per frame
	map16MaxPerUpdate = 8
	map16ChunkMax     = 0x40 // entries per message
	map16AsmSkipped   = 0xFF
	// map16TableEnd terminates the table passed to the map16 apply routine; tilemap offsets are even and < $2000:
	map16TableEnd = uint16(0xFFFF)
)

type map16Entry struct {
	Pos  uint16
	Tile uint16
}

// map16Local tracks the local player's map16 change log.
type map16Local struct {
	// valid is true when the log was read in an overworld module for area:
	valid   bool
	area    uint16
	count   int
	entries [map16LogMax]map16Entry
	// readBase is the log index the last enqueued window read starts at; -1 when none:
	readBase int
	// sent is how many entries were already sent:
	sent int
	// known is the latest tile the local tilemap is known to have at each position in the log:
	known map[uint16]uint16

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *map16Update

	// diagLogged is set once the remote players' logs were described after entering the area:
	diagLogged bool
}

// map16Remote tracks a remote player's map16 change log as received.
type map16Remote struct {
	area    uint16
	count   int
	entries [map16LogMax]map16Entry
	have    [map16LogMax]bool
	// applied is how many entries of this log were applied locally:
	applied int

	// lastHdr is the last message header received, for diagnostics:
	lastHdr map16Header
}

type map16Header struct {
	Area  uint16
	Count uint8
	Start uint8
	Len   uint8
}

func (r *map16Remote) reset(area uint16) {
	*r = map16Remote{area: area}
}

func (g *Game) resetMap16() {
	m := &g.map16
	if m.inflight != nil {
		m.inflight.cancelled = true
	}
	*m = map16Local{readBase: -1}
	g.resetMap16Applied()
}

// resetMap16Log is called when the local area was (re)loaded, which undoes all changes in the local tilemap.
func (g *Game) resetMap16Log() {
	m := &g.map16
	m.count = 0
	m.sent = 0
	m.known = make(map[uint16]uint16)
	g.resetMap16Applied()
}

// resetMap16Applied makes all remote changes apply again.
func (g *Game) resetMap16Applied() {
	for i := range g.players {
		g.players[i].Map16.applied = 0
	}
}

func isOverworldModule(module Module) bool {
	return module == 0x09 || module == 0x0B
}

// enqueueMap16Reads reads the part of the log past what we have read so far.
func (g *Game) enqueueMap16Reads(q []snes.Read) []snes.Read {
	m := &g.map16
	m.readBase = -1
	if !g.SyncOverworld || !isOverworldModule(g.local.Module) {
		return q
	}
	base := 0
	if m.valid {
		base = m.count
	}
	if base >= map16LogMax {
		return q
	}
	m.readBase = base
	q = g.readEnqueue(q, 0xF50000+(map16LogPos&0xFFFF)+uint32(base<<1), map16ReadWindow<<1, nil)
	q = g.readEnqueue(q, 0xF50000+(map16LogTile&0xFFFF)+uint32(base<<1), map16ReadWindow<<1, nil)
	return q
}

// readMap16 samples the local map16 change log. Called when WRAM is read from the SNES.
func (g *Game) readMap16() {
	local := g.local
	m := &g.map16

	if local.Module == 0x0E {
		// menus don't touch the log:
		return
	}
	if !isOverworldModule(local.Module) {
		// the next area load resets the log:
		if m.valid {
			g.resetMap16()
		}
		return
	}
	if !g.wramFresh[0x8A] || !g.wramFresh[map16LogIndex] || !g.wramFresh[map16LogIndex+1] {
		return
	}
	area := g.wramU16(0x8A)
	if m.valid && m.area != area {
		g.resetMap16()
	}
	// area loads reset the log in other submodules; $8A and $04AC are not updated together:
	if local.SubModule != 0 {
		return
	}

	n := int(g.wramU16(uint32(map16LogIndex)))
	if n&1 != 0 || n > map16LogMax<<1 {
		return
	}
	n >>= 1

	if !m.valid {
		m.valid = true
		m.area = area
		g.resetMap16Log()
	}
	if n < m.count {
		// the area was reloaded:
		g.resetMap16Log()
		return
	}

	if m.readBase != m.count {
		return
	}
	for i := m.count; i < n && i < m.readBase+map16ReadWindow; i++ {
		po := (map16LogPos & 0xFFFF) + uint32(i<<1)
		to := (map16LogTile & 0xFFFF) + uint32(i<<1)
		if !g.wramFresh[po] || !g.wramFresh[po+1] || !g.wramFresh[to] || !g.wramFresh[to+1] {
			break
		}
		e := map16Entry{Pos: g.wramU16(po), Tile: g.wramU16(to)}
		m.entries[i] = e
		m.known[e.Pos] = e.Tile
		m.count = i + 1
		log.Printf("alttp: map16: local: [$%02x] $%04x <- $%04x\n", i, e.Pos, e.Tile)
	}
}

// sendMap16 sends the new entries of the local log, and the whole log every 16 frames, to the players in the area.
func (g *Game) sendMap16() {
	m := &g.map16
	// menus and text boxes ($0E) keep the log; keep sending it so players entering the area still catch up:
	if !m.valid || !(isOverworldModule(g.local.Module) || g.local.Module == 0x0E) {
		return
	}

	start := m.sent
	if g.monotonicFrameTime&15 == 0 {
		start = 0
	} else if start >= m.count {
		return
	}

	for {
		end := start + map16ChunkMax
		if end > m.count {
			end = m.count
		}
		s := g.makeSectorMessage()
		if err := g.SerializeMap16(s, start, end); err != nil {
			panic(err)
		}
		g.send(s)
		start = end
		if start >= m.count {
			break
		}
	}
	m.sent = m.count
}

// SerializeMap16 writes message 0x17 for local log entries [start, end).
func (g *Game) SerializeMap16(w io.Writer, start, end int) (err error) {
	m := &g.map16
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgMap16)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, m.area); err != nil {
		return
	}
	if _, err = w.Write([]byte{uint8(m.count), uint8(start), uint8(end - start)}); err != nil {
		return
	}
	return binary.Write(w, binary.LittleEndian, m.entries[start:end])
}

func (g *Game) DeserializeMap16(p *Player, r io.Reader) (err error) {
	var hdr map16Header
	if err = binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("error deserializing map16 changes: %w", err)
	}
	entries := make([]map16Entry, hdr.Len)
	if err = binary.Read(r, binary.LittleEndian, entries); err != nil {
		return fmt.Errorf("error deserializing map16 changes: %w", err)
	}
	count, start := int(hdr.Count), int(hdr.Start)
	if count > map16LogMax || start+len(entries) > count {
		return nil
	}

	rm := &p.Map16
	if hdr != rm.lastHdr {
		// diagnostics: log only when the header changes, so the periodic whole-log resends don't spam:
		rm.lastHdr = hdr
		log.Printf("alttp: map16: from '%s': area $%02x count %d start %d len %d\n", p.Name(), hdr.Area, hdr.Count, hdr.Start, hdr.Len)
	}
	if rm.area != hdr.Area || count < rm.count {
		// another area, or the sender's area was reloaded:
		rm.reset(hdr.Area)
	}
	rm.count = count
	for k, e := range entries {
		i := start + k
		if rm.have[i] && rm.entries[i] != e && i < rm.applied {
			// the sender's log was replaced without us noticing; apply again from here:
			rm.applied = i
		}
		rm.entries[i] = e
		rm.have[i] = true
	}
	return nil
}

// map16Update is a generated update routine applying remote map16 changes, awaiting execution confirmation.
type map16Update struct {
	g       *Game
	area    uint16
	entries []map16Entry
	// ends is the remote log index each player's changes were applied up to, by player index:
	ends      map[int]int
	cancelled bool
}

func (u *map16Update) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	m := &g.map16
	if u.cancelled || m.inflight != u {
		return
	}
	m.inflight = nil

	if value == map16AsmSkipped {
		log.Printf("alttp: map16: update skipped\n")
		return
	}
	if !m.valid || m.area != u.area {
		return
	}
	n := int(value)
	if n > len(u.entries) {
		n = len(u.entries)
	}
	for _, e := range u.entries[:n] {
		m.known[e.Pos] = e.Tile
	}
	log.Printf("alttp: map16: update: applied %d of %d changes\n", n, len(u.entries))
	if n < len(u.entries) {
		// out of room for the VRAM stripes; the rest are applied by a later update (applying again is harmless):
		return
	}
	for i, end := range u.ends {
		rm := &g.players[i].Map16
		if rm.area == u.area && end <= rm.count && end > rm.applied {
			rm.applied = end
		}
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed; it is generated again.
func (u *map16Update) AbortAsm() {
	m := &u.g.map16
	if u.cancelled || m.inflight != u {
		return
	}
	m.inflight = nil
}

// generateMap16Update generates an update routine to apply the map16 changes of the remote players in the same area
// that were not applied yet. The caller must set inflight if the routine is emitted.
func (g *Game) generateMap16Update(newEmitter func() *asm.Emitter, index uint32) (u *map16Update, a *asm.Emitter) {
	m := &g.map16
	local := g.local
	if !m.valid || m.inflight != nil {
		return
	}
	if !isOverworldModule(local.Module) || local.SubModule != 0 {
		return
	}
	if !m.diagLogged {
		// describe what we know of the remote players' logs once per area visit, to explain missing changes:
		m.diagLogged = true
		for _, p := range g.RemotePlayers() {
			rm := &p.Map16
			have := 0
			for i := 0; i < rm.count; i++ {
				if rm.have[i] {
					have++
				}
			}
			log.Printf(
				"alttp: map16: area $%02x: remote '%s' team %d module $%02x location $%06x log area $%02x count %d have %d applied %d\n",
				m.area, p.Name(), p.Team, uint8(p.Module), p.Location, rm.area, rm.count, have, rm.applied,
			)
		}
	}

	// collect pending changes, keeping only the latest tile for each position:
	list := make([]map16Entry, 0, map16MaxPerUpdate)
	at := make(map[uint16]int, map16MaxPerUpdate)
	ends := make(map[int]int)
	for _, p := range g.RemotePlayers() {
		// game worlds are team specific; menus and text boxes ($0E) keep the log, like alttpo:
		if p.Team != local.Team || !(isOverworldModule(p.Module) || p.Module == 0x0E) || p.Location&0x010000 != 0 || p.OverworldArea != m.area {
			continue
		}
		rm := &p.Map16
		if rm.area != m.area {
			continue
		}
		i := rm.applied
		for ; i < rm.count && rm.have[i]; i++ {
			e := rm.entries[i]
			if j, ok := at[e.Pos]; ok {
				list[j].Tile = e.Tile
				continue
			}
			if len(list) >= map16MaxPerUpdate {
				break
			}
			at[e.Pos] = len(list)
			list = append(list, e)
		}
		if i > rm.applied {
			ends[p.Index()] = i
		}
	}
	if len(ends) == 0 {
		return
	}

	// skip changes the local tilemap already has (e.g. our own, or ones we passed on):
	entries := list[:0]
	for _, e := range list {
		if t, ok := m.known[e.Pos]; ok && t == e.Tile {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		for i, end := range ends {
			g.players[i].Map16.applied = end
		}
		return
	}

	u = &map16Update{g: g, area: m.area, entries: entries, ends: ends}
	log.Printf("alttp: map16: generating update for %d of %d pending changes\n", len(entries), len(list))

	a = newEmitter()
	a.Comment(fmt.Sprintf("map16 changes for area $%02x:", m.area))
	a.BRA("m16_start")
	table := a.PC()
	a.EmitBytes([]byte{byte(m.area), byte(m.area >> 8)})
	for _, e := range entries {
		a.EmitBytes([]byte{byte(e.Pos), byte(e.Pos >> 8), byte(e.Tile), byte(e.Tile >> 8)})
	}
	a.EmitBytes(binary.LittleEndian.AppendUint16(nil, map16TableEnd))
	a.Label("m16_start")
	a.LDA_imm16_w(uint16(table))
	a.JSR_abs(uint16(map16ApplyAddr & 0xFFFF))
	emitReport(a, index)

	return
}

// emitMap16Apply emits the routine at map16ApplyAddr in SRAM which applies a table of map16 changes. It is called by
// generated map16 update routines in 16-bit mode (REP #$30) with JSR, with A = address of the table in bank $70:
//
//	dw area                ; $8A the changes are for
//	dw pos, tile ...       ; tilemap offset ($7E2000,X) and map16 tile of each change
//	dw map16TableEnd
//
// It returns A = the number of changes applied (it stops early when the VRAM stripe buffer is getting full), or
// map16AsmSkipped if the player is no longer in the area in the overworld module, submodule 0. fnDraw is
// Overworld_DrawMap16_Persist.
func emitMap16Apply(a *asm.Emitter, fnDraw uint32) error {
	if a.Label("map16Apply") != map16ApplyAddr {
		return fmt.Errorf("map16ApplyAddr expected to be $%06x but got $%06x", a.PC(), map16ApplyAddr)
	}
	bank := map16ApplyAddr & 0xFF0000

	a.AssumeREP(0x30)
	a.Comment("X = table address; in the overworld module, submodule 0, in the table's area:")
	a.TAX()
	a.LDA_dp(0x10)
	a.CMP_imm16_w(0x0009)
	a.BEQ("m16_module_ok")
	a.CMP_imm16_w(0x000B)
	a.BNE_far("m16_skip")
	a.Label("m16_module_ok")
	a.LDA_dp(0x8A)
	a.CMP_long_x(bank)
	a.BNE_far("m16_skip")
	a.Comment("X = first entry, also pushed to count the entries applied:")
	a.INX()
	a.INX()
	a.PHX()
	a.Comment("$06 = $0712 (0 in small areas, which are entirely in VRAM):")
	a.LDA_abs(0x0712)
	a.AND_imm16_w(0x00FF)
	a.STA_dp(0x06)
	a.Comment("$08 = row and $0A = column of $84 for the VRAM residency check:")
	a.LDA_dp(0x84)
	a.AND_imm16_w(0x1F80)
	a.STA_dp(0x08)
	a.LDA_dp(0x84)
	a.AND_imm16_w(0x007E)
	a.STA_dp(0x0A)
	a.Label("m16_loop")
	a.Comment("stop when the VRAM stripe buffer is getting full or at the end of the table:")
	a.LDA_abs(0x1000)
	a.CMP_imm16_w(0x0080)
	a.BCS("m16_done")
	a.LDA_long_x(bank + 0)
	a.CMP_imm16_w(map16TableEnd)
	a.BEQ("m16_done")
	a.PHX()
	a.LDA_long_x(bank + 2)
	a.STA_dp(0x0E)
	a.LDA_long_x(bank + 0)
	a.TAX()
	a.Comment("skip if the tilemap already has this tile:")
	a.LDA_long_x(0x7E2000)
	a.CMP_dp(0x0E)
	a.BEQ("m16_next")
	a.Comment("memorize the change like Overworld_MemorizeMap16Change if the log has room:")
	a.LDA_abs(map16LogIndex)
	a.CMP_imm16_w(map16LogMax << 1)
	a.BCS("m16_no_memo")
	a.PHX()
	a.TAX()
	a.LDA_dp(0x0E)
	a.STA_long_x(map16LogTile)
	a.PLA()
	a.STA_long_x(map16LogPos)
	a.INX()
	a.INX()
	a.STX_abs(map16LogIndex)
	a.TAX()
	a.Label("m16_no_memo")
	a.Comment("small areas never stream tiles while scrolling; always draw:")
	a.LDA_dp(0x06)
	a.BEQ("m16_draw")
	a.Comment("large areas: draw if within 7 map16 rows/columns before or 23 after $84, which VRAM always holds:")
	a.TXA()
	a.AND_imm16_w(0x1F80)
	a.SEC()
	a.SBC_dp(0x08)
	a.CLC()
	a.ADC_imm16_w(7 << 7)
	a.CMP_imm16_w(31 << 7)
	a.BCS("m16_offscreen")
	a.TXA()
	a.AND_imm16_w(0x007E)
	a.SEC()
	a.SBC_dp(0x0A)
	a.CLC()
	a.ADC_imm16_w(7 << 1)
	a.CMP_imm16_w(31 << 1)
	a.BCS("m16_offscreen")
	a.Label("m16_draw")
	a.LDA_dp(0x0E)
	a.JSL(fnDraw)
	a.SEP(0x20)
	a.LDA_imm8_b(0x01)
	a.STA_dp(0x14)
	a.REP(0x20)
	a.BRA("m16_next")
	a.Label("m16_offscreen")
	a.LDA_dp(0x0E)
	a.STA_long_x(0x7E2000)
	a.Label("m16_next")
	a.PLX()
	a.INX()
	a.INX()
	a.INX()
	a.INX()
	a.JMP_abs("m16_loop")
	a.Label("m16_done")
	a.Comment("return how many changes were applied:")
	a.TXA()
	a.SEC()
	a.SBC_sr(0x01)
	a.LSR()
	a.LSR()
	a.PLX()
	a.RTS()
	a.Label("m16_skip")
	a.LDA_imm16_w(map16AsmSkipped)
	a.RTS()
	return nil
}
