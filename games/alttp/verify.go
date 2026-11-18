package alttp

import (
	"fmt"
	"strings"
	"time"
)

// Peer verification of the run timing (RUN_TIMING.md, Verification). Every client checks the other players' 0x84
// messages against the server's stamps on them, which the sender can't forge and which are always later than what the
// message reports. A player who fails a check is shown as unverified, with the reasons; the reported times are never
// changed, so that every client shows the same run time from the same messages.

const (
	// a start or finish must be reported within this long of when it happened:
	rvReportWithin = 2 * time.Second
	// what the sender's server clock estimate may be off by:
	rvClockSlack = 100 * time.Millisecond
	// the messages whose lowest offset is taken as the current one, so that a single late message doesn't move it:
	rvOffsetWindow = 10
	// an offset this much above the reference, over the whole window, means the counter was paused or rewound:
	rvOffsetRise = 2 * time.Second
	// running fast is allowed this much (as in o2's own rate check), plus rvGainSlack:
	rvRateTolerance = rtRateTolerance
	rvGainSlack     = 2 * time.Second
)

// runTimingVerify is what a client keeps about another player's run timing to verify it.
type runTimingVerify struct {
	// messages without a start, and with a start but without a finish, were received: only then does the server stamp
	// of the first message that reports a start (or finish) bound when it happened:
	sawUnstarted  bool
	sawUnfinished bool

	// the lowest server stamp minus sampleAt of its messages, which bounds its one-way delay:
	minDelay   time.Duration
	minDelayOK bool

	// the rate: the offset of a message is its server stamp minus its NMI frames at the nominal frame period. It never
	// goes below the true offset (messages arrive after their sample), and only goes down if frames ran faster than
	// the wall time:
	rateOK       bool
	lastNMI      uint32
	lastSampleAt time.Time
	frames       int64
	ref          int64
	recent       []int64
	firstStamp   time.Time
	lastStamp    time.Time
	gain         time.Duration

	// the server stamp of the last pre-game to in-game module change in its location messages before it reported a
	// start, and of the first location in module $19 after the start:
	lastModule  Module
	moduleOK    bool
	inGameAt    time.Time
	triforceAt  time.Time
	startFrozen bool

	// the reasons last shown, to update the players list when they change:
	lastReasons string
}

// preGameModule reports whether m comes before loading a file: the title, file select and attract mode modules.
func preGameModule(m Module) bool {
	return m < runStartModule || m == 0x14
}

// observeLocation records the module changes of a player's location message.
func (v *runTimingVerify) observeLocation(p *Player) {
	stamp := p.lastServerStamp
	if stamp.IsZero() {
		return
	}
	m := p.Module
	if !v.startFrozen && v.moduleOK && preGameModule(v.lastModule) && !preGameModule(m) {
		v.inGameAt = stamp
	}
	if m == runFinishModule && v.startFrozen && v.triforceAt.IsZero() {
		v.triforceAt = stamp
	}
	v.lastModule, v.moduleOK = m, true
}

// observeRunTiming records a player's 0x84 message, already deserialized into p.RunTiming.
func (v *runTimingVerify) observeRunTiming(p *Player) {
	m := &p.RunTiming
	stamp := p.lastServerStamp
	if stamp.IsZero() {
		return
	}
	if m.StartAt.IsZero() {
		v.sawUnstarted = true
		v.startFrozen = false
		v.triforceAt = time.Time{}
		v.rateOK = false
		v.gain = 0
	} else {
		v.startFrozen = true
		if m.FinishAt.IsZero() {
			v.sawUnfinished = true
		}
	}

	if !m.SampleAt.IsZero() {
		if d := stamp.Sub(m.SampleAt); !v.minDelayOK || d < v.minDelay {
			v.minDelay, v.minDelayOK = d, true
		}
	}

	// the rate during the run, from messages with a new sample:
	if m.StartAt.IsZero() || m.SampleAt.IsZero() || m.SampleAt.Equal(v.lastSampleAt) {
		return
	}
	finishing := !m.FinishAt.IsZero()
	if finishing && v.lastStamp.After(m.FinishAt) {
		// the run's rate was already checked up to the finish:
		return
	}
	v.lastSampleAt = m.SampleAt
	if !v.rateOK {
		v.rateOK = true
		v.lastNMI, v.frames = m.NMIFrames, 0
		v.ref = stamp.UnixNano()
		v.recent = append(v.recent[:0], v.ref)
		v.firstStamp, v.lastStamp = stamp, stamp
		return
	}
	v.frames += int64(u24Diff(v.lastNMI, m.NMIFrames))
	v.lastNMI = m.NMIFrames
	v.lastStamp = stamp
	o := stamp.UnixNano() - v.frames*int64(snesFramePeriod)
	if o < v.ref {
		// frames ran ahead of the wall time:
		v.gain += time.Duration(v.ref - o)
		v.ref = o
	}
	v.recent = append(v.recent, o)
	if len(v.recent) > rvOffsetWindow {
		v.recent = v.recent[len(v.recent)-rvOffsetWindow:]
	}
	if len(v.recent) == rvOffsetWindow {
		low := v.recent[0]
		for _, r := range v.recent[1:] {
			low = min(low, r)
		}
		if low > v.ref+int64(rvOffsetRise) {
			// paused, slowed down or rewound: the frames from here on are compared with the new offset:
			v.ref = low
		}
	}
}

