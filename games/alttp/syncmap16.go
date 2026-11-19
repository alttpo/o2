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
// The log is shared with the other players (alttpo's Map16Changes.as) in message 0x17: `location u24`, `count u8`
// (entries in the sender's log), `start u8`, `len u8`, then `len` entries of `pos u16`, `tile u16` for log indices
// [start, start+len). New entries are sent as soon as they are seen and the whole log every 32 frames. It is sent to
// all players (broadcastAll), not just those in the same area, so that a player entering the area already has the
// changes while the area loads and they appear seamlessly (see map16stage.go).
//
// The location is the one the log was read in, encoded like the location in message 0x01: bit 17 the dark world
// ($0FFF & 1), bit 16 the underworld (0) and bits 0-15 the area ($8A). The log is only read, and changes are only
// applied, while $0FFF agrees with the area's world (areas $40-$7F are the dark world), which it does not while a
// warp switches worlds, and changes are only applied in exactly the sender's location, which must also be the
// sender's current location; the update routine checks $8A and $0FFF again when it runs.
//
// Each receiver remembers how far into each remote player's log it has applied and only applies newly added entries.
// Applying an entry appends it to the local log (so it survives a mirror bonk and is passed on to players who enter
// the area later). If $7E2000,X already holds the tile (the stage hook applied it while the area loaded), that is all;
// otherwise it writes it and draws it with Overworld_DrawMap16_Persist if VRAM holds it; other tiles are drawn by the
// game as it scrolls:
//
//   - Small areas ($0712 = 0) are entirely in VRAM since the area load and the game never draws tiles while
//     scrolling there, so changes are always drawn.
//   - Large areas keep a 32x32 map16 ring in VRAM around $84, which scrolling moves along: the area load draws rows
//     -7..+24 and columns -8..+23 of $84, and scrolling draws the next row or column (-8 or +24) from $7E2000 before
//     moving $84, even if that row was already in VRAM. Rows and columns -7..+23 are always in VRAM and are drawn;
//     anything else is drawn by the game when scrolled to. (Drawing outside that range would overwrite the ring slot
//     of a row that is in VRAM, and leaving a tile inside it undrawn would show the old tile until it scrolls away
//     and back.)
//
// An entry that arrives while the local player has been in the area for a while (longer than the senders take to
// resend their whole log, so not while catching up) is live: the remote player changed the tile just now. If a live
// entry replaces a bush, rock or sign that is still there locally, the update routine also reveals the secret under
// it (see syncsecrets.go).

const (
	map16LogIndex     = uint16(0x04AC)
	map16LogPos       = uint32(0x7EF800)
	map16LogTile      = uint32(0x7EFA00)
	map16LogMax       = 0xA0 // entries
	map16ReadWindow   = 0x10 // entries read per frame
	map16MaxPerUpdate = 8
	map16ChunkMax     = 0x40 // entries per message
	map16AsmSkipped   = 0xFF
	// map16RevealFlag is set in the tilemap offset of a change in the table for map16Update (see emitMap16Update) to
	// reveal the secret under the object there; tilemap offsets are even and < $2000:
	map16RevealFlag = uint16(0x8000)
	// map16LiveAfter is how many reads of the log in an area pass before entries received are taken as live; senders
	// resend their whole log every 32 frames (older versions every 16), so entries made before the local player arrived
	// come in before that:
	map16LiveAfter = 48
	// map16ResendMask: the whole log is sent on frames where (frame & map16ResendMask) == 0:
	map16ResendMask = 31
)

type map16Entry struct {
	Pos  uint16
	Tile uint16
}

// map16Local tracks the local player's map16 change log.
type map16Local struct {
	// valid is true when the log was read in an overworld module at loc (see owLocation):
	valid   bool
	loc     uint32
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

	// age is how many times the log was read since the area was loaded; see map16LiveAfter:
	age int
}

