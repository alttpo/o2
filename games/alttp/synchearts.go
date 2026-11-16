package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/rand"
	"sort"
	"strings"

	"github.com/alttpo/snes/asm"
)

// Heart container and heart piece synchronization
//
// Hearts used to be synced like other items by taking the highest value of $7EF36C (health capacity) and $7EF36B
// (heart pieces). Two players collecting different heart pieces at about the same time then ended up with only one of
// them: both went from n to n+1 and the highest value was n+1. Hearts are instead synced like small keys (see
// syncsmallkeys.go), as per-player counters of *changes*, and shared with alttpo's Hearts.as in message 0x83:
//
//   - The count is the total number of heart pieces, `($7EF36C & $F8) / 2 + ($7EF36B & 3)`, a heart container counting
//     as four pieces. It is at most 20 hearts and 3 pieces, 83.
//   - Each player tracks `net`: the cumulative number of pieces the local player collected during this sync session,
//     not counting sync writes, and broadcasts (session, net, value) every frame.
//   - When a receiver sees a remote player's net go up, it adds the difference to its own count with a routine that
//     executes within a single game frame and reports the count it saw, so it never clobbers a local change and never
//     mistakes its own write for one. The first message from a remote session only establishes a baseline.
//   - A player with no history (fresh session, game reloaded) catches up once by adopting the highest count of the
//     remote players on the same team.
//
// Hearts only go up, except for a moment when the 4th piece is collected: the game sets $7EF36B to 0 right away but
// only adds a heart to $7EF36C once Link stops holding it up. The local count is therefore tracked as a high-water
// mark: decreases are ignored and only counts above it are taken as local changes, so the piece is counted once, when
// the heart container is added. Sync writes made meanwhile raise the high-water mark by the amount they added.

// heartPiecesMax is the highest heart piece count: 20 hearts and 3 pieces.
const heartPiecesMax = 20*4 + 3

// heartPiecesUnknown is sent as the value of a heart count not yet read from the SNES.
const heartPiecesUnknown = 0xFF

// heartsLocal tracks the local player's heart state.
type heartsLocal struct {
	// session uniquely identifies this player's sync session; changes on reset so remote baselines are re-established:
	session uint32

	// valid is true when observed holds a value read from the SNES while in a stable game state:
	valid bool
	// observed is the high-water mark of the heart piece count, accounting for all local changes and sync writes:
	observed uint8
	// net is the number of heart pieces the local player collected during this session:
	net int16
	// caughtUp is true once we adopted the group's heart count (or nobody had a higher one):
	caughtUp bool

	// pending is the number of heart pieces collected by remote players not yet added to our SNES:
	pending int16
	// pendingFrom names the players pending was collected by, for the notification:
	pendingFrom []string
	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *heartsUpdate
}

// heartsRemote is the last heart state received from a remote player.
type heartsRemote struct {
	valid   bool
	session uint32
	net     int16
	value   uint8

	// applied is the remote player's net already accounted for (applied to our SNES or taken as a baseline):
	applied int16
}

// heartPieces returns the total number of heart pieces for the health capacity $7EF36C and heart pieces $7EF36B.
func heartPieces(capacity, pieces uint8) uint8 {
	return (capacity&0xF8)>>1 + pieces&3
}

// describeHeartPieces describes a number of heart pieces in hearts and pieces, e.g. "1 new heart, 2 new heart pieces".
func describeHeartPieces(n int) string {
	full, pieces := n>>2, n&3
	hc := &strings.Builder{}
	if full == 1 {
		hc.WriteString("1 new heart")
	} else if full > 1 {
		hc.WriteString(fmt.Sprintf("%d new hearts", full))
	}
	if full >= 1 && pieces >= 1 {
		hc.WriteString(", ")
	}
	if pieces == 1 {
		hc.WriteString("1 new heart piece")
	} else if pieces > 1 {
		hc.WriteString(fmt.Sprintf("%d new heart pieces", pieces))
	}
	return hc.String()
}

// initHeartsSync called from Reset(); starts a new heart sync session.
func (g *Game) initHeartsSync() {
	h := &g.hearts
	if h.inflight != nil {
		h.inflight.cancelled = true
	}

	*h = heartsLocal{}
	for h.session == 0 {
		h.session = rand.Uint32()
	}
}

