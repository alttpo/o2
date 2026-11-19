package alttp

import (
	"fmt"
	"time"
)

// Frame pacing anomalies: every client follows every player's NMI frame counter as that player reports it in its run
// timing messages (0x84: nmiFrames and sampleAt, on the sender's server clock), and notes in the notification history
// when a player's game starts running slow, paused or fast, or its counter jumps, and when it's back to normal. Since
// every client checks the same values from the same messages (the local player's are checked as they are sent), all
// players see the same reports.

const (
	// the rate is measured over at least this much of the sender's samples:
	pacingWindow = 5 * time.Second
	// a gap without samples this long (e.g. the SNES disconnected) starts measuring afresh:
	pacingGap = 10 * time.Second
	// a jump between two samples of more frames than this much wall time allows:
	pacingJump = 2 * time.Second

	// the speed (frames over wall time, 1.0 is normal) outside of which an anomaly starts, and within which it ends:
	pacingSlowStart  = 0.95
	pacingFastStart  = 1.02
	pacingNormalLow  = 0.98
	pacingNormalHigh = 1.015
)

type pacingState uint8

const (
	pacingNormal pacingState = iota
	pacingSlow
	pacingFast
)

type pacingSample struct {
	nmi uint32
	at  time.Time
}

// framePacing follows a player's frame counter for frame pacing anomalies.
type framePacing struct {
	samples []pacingSample
	state   pacingState
	// the sample time the current anomaly started at:
	since time.Time
}

// reset forgets the samples and any anomaly, without a note.
func (f *framePacing) reset() {
	f.samples = f.samples[:0]
	f.state = pacingNormal
	f.since = time.Time{}
}

// observe takes a player's counter sample from its message 0x84, and returns the notes to add to the notification
// history about it, without the player's name: each note follows the name.
func (f *framePacing) observe(nmi uint32, at time.Time, flags uint8) (notes []string) {
	if at.IsZero() || flags&rtFlagNoCounter != 0 {
		return
	}
	// the counter didn't advance since the last sample:
	stalled := false
	if n := len(f.samples); n > 0 {
		last := f.samples[n-1]
		if !at.After(last.at) {
			// no new sample:
			return
		}
		wall := at.Sub(last.at)
		frames := u24Diff(last.nmi, nmi)
		if wall > pacingGap {
			// not sampled for a while; its counter is followed afresh:
			f.samples = f.samples[:0]
		} else if frames < 0 {
			notes = append(notes, fmt.Sprintf("frame counter went back %d frames (%.1fs; savestate loaded?)",
				-frames, framesToDuration(int64(-frames)).Seconds()))
			f.samples = f.samples[:0]
		} else if ahead := framesToDuration(int64(frames)) - wall; ahead > pacingJump {
			notes = append(notes, fmt.Sprintf("frame counter jumped ahead %.1fs (fast-forward or savestate loaded?)",
				ahead.Seconds()))
			f.samples = f.samples[:0]
		} else {
			stalled = frames == 0
		}
	}
	f.samples = append(f.samples, pacingSample{nmi, at})

	// measure from the newest sample at least pacingWindow old:
	first := -1
	for i := len(f.samples) - 1; i >= 0; i-- {
		if at.Sub(f.samples[i].at) >= pacingWindow {
			first = i
			break
		}
	}
	if first < 0 {
		return
	}
	f.samples = append(f.samples[:0], f.samples[first:]...)
	from := f.samples[0]
	wall := at.Sub(from.at)
	frames := u24Diff(from.nmi, nmi)
	speed := float64(framesToDuration(int64(frames))) / float64(wall)

	kind := pacingNormal
	switch {
	case speed < pacingSlowStart:
		kind = pacingSlow
	case speed > pacingFastStart:
		kind = pacingFast
	case f.state != pacingNormal && (speed < pacingNormalLow || speed > pacingNormalHigh):
		// not yet back within the normal range:
		kind = f.state
	}
	if kind == f.state {
		return
	}
	if f.state != pacingNormal {
		notes = append(notes, fmt.Sprintf("frame pacing back to normal after %.1fs", at.Sub(f.since).Seconds()))
	}
	f.state = kind
	switch kind {
	case pacingSlow:
		f.since = at
		if stalled {
			notes = append(notes, "frame pacing anomaly: paused, no new frames")
		} else {
			notes = append(notes, fmt.Sprintf("frame pacing anomaly: running slow at %.0f%% speed", speed*100))
		}
	case pacingFast:
		f.since = at
		notes = append(notes, fmt.Sprintf("frame pacing anomaly: running fast at %.0f%% speed", speed*100))
	}
	return
}

// observePacing checks a player's message 0x84 for frame pacing anomalies and notes them in the notification history.
func (g *Game) observePacing(p *Player, m *runTimingRemote) {
	notes := p.Pacing.observe(m.NMIFrames, m.SampleAt, m.Flags)
	if len(notes) == 0 {
		return
	}
	name := p.Name()
	if name == "" {
		name = fmt.Sprintf("player #%02x", p.Index())
	}
	for _, n := range notes {
		g.PushNotification(name + ": " + n)
	}
}
