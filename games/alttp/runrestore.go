package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
)

// Run state across disconnects (RUN_TIMING.md, Disconnects).
//
// A player whose SNES or server connection dropped for a while keeps its start (and finish) time: the players that
// saw them keep what they saw, and give it back when the player reports it hasn't started (it lost its state, e.g.
// because it was restarted) in message 0x85. Every player that lost a connection announces how long it was gone in
// message 0x86, and every client notes that in its notification history so that the gap can be verified.

const (
	// the o2-only message types are in serde.go
	runRestoreVersion = 1
	// the bytes after the size byte: target u16, name [20], resetID u64, resetInitiator u16, flags u8, startAt i64,
	// finishAt i64, startFrame u24, finishFrame u24, lagFrames u24, penaltyMs u32
	runRestoreSize = 2 + 20 + 8 + 2 + 1 + 8 + 8 + 3 + 3 + 3 + 4

	runGapVersion = 1
	// kind u8, lostAt i64, backAt i64
	runGapSize = 1 + 8 + 8

	// a player that still reports no start is sent its start again after this long:
	runRestoreResend = 2 * time.Second
	// how long a player that finds a start in its SNES it can't place waits for the others to give its state back:
	runRestoreGrace = 4 * time.Second
	// a gap is announced again every slowbeat for this long, as the packets may be lost:
	runGapRepeat = 5 * time.Second
	// gaps longer than this are not believed:
	runGapMax = 24 * time.Hour
)

// the kinds of connection that can be lost:
const (
	gapSNES   uint8 = 1
	gapServer uint8 = 2
)

func gapKindName(kind uint8) string {
	switch kind {
	case gapSNES:
		return "SNES"
	case gapServer:
		return "server"
	}
	return "connection"
}

// runGap is a stretch of time a player was disconnected from its SNES or the server, on the server's clock.
type runGap struct {
	kind   uint8
	lostAt time.Time
	backAt time.Time
}

// pendingGap is a gap of the local player to announce.
type pendingGap struct {
	runGap
	announced bool
	until     time.Time
}

// runAbsence tracks the local player's lost connections. The SNES's is set from other goroutines (the game loop only
// notices it later), so it has its own lock.
type runAbsence struct {
	// whether the SNES was ever connected, and when it was lost, zero while connected:
	hadSNES    bool
	snesLostAt time.Time
	// the same for the server, set on the game loop:
	hadServer    bool
	serverLostAt time.Time

	pending []pendingGap
}

// gapNow is the server time to stamp gaps with: the server's clock once known (it keeps its offset while we're
// disconnected), else ours.
func (g *Game) gapNow() time.Time {
	if _, ok := g.clock.offset(); ok || !g.lastServerTime.IsZero() {
		return g.ServerNow()
	}
	return time.Now()
}

// noteSNESQueue is told whether the game has a SNES queue, and notes when it lost one and got one back.
func (g *Game) noteSNESQueue(present bool) {
	g.absenceLock.Lock()
	defer g.absenceLock.Unlock()
	a := &g.absence
	if !present {
		if a.hadSNES && a.snesLostAt.IsZero() {
			a.snesLostAt = g.gapNow()
		}
		return
	}
	a.hadSNES = true
	if !a.snesLostAt.IsZero() {
		a.pending = append(a.pending, pendingGap{runGap: runGap{gapSNES, a.snesLostAt, g.gapNow()}})
		a.snesLostAt = time.Time{}
	}
}

// noteServerLost is called when the server connection closed. wasJoined tells whether we were in the group.
func (g *Game) noteServerLost(wasJoined bool) {
	g.absenceLock.Lock()
	defer g.absenceLock.Unlock()
	a := &g.absence
	if wasJoined {
		a.hadServer = true
	}
	if a.hadServer && a.serverLostAt.IsZero() {
		a.serverLostAt = g.gapNow()
	}
}

// noteServerBack is called when the server put us back in the group.
func (g *Game) noteServerBack() {
	g.absenceLock.Lock()
	defer g.absenceLock.Unlock()
	a := &g.absence
	a.hadServer = true
	if !a.serverLostAt.IsZero() {
		a.pending = append(a.pending, pendingGap{runGap: runGap{gapServer, a.serverLostAt, g.gapNow()}})
		a.serverLostAt = time.Time{}
	}
}