// invalidateHearts forgets the observed heart count so it is re-read from the SNES without counting any difference as
// a local change. When adopt is true, also catch up to the group's heart count again.
func (g *Game) invalidateHearts(adopt bool) {
	h := &g.hearts
	h.valid = false
	if h.inflight != nil {
		h.inflight.cancelled = true
		h.inflight = nil
	}
	if adopt {
		h.pending = 0
		h.pendingFrom = nil
		h.caughtUp = false
	}
}

// readHearts samples the heart count from WRAM and records any heart pieces collected by the local player.
// Called when WRAM is read from the SNES.
func (g *Game) readHearts() {
	local := g.local
	h := &g.hearts

	// only sample during stable game states:
	if !(local.Module == 0x07 || local.Module == 0x09 || local.Module == 0x0B) || local.SubModule != 0 {
		return
	}
	if !g.wramFresh[0xF36B] || !g.wramFresh[0xF36C] || !g.wramFresh[0x02E4] {
		return
	}
	if g.wram[0x02E4] != 0 {
		// Link is frozen, e.g. holding up an item; a heart container may be about to be added:
		return
	}
	if h.inflight != nil {
		// wait for the update to confirm; it accounts for any local changes made in the meantime:
		return
	}

	v := heartPieces(g.wram[0xF36C], g.wram[0xF36B])
	if !h.valid {
		// first observation; nothing to attribute:
		h.valid = true
		h.observed = v
		return
	}

	if v <= h.observed {
		// no change, or the 4th piece was collected and the heart container is not added yet:
		return
	}

	g.recordLocalHeartsChange(int(v) - int(h.observed))
	h.observed = v
}

func (g *Game) recordLocalHeartsChange(delta int) {
	if delta <= 0 {
		return
	}
	h := &g.hearts
	h.net += int16(delta)
	log.Printf("alttp: hearts: local: %+d heart pieces (net %+d)\n", delta, h.net)
}

// adoptHearts makes a player with no heart history catch up to the group's heart count. This only happens once per
// session (or after the game is reloaded) so that it never discards deltas during normal play.
func (g *Game) adoptHearts() {
	h := &g.hearts
	if !h.valid || h.caughtUp || h.inflight != nil {
		return
	}

	var highest *Player
	for _, p := range g.RemotePlayers() {
		if p.Team != g.local.Team {
			continue
		}
		rh := &p.Hearts
		if !rh.valid || rh.value == heartPiecesUnknown {
			continue
		}
		if highest == nil || rh.value > highest.Hearts.value {
			highest = p
		}
	}
	if highest == nil {
		// no remote players with known heart counts yet:
		return
	}

	h.caughtUp = true
	value := highest.Hearts.value
	if value <= h.observed {
		return
	}

	// replace any pending changes since the adopted value already includes them:
	h.pending = int16(value) - int16(h.observed)
	h.pendingFrom = []string{highest.Name()}
	log.Printf("alttp: hearts: catch up to %d heart pieces from player '%s'\n", value, highest.Name())
}

// heartsUpdate is a generated update routine adding heart pieces, awaiting execution confirmation.
type heartsUpdate struct {
	g         *Game
	delta     int16
	from      []string
	cancelled bool
}

