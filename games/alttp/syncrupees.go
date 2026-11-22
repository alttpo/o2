package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sort"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Rupee floor synchronization
//
// Some dungeon rooms have a rupee floor (room object $12 of subtype 3, RoomDraw_RupeeFloor): 3x3 rupees of 8x16 pixels
// drawn into the lower layer's tilemap ($7E2000) with tile attribute $60 (TileBehavior_RupeeTile) in $7F2000, e.g. in
// the north east quadrant of the Palace of Darkness room $099. Walking over one gives 5 rupees and calls
// DeleteRupeeTile with $00/$02 = Y/X, which replaces its two tiles with $190F, their attributes with that tile's,
// queues a VRAM stripe ($1000), sets $0403 bit $10 and $14 = 1. A room whose $0403 bit $10 is set when it loads has no
// rupees at all. That bit is shared with the room's flags ($7EF000, and message 0x1A), but the rupees collected while
// another player is in the room were only gone for them once they reloaded the room.
//
// The patch hooks the end of DeleteRupeeTile (rupeeHookExpected, `LDA #$01 : STA $14`) to log the tilemap offset of
// each rupee it deletes ($06) at rupeeLogAddr, with the room it was deleted in ($A0); the log is emptied when a rupee
// is deleted in another room. o2 reads it with the SRAM reads.
//
// Shared with alttpo (Rupees.as) in message 0x20, broadcastSector to the room's sector: `room u16`, `count u8`, then
// `count` tilemap offsets `pos u16` of the rupees deleted in the room (DeleteRupeeTile's $06: (y/8 * 64 + x/8) * 2).
// It is sent in the dungeon module while the log is for the current room.
//
// A player deletes the rupees that remote players in the same room deleted, by calling DeleteRupeeTile without giving
// the rupees, for the positions whose attribute is still $60 (and at most rupeesPerFrame a frame, since each queues a
// VRAM stripe). The rupees deleted this way are logged too, so they are passed on to players who enter later.

const (
	// rupeeLogAddr: the room ($A0) of the rupees logged (u16), their byte length (u16), then up to rupeeLogMax
	// tilemap offsets (u16). Emptied by the init code:
	rupeeLogAddr  = uint32(0x706D20)
	rupeeLogRoom  = rupeeLogAddr
	rupeeLogLen   = rupeeLogAddr + 2
	rupeeLogList  = rupeeLogAddr + 4
	rupeeLogMax   = 32
	rupeeLogEnd   = rupeeLogList + rupeeLogMax*2
	rupeeLogReadN = rupeeLogEnd - rupeeLogAddr

	// rupeeHookOffset is the offset of `LDA #$01 : STA $14` from the start of DeleteRupeeTile, in all supported ROMs:
	rupeeHookOffset = 0x87

	// rupeeTileAttr is a rupee's tile attribute in $7F2000:
	rupeeTileAttr = uint8(0x60)
	// rupeesPerFrame is the most rupees deleted by one update routine, and rupeeStripeLimit the VRAM stripe buffer
	// offset ($1000) at which it stops; DeleteRupeeTile queues $18 bytes:
	rupeesPerFrame   = 4
	rupeeStripeLimit = uint16(0x0068)
)

// the log is after remoteFrame's recording and before the run timing:
const _ = rupeeLogAddr - (remoteReplayAddr + remoteReplaySize)
const _ = runTimingAddr - rupeeLogEnd

// rupeeHookExpected is the code at DeleteRupeeTile+rupeeHookOffset that the hook replaces with `JSL hook`, `PLB` and
// `RTL` after it. The hook runs the first two instructions before returning.
var rupeeHookExpected = []byte{0xA9, 0x01, 0x85, 0x14, 0xAB, 0x6B}