// announceGaps notes the local player's new gaps in the notification history, and sends them to the others for a while.
// Called on the game loop every slowbeat, when connected to the group.
func (g *Game) announceGaps(now time.Time) {
	g.absenceLock.Lock()
	pending := append([]pendingGap(nil), g.absence.pending...)
	g.absenceLock.Unlock()
	if len(pending) == 0 || g.IsResetting() || g.LocalPlayer().Index() < 0 {
		return
	}

	for i := range pending {
		pg := &pending[i]
		if !pg.announced {
			g.PushNotification(describeGap(g.LocalPlayer(), pg.runGap))
			log.Printf("alttp: %s\n", describeGap(g.LocalPlayer(), pg.runGap))
			pg.announced = true
			pg.until = now.Add(runGapRepeat)
		}
		o := g.makeO2BroadcastMessage()
		if err := SerializeRunGap(o, pg.runGap); err != nil {
			log.Printf("alttp: serialize run gap: %v\n", err)
			continue
		}
		g.send(o)
	}

	// keep the state of the announcements, and drop the ones that were sent for long enough:
	g.absenceLock.Lock()
	defer g.absenceLock.Unlock()
	kept := g.absence.pending[:0]
	for i, pg := range g.absence.pending {
		if i < len(pending) {
			pg = pending[i]
		}
		if pg.announced && !now.Before(pg.until) {
			continue
		}
		kept = append(kept, pg)
	}
	g.absence.pending = kept
}

func playerLabel(p *Player) string {
	if n := p.Name(); n != "" {
		return n
	}
	return fmt.Sprintf("player #%02x", uint8(p.Index()))
}

// describeGap is the note every client adds to its notification history for a gap.
func describeGap(p *Player, gap runGap) string {
	return fmt.Sprintf("%s: %s connection lost for %s (%s to %s)",
		playerLabel(p), gapKindName(gap.kind), FormatDur(gap.lostAt, gap.backAt),
		FormatTime(gap.lostAt), FormatTime(gap.backAt))
}

// SerializeRunGap writes message 0x86.
func SerializeRunGap(w io.Writer, gap runGap) error {
	var b bytes.Buffer
	b.WriteByte(byte(MsgRunGap))
	b.WriteByte(runGapVersion)
	b.WriteByte(runGapSize)
	b.WriteByte(gap.kind)
	_ = binary.Write(&b, binary.LittleEndian, unixNanoOrZero(gap.lostAt))
	_ = binary.Write(&b, binary.LittleEndian, unixNanoOrZero(gap.backAt))
	_, err := w.Write(b.Bytes())
	return err
}

// gapKey identifies a gap of a player, to note it once.
type gapKey struct {
	kind   uint8
	lostAt int64
}

// DeserializeRunGap reads message 0x86: a player tells for how long it was disconnected.
func (g *Game) DeserializeRunGap(p *Player, r io.Reader) (err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	body := make([]byte, hdr[1])
	if _, err = io.ReadFull(r, body); err != nil {
		return
	}
	if hdr[0] != runGapVersion || len(body) < runGapSize || p == g.local {
		return nil
	}
	gap := runGap{
		kind:   body[0],
		lostAt: timeOrZero(int64(binary.LittleEndian.Uint64(body[1:]))),
		backAt: timeOrZero(int64(binary.LittleEndian.Uint64(body[9:]))),
	}
	if gap.lostAt.IsZero() || gap.backAt.Before(gap.lostAt) || gap.backAt.Sub(gap.lostAt) > runGapMax {
		return nil
	}
	key := gapKey{gap.kind, gap.lostAt.UnixNano()}
	for _, k := range p.gapsSeen {
		if k == key {
			return nil
		}
	}
	p.gapsSeen = append(p.gapsSeen, key)
	if len(p.gapsSeen) > 16 {
		p.gapsSeen = p.gapsSeen[len(p.gapsSeen)-16:]
	}
	g.PushNotification(describeGap(p, gap))
	return nil
}