// map16Remote tracks a remote player's map16 change log as received.
type map16Remote struct {
	// loc is the location (see owLocation) the sender's log belongs to:
	loc     uint32
	count   int
	entries [map16LogMax]map16Entry
	have    [map16LogMax]bool
	// live is set for entries first received while the local player was in the area, until they were applied:
	live [map16LogMax]bool
	// applied is how many entries of this log were applied locally:
	applied int

	// lastHdr is the last message header received, for diagnostics:
	lastHdr map16Header
}

type map16Header struct {
	Loc   uint32
	Count uint8
	Start uint8
	Len   uint8
}

func (r *map16Remote) reset(loc uint32) {
	*r = map16Remote{loc: loc}
}

// owLocation returns the overworld location Link is in, like the location sent in message 0x01: bit 17 is the dark
// world ($0FFF & 1), bit 16 (underworld) is 0 and bits 0-15 are the area ($8A). ok is false unless both were read this
// cycle and agree (the dark world areas are $40-$7F), which they don't while a mirror or portal warp switches worlds.
func (g *Game) owLocation() (loc uint32, ok bool) {
	if !g.wramFresh[0x8A] || !g.wramFresh[0x8B] || !g.wramFresh[0x0FFF] {
		return
	}
	return g.owLocationLastRead()
}

// owLocationLastRead is owLocation from the values last read, which may be from an earlier read cycle. Generated update
// routines check $8A and $0FFF again when they run.
func (g *Game) owLocationLastRead() (loc uint32, ok bool) {
	area := g.wramU16(0x8A)
	world := uint32(g.wram[0x0FFF] & 1)
	if uint32(area>>6)&1 != world {
		return
	}
	return world<<17 | uint32(area), true
}

// owLocationArea returns the area of an overworld location.
func owLocationArea(loc uint32) uint16 {
	return uint16(loc)
}

// emitWorldCheck emits a check that $0FFF is the world of loc, branching to skip if not; 16-bit A:
func emitWorldCheck(a *asm.Emitter, loc uint32, skip string) {
	a.Comment("in the same world ($0FFF):")
	a.LDA_abs(0x0FFF)
	a.AND_imm16_w(0x0001)
	a.CMP_imm16_w(uint16(loc>>17) & 1)
	a.BNE(skip)
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
	m.age = 0
	g.resetMap16Applied()
	// changes received before are not revealed again:
	for i := range g.players {
		g.players[i].Map16.live = [map16LogMax]bool{}
	}
}

// resetMap16Applied makes all remote changes apply again.
func (g *Game) resetMap16Applied() {
	for i := range g.players {
		g.players[i].Map16.applied = 0
	}
}

// map16LogBelongsToArea returns false while Link lands from a mirror or portal warp. The warp (Module09_MirrorWarp)
// changes the area and returns to submodule 0 but keeps the previous area's log, which LinkState_CrossingWorlds only
// resets once Link lands in the other world (and keeps for a mirror bonk, which takes him back to that area). Reading
// it before would send the previous area's changes as changes of the new area.
func (g *Game) map16LogBelongsToArea() bool {
	return g.wramFresh[0x5D] && g.wram[0x5D] != linkStateMirror
}