func (u *heartsUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	h := &g.hearts
	if u.cancelled || h.inflight != u {
		return
	}
	h.inflight = nil

	// value is the heart piece count the routine saw right before adding to it. A count above the high-water mark was
	// collected by the local player in the meantime; one below it means the 4th piece was collected and its heart
	// container is not added yet, which the high-water mark already includes:
	before := value
	if h.valid && before > h.observed {
		g.recordLocalHeartsChange(int(before) - int(h.observed))
	}

	base := before
	if h.valid && h.observed > base {
		base = h.observed
	}
	after := int(base) + int(u.delta)
	if after > heartPiecesMax {
		after = heartPiecesMax
	}
	h.valid = true
	h.observed = uint8(after)

	log.Printf("alttp: hearts: update: %d %+d -> %d\n", before, u.delta, after)
	if len(u.from) > 0 {
		g.PushNotification(fmt.Sprintf("got %s from %s", describeHeartPieces(int(u.delta)), strings.Join(u.from, ", ")))
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *heartsUpdate) AbortAsm() {
	g := u.g
	h := &g.hearts
	if u.cancelled || h.inflight != u {
		return
	}
	h.inflight = nil

	// we cannot know whether the routine executed or not so re-read the heart count without attributing any change
	// to the local player and re-adopt the group's heart count:
	log.Printf("alttp: hearts: update aborted; re-syncing\n")
	h.valid = false
	h.pending = 0
	h.pendingFrom = nil
	h.caughtUp = false
}

// generateHeartsUpdate generates an update routine to add the heart pieces collected by remote players.
// It has no side effects; the caller must call begin() if the routine is emitted.
func (g *Game) generateHeartsUpdate(newEmitter func() *asm.Emitter, index uint32) (u *heartsUpdate, a *asm.Emitter) {
	h := &g.hearts
	if !h.valid || h.inflight != nil || h.pending <= 0 {
		return
	}

	delta := h.pending
	if delta > heartPiecesMax {
		delta = heartPiecesMax
	}

	u = &heartsUpdate{g: g, delta: delta, from: h.pendingFrom}

	a = newEmitter()
	report := a.GetBase() + 0x02 + index
	a.Comment(fmt.Sprintf("add %d heart pieces:", delta))
	a.Comment("heart pieces = ($7EF36C & $F8) / 2 | ($7EF36B & 3); report it for confirmation:")
	a.LDA_long(0x7EF36B)
	a.AND_imm8_b(0x03)
	a.STA_long(report)
	a.LDA_long(0x7EF36C)
	a.AND_imm8_b(0xF8)
	a.LSR()
	a.ORA_long(report)
	a.STA_long(report)
	a.CLC()
	a.ADC_imm8_b(uint8(delta))
	a.CMP_imm8_b(heartPiecesMax + 1)
	a.BCC("hearts_ok")
	a.LDA_imm8_b(heartPiecesMax)
	a.Label("hearts_ok")
	a.PHA()
	a.AND_imm8_b(0x03)
	a.STA_long(0x7EF36B)
	a.PLA()
	a.AND_imm8_b(0xFC)
	a.ASL()
	a.STA_long(0x7EF36C)

	return
}

// begin marks the update as in flight once its routine has been emitted.
func (u *heartsUpdate) begin() {
	h := &u.g.hearts
	h.inflight = u
	h.pending -= u.delta
	h.pendingFrom = nil
	log.Printf("alttp: hearts: update: %+d heart pieces\n", u.delta)
}

func (g *Game) SerializeHearts(w io.Writer) (err error) {
	h := &g.hearts
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgHearts)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, h.session); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, h.net); err != nil {
		return
	}
	value := h.observed
	if !h.valid {
		// don't let anyone adopt a value we haven't read yet:
		value = heartPiecesUnknown
	}
	if err = binary.Write(w, binary.LittleEndian, value); err != nil {
		return
	}
	return
}

func (g *Game) DeserializeHearts(p *Player, r io.Reader) (err error) {
	var session uint32
	var net int16
	var value uint8
	if err = binary.Read(r, binary.LittleEndian, &session); err != nil {
		return fmt.Errorf("error deserializing hearts: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &net); err != nil {
		return fmt.Errorf("error deserializing hearts: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &value); err != nil {
		return fmt.Errorf("error deserializing hearts: %w", err)
	}

	if p == g.local {
		return
	}

	rh := &p.Hearts
	if !rh.valid || rh.session != session {
		// first message from this session; only establish a baseline:
		*rh = heartsRemote{valid: true, session: session, applied: net}
		log.Printf("alttp: hearts: player '%s' session %08x baseline\n", p.Name(), session)
	} else if d := net - rh.applied; d != 0 {
		rh.applied = net
		if d > 0 && g.SyncHearts && p.Team == g.local.Team {
			h := &g.hearts
			h.pending += d
			name := p.Name()
			i := sort.SearchStrings(h.pendingFrom, name)
			if i >= len(h.pendingFrom) || h.pendingFrom[i] != name {
				h.pendingFrom = append(h.pendingFrom, "")
				copy(h.pendingFrom[i+1:], h.pendingFrom[i:])
				h.pendingFrom[i] = name
			}
			log.Printf("alttp: hearts: remote: player '%s' %+d heart pieces\n", name, d)
		}
	}

	rh.net = net
	rh.value = value
	return
}

// readOverworldFlags notices changes of the overworld area flags ($7EF280..$7EF33F) so they are sent right away instead
// of with the next periodic SRAM broadcast, e.g. so that other players in the area see a heart piece disappear as soon
// as it is collected (see removeCollectedHeartSprites). The flags of the current area are read every frame.
func (g *Game) readOverworldFlags() {
	for i := range g.owFlags {
		o := 0xF280 + i
		if !g.wramFresh[o] {
			continue
		}
		v := g.wram[o]
		if g.owFlagsKnown[i] && g.owFlags[i] != v {
			g.owFlagsSend = true
		}
		g.owFlags[i], g.owFlagsKnown[i] = v, true
	}
}