var rupeeLogPak = func() uint32 {
	pak, err := lorom.BusAddressToPak(rupeeLogAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// rupeeHookSite returns the address of the code the hook replaces, if the ROM has it.
func rupeeHookSite(rom *snes.ROM, fns map[romFunction]uint32) (addr uint32, ok bool) {
	fn, ok := fns[fnDeleteRupeeTile]
	if !ok {
		return 0, false
	}
	addr = fn + rupeeHookOffset
	pak, err := lorom.BusAddressToPak(addr)
	if err != nil || int(pak)+len(rupeeHookExpected) > len(rom.Contents) {
		return 0, false
	}
	return addr, bytes.Equal(rom.Contents[pak:pak+uint32(len(rupeeHookExpected))], rupeeHookExpected)
}

// emitRupeeInit emits the init code that empties the rupee log. 16-bit A.
func emitRupeeInit(a *asm.Emitter) {
	a.Comment("empty the rupee log:")
	a.LDA_imm16_w(0x0000)
	a.STA_long(rupeeLogLen)
}

// emitRupeeHook emits the routine that the patched DeleteRupeeTile calls with JSL once it deleted a rupee: it logs the
// rupee's tilemap offset ($06), then runs the instructions it replaced. Called with 8-bit registers; preserves X, Y
// and P.
func emitRupeeHook(a *asm.Emitter) {
	a.AssumeSEP(0x30)
	a.Label("rupeeHook")
	a.PHP()
	a.REP(0x30)
	a.PHX()
	a.Comment("a rupee of another room empties the log:")
	a.LDA_dp(0xA0)
	a.CMP_long(rupeeLogRoom)
	a.BEQ("rh_room")
	a.STA_long(rupeeLogRoom)
	a.LDA_imm16_w(0x0000)
	a.STA_long(rupeeLogLen)
	a.Label("rh_room")
	a.Comment("the offset first, then the length, so that a read of the log never sees an entry not yet written:")
	a.LDA_long(rupeeLogLen)
	a.CMP_imm16_w(rupeeLogMax * 2)
	a.BCS("rh_done")
	a.TAX()
	a.LDA_dp(0x06)
	a.STA_long_x(rupeeLogList)
	a.INX()
	a.INX()
	a.TXA()
	a.STA_long(rupeeLogLen)
	a.Label("rh_done")
	a.PLX()
	a.PLP()
	a.Comment("the instructions the JSL replaced:")
	a.EmitBytes(rupeeHookExpected[:4])
	a.RTL()
}

// rupeesRemote is a remote player's deleted rupees as received in message 0x20.
type rupeesRemote struct {
	valid bool
	room  uint16
	pos   []uint16
}

// rupeesLocal tracks the rupees deleted in the current room.
type rupeesLocal struct {
	// room is the room ($A0) the local player is in, valid while in the dungeon module:
	room  uint16
	valid bool
	// pos are the rupees deleted in room, from the log, sorted:
	pos []uint16
	// done are the remote rupees the update routines handled in this room visit, whether or not there was a rupee
	// there:
	done map[uint16]bool

	inflight *rupeesUpdate
}

// resetRupees forgets the rupee state; it is read again from the SNES.
func (g *Game) resetRupees() {
	r := &g.rupees
	if r.inflight != nil {
		r.inflight.cancelled = true
	}
	*r = rupeesLocal{}
}

// enqueueRupeesRead reads the rupee log with the SRAM reads in the dungeon module.
func (g *Game) enqueueRupeesRead(q []snes.Read) []snes.Read {
	if !g.SyncUnderworldLocal || g.local.Module != 0x07 {
		return q
	}
	if _, ok := g.romFunctions[fnDeleteRupeeTile]; !ok {
		return q
	}
	return g.readEnqueue(q, rupeeLogPak, uint8(rupeeLogReadN), nil)
}

// readRupeesRoom follows the local player's room. Called when WRAM is read from the SNES.
func (g *Game) readRupeesRoom() {
	local := g.local
	r := &g.rupees
	if local.Module == 0x0E {
		// menus keep the room:
		return
	}
	if local.Module != 0x07 || !g.wramFresh[0xA0] || !g.wramFresh[0xA1] {
		if local.Module != 0x07 && r.valid {
			g.resetRupees()
		}
		return
	}
	room := g.wramU16(0xA0)
	if !r.valid || r.room != room {
		g.resetRupees()
		r.room, r.valid = room, true
	}
}

// readRupeesResponse takes the rupee log from an SRAM read response.
func (g *Game) readRupeesResponse(rsp snes.Response) {
	if rsp.Address != rupeeLogPak || len(rsp.Data) != int(rupeeLogReadN) {
		return
	}
	r := &g.rupees
	if !r.valid {
		return
	}
	d := rsp.Data
	room := binary.LittleEndian.Uint16(d[0:])
	n := int(binary.LittleEndian.Uint16(d[2:]))
	if room != r.room || n&1 != 0 || n > rupeeLogMax*2 {
		return
	}
	pos := make([]uint16, 0, n/2)
	for o := 4; o < 4+n; o += 2 {
		pos = append(pos, binary.LittleEndian.Uint16(d[o:]))
	}
	sort.Slice(pos, func(i, j int) bool { return pos[i] < pos[j] })
	pos = compactU16(pos)
	if len(pos) != len(r.pos) {
		log.Printf("alttp: rupees: local: room $%03x: %d rupees deleted\n", room, len(pos))
	}
	r.pos = pos
}

func compactU16(s []uint16) []uint16 {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func (r *rupeesLocal) has(pos uint16) bool {
	i := sort.Search(len(r.pos), func(i int) bool { return r.pos[i] >= pos })
	return i < len(r.pos) && r.pos[i] == pos
}

// SerializeRupees writes message 0x20 with the rupees deleted in the current room, if any.
func (g *Game) SerializeRupees(w io.Writer) (err error) {
	r := &g.rupees
	if !r.valid || len(r.pos) == 0 {
		return
	}
	b := []byte{uint8(MsgRupees), uint8(r.room), uint8(r.room >> 8), uint8(len(r.pos))}
	for _, p := range r.pos {
		b = binary.LittleEndian.AppendUint16(b, p)
	}
	_, err = w.Write(b)
	return
}

func (g *Game) DeserializeRupees(p *Player, r io.Reader) (err error) {
	var hdr struct {
		Room  uint16
		Count uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("error deserializing rupees: %w", err)
	}
	pos := make([]uint16, hdr.Count)
	if err = binary.Read(r, binary.LittleEndian, pos); err != nil {
		return fmt.Errorf("error deserializing rupees: %w", err)
	}
	p.Rupees = rupeesRemote{valid: true, room: hdr.Room, pos: pos}
	return nil
}

// rupeesToDelete returns the rupees that remote players in the same room deleted, which are neither deleted here nor
// handled already, and who deleted the first one.
func (g *Game) rupeesToDelete() (list []uint16, from *Player) {
	local := g.local
	r := &g.rupees
	seen := map[uint16]bool{}
	for _, p := range g.RemotePlayers() {
		if p == local || p.TTL() <= 0 || p.Team != local.Team {
			continue
		}
		if p.Module != 0x07 || p.Location&0x010000 == 0 || p.DungeonRoom != r.room {
			continue
		}
		rr := &p.Rupees
		if !rr.valid || rr.room != r.room {
			continue
		}
		for _, pos := range rr.pos {
			if pos >= 0x2000 || pos&1 != 0 || seen[pos] || r.done[pos] || r.has(pos) {
				continue
			}
			seen[pos] = true
			if from == nil {
				from = p
			}
			list = append(list, pos)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
	return
}

// rupeesUpdate is a generated update routine deleting remote players' rupees, awaiting confirmation.
type rupeesUpdate struct {
	g         *Game
	room      uint16
	pos       []uint16
	from      *Player
	cancelled bool
}

func (u *rupeesUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	r := &g.rupees
	if u.cancelled || r.inflight != u {
		return
	}
	r.inflight = nil
	if value == crystalSwitchSkipped || r.room != u.room {
		return
	}
	if r.done == nil {
		r.done = map[uint16]bool{}
	}
	for _, p := range u.pos {
		r.done[p] = true
	}
	log.Printf("alttp: rupees: update: room $%03x: deleted rupees % 04x of player '%s'\n", u.room, u.pos, u.from.Name())
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *rupeesUpdate) AbortAsm() {
	r := &u.g.rupees
	if u.cancelled || r.inflight != u {
		return
	}
	// the routine checks the attributes, so trying again is harmless:
	r.inflight = nil
}

// generateRupeesUpdate generates an update routine deleting the rupees that remote players in the same room deleted.
// It has no side effects; the caller must set inflight if the routine is emitted.
func (g *Game) generateRupeesUpdate(newEmitter func() *asm.Emitter, index uint32) (u *rupeesUpdate, a *asm.Emitter) {
	r := &g.rupees
	local := g.local
	if !r.valid || r.inflight != nil || local.Module != 0x07 || local.SubModule != 0 {
		return
	}
	fn, ok := g.romFunctions[fnDeleteRupeeTile]
	if !ok {
		return
	}
	list, from := g.rupeesToDelete()
	if len(list) == 0 {
		return
	}
	if len(list) > rupeesPerFrame {
		list = list[:rupeesPerFrame]
	}

	u = &rupeesUpdate{g: g, room: r.room, pos: list, from: from}

	a = newEmitter()
	a.Comment(fmt.Sprintf("delete rupees % 04x in room $%03x from %s:", list, r.room, from.Name()))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("rp_skip")
	a.LDA_dp(0x11)
	a.BNE("rp_skip")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(r.room))
	a.BNE("rp_skip")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(r.room >> 8))
	a.BEQ("rp_apply")
	a.Label("rp_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.BRA("rp_report")
	a.Label("rp_apply")
	for i, pos := range list {
		next := fmt.Sprintf("rp_next_%d", i)
		t := uint32(pos >> 1)
		x, y := uint16(t&0x3F)<<3, uint16(t>>6)<<3
		a.Comment(fmt.Sprintf("the rupee at $%04x (x $%03x, y $%03x), if still there and the stripe buffer has room:", pos, x, y))
		a.LDA_long(0x7F2000 + t)
		a.CMP_imm8_b(rupeeTileAttr)
		a.BNE(next)
		a.REP(0x20)
		a.LDA_abs(0x1000)
		a.CMP_imm16_w(rupeeStripeLimit)
		a.BCS("rp_full")
		a.LDA_imm16_w(y)
		a.STA_dp(0x00)
		a.LDA_imm16_w(x)
		a.STA_dp(0x02)
		a.SEP(0x30)
		a.JSL(fn)
		a.Label(next)
	}
	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("rp_report")
	a.Comment("the rest is tried again; those deleted already are skipped then:")
	a.Label("rp_full")
	a.SEP(0x30)
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("rp_report")
	a.STA_long(a.GetBase() + 0x02 + index)
	return
}