// keepRunAcrossAbsence keeps what was seen of a player's run when its message 0x84 reports less, which happens when the
// player lost its state while away (e.g. it was restarted). old is the player's run timing before the message, m after
// it. It asks the player to take its start back with message 0x85. A different player in the slot is not given the
// run of the one before it.
func (g *Game) keepRunAcrossAbsence(p *Player, old, m *runTimingRemote, startAt, finishAt *time.Time) {
	lostStart := startAt.IsZero() && !old.StartAt.IsZero()
	lostFinish := finishAt.IsZero() && !old.FinishAt.IsZero()
	if !lostStart && !lostFinish {
		return
	}
	if p.runName != "" && p.NameF != p.runName {
		// someone else took the slot:
		p.runName = ""
		return
	}
	if lostStart {
		*startAt = old.StartAt
		m.StartFrame = old.StartFrame
	}
	if lostFinish {
		*finishAt = old.FinishAt
		m.FinishFrame = old.FinishFrame
	}
	m.LagFrames, m.Penalty = old.LagFrames, old.Penalty
	m.Flags |= old.Flags & (rtFlagStarted | rtFlagFinished | rtFlagDiscontinuity | rtFlagRate | rtFlagStateLoad)
	g.requestRunRestore(p, m, *startAt, *finishAt)
}

// requestRunRestore sends the player its run in message 0x85, at most once in a while.
func (g *Game) requestRunRestore(p *Player, m *runTimingRemote, startAt, finishAt time.Time) {
	now := time.Now()
	if now.Sub(p.restoreSent) < runRestoreResend || g.IsResetting() || g.LocalPlayer().Index() < 0 || p.Index() < 0 {
		return
	}
	p.restoreSent = now
	o := g.makeO2BroadcastMessage()
	if err := SerializeRunRestore(o, p, g.reset.lastCompleted, m, startAt, finishAt); err != nil {
		log.Printf("alttp: serialize run restore: %v\n", err)
		return
	}
	g.send(o)
	log.Printf("alttp: run timing: %s reports no start; sending it back (start %s)\n", playerLabel(p), FormatTime(startAt))
}

// SerializeRunRestore writes message 0x85: the run of the player p, for p to take back.
func SerializeRunRestore(w io.Writer, p *Player, key resetKey, m *runTimingRemote, startAt, finishAt time.Time) error {
	var b bytes.Buffer
	b.WriteByte(byte(MsgRunRestore))
	b.WriteByte(runRestoreVersion)
	b.WriteByte(runRestoreSize)
	_ = binary.Write(&b, binary.LittleEndian, uint16(p.Index()))
	var name [20]byte
	n := copy(name[:], p.Name())
	for ; n < 20; n++ {
		name[n] = ' '
	}
	b.Write(name[:])
	_ = binary.Write(&b, binary.LittleEndian, key.id)
	_ = binary.Write(&b, binary.LittleEndian, key.initiator)
	b.WriteByte(m.Flags & (rtFlagStarted | rtFlagFinished | rtFlagDiscontinuity | rtFlagRate | rtFlagStateLoad))
	_ = binary.Write(&b, binary.LittleEndian, unixNanoOrZero(startAt))
	_ = binary.Write(&b, binary.LittleEndian, unixNanoOrZero(finishAt))
	putU24(&b, m.StartFrame)
	putU24(&b, m.FinishFrame)
	putU24(&b, m.LagFrames)
	ms := m.Penalty.Milliseconds()
	if ms < 0 {
		ms = 0
	} else if ms > 0xFFFFFFFF {
		ms = 0xFFFFFFFF
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(ms))
	_, err := w.Write(b.Bytes())
	return err
}