// verifyReasons checks a player's run timing, and returns why it is unverified, or nothing.
func (v *runTimingVerify) verifyReasons(m *runTimingRemote) (reasons []string) {
	if m.GatesChanged {
		reasons = append(reasons, "gates changed during the run")
	}

	maxDelay := func(at time.Time) time.Time {
		d := v.minDelay
		if d < 0 {
			d = 0
		}
		return at.Add(-d).Add(rvClockSlack)
	}
	check := func(what string, at, seenAt time.Time, sawBefore bool, moduleAt time.Time) {
		if at.IsZero() {
			return
		}
		if sawBefore && !seenAt.IsZero() {
			if late := seenAt.Sub(at); late > rvReportWithin {
				reasons = append(reasons, fmt.Sprintf("%s reported %.1fs after it", what, late.Seconds()))
			}
			if v.minDelayOK && at.After(maxDelay(seenAt)) {
				reasons = append(reasons, fmt.Sprintf("%s is later than its message could have been sent", what))
			}
		}
		if !moduleAt.IsZero() && at.After(moduleAt.Add(rvClockSlack)) {
			reasons = append(reasons, fmt.Sprintf("%s is %.1fs after its location showed it", what, at.Sub(moduleAt).Seconds()))
		}
	}
	inGameAt := v.inGameAt
	if !v.startFrozen {
		inGameAt = time.Time{}
	}
	check("start", m.StartAt, m.StartSeenAt, v.sawUnstarted, inGameAt)
	check("finish", m.FinishAt, m.FinishSeenAt, v.sawUnfinished, v.triforceAt)

	if v.rateOK {
		allowed := m.Penalty + rvGainSlack + time.Duration(float64(v.lastStamp.Sub(v.firstStamp))*rvRateTolerance)
		if v.gain > allowed {
			reasons = append(reasons, fmt.Sprintf("ran %.1fs ahead of the wall time", v.gain.Seconds()))
		}
	}
	return
}

// runCost is what makes players pay the same per frame: the patch and the gates (RUN_TIMING.md, Equal cost).
type runCost struct {
	signature patchSignature
	gates     uint8
}

// commonRunCost returns the cost that the most players run with, preferring the local player's on a tie, and whether
// any started player runs with another.
func (g *Game) commonRunCost(players []*Player) (common runCost, differ bool) {
	counts := make(map[runCost]int)
	var local runCost
	haveLocal := false
	for _, p := range players {
		rt := g.playerRunTiming(p)
		if !rt.OK {
			continue
		}
		c := runCost{rt.Signature, rt.Gates}
		counts[c]++
		if p == g.local {
			local, haveLocal = c, true
		}
	}
	best := -1
	for c, n := range counts {
		if n > best || (n == best && haveLocal && c == local) {
			common, best = c, n
		}
	}
	return common, len(counts) > 1
}

// verifyPlayer returns why a player's run timing is unverified, or nothing.
func (g *Game) verifyPlayer(p *Player, common runCost) []string {
	rt := g.playerRunTiming(p)
	if !rt.OK {
		return nil
	}
	var reasons []string
	if rt.Signature != common.signature {
		reasons = append(reasons, "runs a different patched ROM")
	}
	if rt.Gates != common.gates {
		reasons = append(reasons, fmt.Sprintf("runs with gates $%02x instead of $%02x", rt.Gates, common.gates))
	}
	if p == g.local {
		// the server's stamps are only on the messages of the other players:
		if rt.GatesChanged {
			reasons = append(reasons, "gates changed during the run")
		}
		return reasons
	}
	return append(reasons, p.RunVerify.verifyReasons(rt)...)
}

// timingFlagNames describes the run timing flags that mark a player's time.
func timingFlagNames(flags uint8) string {
	var s []string
	if flags&rtFlagDiscontinuity != 0 {
		s = append(s, "frame counter discontinuity")
	}
	if flags&rtFlagRate != 0 {
		s = append(s, "frame rate out of tolerance")
	}
	if flags&rtFlagStateLoad != 0 {
		s = append(s, "savestate loaded")
	}
	if flags&rtFlagNoCounter != 0 {
		s = append(s, "no NMI frame counter in the ROM")
	}
	return strings.Join(s, ", ")
}