// linkStateMirror is Link's state ($5D) while warping between worlds (kPlayerState_Mirror):
const linkStateMirror = 0x14

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
	if !g.wramFresh[map16LogIndex] || !g.wramFresh[map16LogIndex+1] {
		return
	}
	loc, ok := g.owLocation()
	if !ok {
		return
	}
	if m.valid && m.loc != loc {
		g.resetMap16()
	}
	// area loads reset the log in other submodules; $8A and $04AC are not updated together:
	if local.SubModule != 0 {
		return
	}
	if !g.map16LogBelongsToArea() {
		return
	}

	n := int(g.wramU16(uint32(map16LogIndex)))
	if n&1 != 0 || n > map16LogMax<<1 {
		return
	}
	n >>= 1

	if !m.valid {
		m.valid = true
		m.loc = loc
		g.resetMap16Log()
	}
	if n < m.count {
		// the area was reloaded:
		g.resetMap16Log()
		return
	}

	if m.age < map16LiveAfter {
		m.age++
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

// sendMap16 sends the new entries of the local log, and the whole log every 32 frames, to all players.
func (g *Game) sendMap16() {
	m := &g.map16
	// menus and text boxes ($0E) keep the log; keep sending it so players entering the area still catch up:
	if !m.valid || !(isOverworldModule(g.local.Module) || g.local.Module == 0x0E) {
		return
	}

	start := m.sent
	if g.monotonicFrameTime&map16ResendMask == 0 {
		start = 0
	} else if start >= m.count {
		return
	}

	for {
		end := start + map16ChunkMax
		if end > m.count {
			end = m.count
		}
		// to all players, so that players entering the area have the changes while it loads:
		s := g.makeBroadcastMessage()
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
	if err = writeU24(w, m.loc); err != nil {
		return
	}
	if _, err = w.Write([]byte{uint8(m.count), uint8(start), uint8(end - start)}); err != nil {
		return
	}
	return binary.Write(w, binary.LittleEndian, m.entries[start:end])
}

func (g *Game) DeserializeMap16(p *Player, r io.Reader) (err error) {
	var hdr map16Header
	if hdr.Loc, err = readU24(r); err != nil {
		return fmt.Errorf("error deserializing map16 changes: %w", err)
	}
	var b [3]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return fmt.Errorf("error deserializing map16 changes: %w", err)
	}
	hdr.Count, hdr.Start, hdr.Len = b[0], b[1], b[2]
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
		log.Printf("alttp: map16: from '%s': location $%06x count %d start %d len %d\n", p.Name(), hdr.Loc, hdr.Count, hdr.Start, hdr.Len)
	}
	if rm.loc != hdr.Loc || count < rm.count {
		// another area, or the sender's area was reloaded:
		rm.reset(hdr.Loc)
	}
	rm.count = count
	// entries arriving while we have been in the area for a while were just made by the sender:
	m := &g.map16
	here := m.valid && m.loc == hdr.Loc && m.age >= map16LiveAfter
	for k, e := range entries {
		i := start + k
		if rm.have[i] && rm.entries[i] != e && i < rm.applied {
			// the sender's log was replaced without us noticing; apply again from here:
			rm.applied = i
		}
		if !rm.have[i] && here {
			rm.live[i] = true
		}
		rm.entries[i] = e
		rm.have[i] = true
	}
	return nil
}