// DeserializeRunRestore reads message 0x85. The local player takes the run in it back if it is for the local player (its
// index and name), of the group reset the local player is in, and the local player has no start.
func (g *Game) DeserializeRunRestore(p *Player, r io.Reader) (err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	body := make([]byte, hdr[1])
	if _, err = io.ReadFull(r, body); err != nil {
		return
	}
	if hdr[0] != runRestoreVersion || len(body) < runRestoreSize || p == g.local {
		return nil
	}
	local := g.LocalPlayer()
	if int(binary.LittleEndian.Uint16(body)) != local.Index() {
		return nil
	}
	if name := strings.Trim(string(body[2:22]), " \t\n\r\000"); name != local.Name() {
		return nil
	}
	key := resetKey{id: binary.LittleEndian.Uint64(body[22:]), initiator: binary.LittleEndian.Uint16(body[30:])}
	if own := g.reset.lastCompleted; !own.isZero() && !key.isZero() && key != own {
		return nil
	}
	m := &runTimingRemote{
		Flags:    body[32],
		StartAt:  timeOrZero(int64(binary.LittleEndian.Uint64(body[33:]))),
		FinishAt: timeOrZero(int64(binary.LittleEndian.Uint64(body[41:]))),
	}
	m.StartFrame, m.FinishFrame, m.LagFrames = u24(body[49:]), u24(body[52:]), u24(body[55:])
	m.Penalty = time.Duration(binary.LittleEndian.Uint32(body[58:])) * time.Millisecond
	g.adoptRestoredRun(p, m)
	return nil
}

// adoptRestoredRun takes the start (and finish) another player saw back, if the local player has none. The patch's
// latches in SRAM follow by themselves: a start that the SRAM doesn't hold is written back (see readRunTimingResponse).
func (g *Game) adoptRestoredRun(from *Player, m *runTimingRemote) {
	g.stateLock.Lock()
	defer g.stateLock.Unlock()

	r := &g.runTiming
	if r.started || m.StartAt.IsZero() || g.IsResetting() {
		return
	}
	now := g.gapNow()
	if m.StartAt.After(now.Add(rtJumpTolerance)) || (!m.FinishAt.IsZero() && m.FinishAt.Before(m.StartAt)) {
		log.Printf("alttp: run timing: ignoring the run %s sent back: start %s finish %s\n", playerLabel(from), FormatTime(m.StartAt), FormatTime(m.FinishAt))
		return
	}

	r.started, r.startFrame, r.startAt = true, m.StartFrame, m.StartAt
	r.flags |= rtFlagStarted | m.Flags&(rtFlagDiscontinuity|rtFlagRate|rtFlagStateLoad)
	r.penalty = m.Penalty
	r.lagAcc, r.lagFrames = m.LagFrames, m.LagFrames
	r.lagBase = 0
	if n := len(r.samples); n > 0 {
		s := r.samples[n-1]
		r.lagBase = (s.nmi - s.loop) & 0xFFFFFF
	}
	r.gatesAtStart, r.gatesChanged = r.gates, false
	r.restoreWaitSince = time.Time{}
	r.latchRawOK = false

	local := g.local
	local.GameStartTime = m.StartAt
	if !m.FinishAt.IsZero() {
		r.finished, r.finishFrame, r.finishAt = true, m.FinishFrame, m.FinishAt
		r.flags |= rtFlagFinished
		local.GameFinishTime = m.FinishAt
	}
	g.shouldUpdatePlayersList = true
	g.sendRunTiming()
	g.PushNotification(fmt.Sprintf("run timing restored from %s: started at %s", playerLabel(from), FormatTime(m.StartAt)))
	log.Printf("alttp: run timing: restored from %s: start %s finish %s lag %d\n", playerLabel(from), FormatTime(m.StartAt), FormatTime(m.FinishAt), m.LagFrames)
}

// awaitRunRestore reports whether to wait a little before clearing a start that is in the SNES but that this client
// can't place: the others may know when it was. It waits only when there are others to ask.
func (g *Game) awaitRunRestore(at time.Time) bool {
	r := &g.runTiming
	if g.LocalPlayer().Index() < 0 || len(g.RemotePlayers()) == 0 {
		return false
	}
	if r.restoreWaitSince.IsZero() {
		r.restoreWaitSince = at
		log.Printf("alttp: run timing: found a start in the SNES that wasn't followed; waiting for the others to give the run back\n")
	}
	if at.Sub(r.restoreWaitSince) < runRestoreGrace {
		return true
	}
	r.restoreWaitSince = time.Time{}
	return false
}