// map16Update is a generated update routine applying remote map16 changes, awaiting execution confirmation.
type map16Update struct {
	g       *Game
	loc     uint32
	entries []map16Entry
	// ends is the remote log index each player's changes were applied up to, by player index:
	ends map[int]int
	// live are the live remote log entries whose secrets the routine reveals, by player index:
	live      map[int][]int
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
	// the routine revealed the secrets under these, if any; never again:
	for pi, list := range u.live {
		rm := &g.players[pi].Map16
		if rm.loc != u.loc {
			continue
		}
		for _, i := range list {
			rm.live[i] = false
		}
	}
	if !m.valid || m.loc != u.loc {
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
		if rm.loc == u.loc && end <= rm.count && end > rm.applied {
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
	if !g.map16LogBelongsToArea() {
		return
	}
	if loc, ok := g.owLocationLastRead(); !ok || loc != m.loc {
		// Link is not where the log was read (yet):
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
				"alttp: map16: location $%06x: remote '%s' team %d module $%02x location $%06x log location $%06x count %d have %d applied %d\n",
				m.loc, p.Name(), p.Team, uint8(p.Module), p.Location, rm.loc, rm.count, have, rm.applied,
			)
		}
	}

	// collect pending changes, keeping only the latest tile for each position:
	list := make([]map16Entry, 0, map16MaxPerUpdate)
	at := make(map[uint16]int, map16MaxPerUpdate)
	ends := make(map[int]int)
	// reveal is set for changes the remote player made just now, which reveal the secret under a lifted object:
	reveal := make([]bool, 0, map16MaxPerUpdate)
	live := make(map[int][]int)
	for _, p := range g.RemotePlayers() {
		// game worlds are team specific; menus and text boxes ($0E) keep the log, like alttpo:
		// only changes made in exactly this location, including the world, by players who are still there:
		if p.Team != local.Team || !(isOverworldModule(p.Module) || p.Module == 0x0E) || p.Location != m.loc {
			continue
		}
		rm := &p.Map16
		if rm.loc != m.loc {
			continue
		}
		i := rm.applied
		for ; i < rm.count && rm.have[i]; i++ {
			e := rm.entries[i]
			if j, ok := at[e.Pos]; ok {
				list[j].Tile = e.Tile
				if rm.live[i] {
					reveal[j] = true
					live[p.Index()] = append(live[p.Index()], i)
				}
				continue
			}
			if len(list) >= map16MaxPerUpdate {
				break
			}
			at[e.Pos] = len(list)
			list = append(list, e)
			reveal = append(reveal, rm.live[i])
			if rm.live[i] {
				live[p.Index()] = append(live[p.Index()], i)
			}
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
	var reveals []uint16
	for k, e := range list {
		if t, ok := m.known[e.Pos]; ok && t == e.Tile {
			continue
		}
		entries = append(entries, e)
		// only where a remote player removed a small object (not where one put it):
		if reveal[k] && !g.isOwLiftable(e.Tile) {
			reveals = append(reveals, e.Pos)
		}
	}
	if len(entries) == 0 {
		for i, end := range ends {
			rm := &g.players[i].Map16
			rm.applied = end
			for _, k := range live[i] {
				rm.live[k] = false
			}
		}
		return
	}

	_, ok1 := g.romFunctions[fnOverworldRevealSecret]
	_, ok2 := g.romFunctions[fnBank1BRTL]
	_, ok3 := g.romFunctions[fnSpawnThrowableTerrainSilently]
	if !ok1 || !ok2 || !ok3 || len(g.owLiftablesFor()) == 0 {
		reveals = nil
	}
	revealAt := make(map[uint16]bool, len(reveals))
	for _, pos := range reveals {
		revealAt[pos] = true
	}

	// the table for map16Update (see emitMap16Update):
	area := owLocationArea(m.loc)
	data := make([]byte, 0, 4+len(entries)*4+2)
	data = binary.LittleEndian.AppendUint16(data, area)
	data = binary.LittleEndian.AppendUint16(data, uint16(m.loc>>17)&1)
	for _, e := range entries {
		pos := e.Pos
		if revealAt[pos] {
			pos |= map16RevealFlag
		}
		data = binary.LittleEndian.AppendUint16(data, pos)
		data = binary.LittleEndian.AppendUint16(data, e.Tile)
	}
	data = binary.LittleEndian.AppendUint16(data, updateTableEnd)
	table, ok := g.updData.add(data)
	if !ok {
		return nil, nil
	}

	u = &map16Update{g: g, loc: m.loc, entries: entries, ends: ends, live: live}
	log.Printf("alttp: map16: generating update for %d of %d pending changes\n", len(entries), len(list))
	if len(reveals) > 0 {
		log.Printf("alttp: map16: revealing secrets at %d positions\n", len(reveals))
	}

	a = newEmitter()
	a.Comment(fmt.Sprintf("map16 changes for location $%06x, table at $%06x:", m.loc, table))
	emitCallUpdateROM(a, map16UpdateStubAddr, table)
	emitReport(a, index)

	return
}

// emitMap16Update emits the routine in ROM which applies a table of map16 changes. It is called by generated map16
// update routines in 16-bit mode (REP #$30) with JSL through the SRAM stub at map16UpdateStubAddr, with A = address
// of the table in updateDataBank:
//
//	dw area                ; $8A the changes are for
//	dw world               ; $0FFF & 1 the changes are for
//	dw pos, tile ...       ; tilemap offset ($7E2000,X) and map16 tile of each change; map16RevealFlag set in pos
//	                       ; to first reveal the secret under the small liftable object there, if it still is
//	dw updateTableEnd
//
// The secrets are revealed before any tile changes (rv has the routines and objects; nothing is revealed if it is not
// ok). A change whose tile $7E2000 already holds is only memorized. It returns A = the number of changes applied (it
// stops early when the VRAM stripe buffer is getting full), or map16AsmSkipped if the player is no longer in the
// area and world in the overworld module, submodule 0. fnDraw is Overworld_DrawMap16_Persist.
func emitMap16Update(a *asm.Emitter, fnDraw uint32, rv owRevealROM) error {
	bank := uint32(updateDataBank)

	a.AssumeREP(0x30)
	a.Label("map16Update")
	var revealSub uint16
	if rv.ok {
		a.Comment("the subroutines that reveal secrets precede the code:")
		a.JMP_abs("m16_start")
		spawnSub := emitSpawnSecretSub(a, rv.fnSpawn)
		revealSub = emitOwRevealSub(a, rv.liftables, rv.fnReveal, rv.fnRTL, spawnSub)
		a.Label("m16_start")
	}
	a.Comment("X = table address; in the table's world ($0FFF):")
	a.TAX()
	a.LDA_abs(0x0FFF)
	a.AND_imm16_w(0x0001)
	a.CMP_long_x(bank + 2)
	a.BNE_far("m16_skip")
	a.Comment("in the overworld module, submodule 0, in the table's area:")
	a.LDA_dp(0x10)
	a.CMP_imm16_w(0x0009)
	a.BEQ("m16_module_ok")
	a.CMP_imm16_w(0x000B)
	a.BNE_far("m16_skip")
	a.Label("m16_module_ok")
	a.LDA_dp(0x8A)
	a.CMP_long_x(bank)
	a.BNE_far("m16_skip")
	a.Comment("X = first entry:")
	a.INX()
	a.INX()
	a.INX()
	a.INX()
	if rv.ok {
		a.Comment("reveal the secrets under the objects at the flagged positions while they are still there:")
		a.PHX()
		a.Label("m16_rv_loop")
		a.LDA_long_x(bank + 0)
		a.CMP_imm16_w(updateTableEnd)
		a.BEQ("m16_rv_done")
		a.BIT_imm16_w(map16RevealFlag)
		a.BEQ("m16_rv_next")
		a.PHX()
		a.AND_imm16_w(0x1FFE)
		a.TAX()
		a.JSR_abs(revealSub)
		a.PLX()
		a.Label("m16_rv_next")
		a.INX()
		a.INX()
		a.INX()
		a.INX()
		a.BRA("m16_rv_loop")
		a.Label("m16_rv_done")
		a.PLX()
	}
	a.Comment("the first entry is pushed to count the entries applied:")
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
	a.BCC("m16_room")
	a.JMP_abs("m16_done")
	a.Label("m16_room")
	a.LDA_long_x(bank + 0)
	a.CMP_imm16_w(updateTableEnd)
	a.BNE("m16_entry")
	a.JMP_abs("m16_done")
	a.Label("m16_entry")
	a.PHX()
	a.LDA_long_x(bank + 2)
	a.STA_dp(0x0E)
	a.LDA_long_x(bank + 0)
	a.AND_imm16_w(0x1FFE)
	a.TAX()
	a.Comment("$0C = 1 if the tilemap already has this tile (e.g. applied by the stage hook while the area loaded); it is")
	a.Comment("memorized but not drawn then:")
	a.STZ_dp(0x0C)
	a.LDA_long_x(0x7E2000)
	a.CMP_dp(0x0E)
	a.BNE("m16_changed")
	a.INC_dp(0x0C)
	a.Label("m16_changed")
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
	a.LDA_dp(0x0C)
	a.BNE_far("m16_next")
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
	a.RTL()
	a.Label("m16_skip")
	a.LDA_imm16_w(map16AsmSkipped)
	a.RTL()
	return nil
}
