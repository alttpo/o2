package alttp

import (
	"strings"
	"testing"
	"time"
)

// pacingSim sends a player's counter samples every 500ms, as its 0x84 messages carry them, with up to a frame of
// jitter on the sample times.
type pacingSim struct {
	t     *testing.T
	f     framePacing
	at    time.Time
	nmi   float64
	tick  int
	notes []string
}

func newPacingSim(t *testing.T) *pacingSim {
	return &pacingSim{t: t, at: time.Unix(1_700_000_000, 0), nmi: 0xFFFF00}
}

// run plays d of wall time at the given speed (1.0 is normal), and returns the notes it made.
func (s *pacingSim) run(d time.Duration, speed float64) []string {
	var notes []string
	for end := s.at.Add(d); s.at.Before(end); {
		s.at = s.at.Add(500 * time.Millisecond)
		s.nmi += speed * 0.5 * snesFPS
		s.tick++
		jitter := time.Duration(s.tick%3-1) * snesFramePeriod
		notes = append(notes, s.f.observe(uint32(s.nmi)&0xFFFFFF, s.at.Add(jitter), 0)...)
	}
	s.notes = append(s.notes, notes...)
	return notes
}

func expectNotes(t *testing.T, what string, notes []string, want ...string) {
	t.Helper()
	if len(notes) != len(want) {
		t.Fatalf("%s: notes %q; expected %d containing %q", what, notes, len(want), want)
	}
	for i, w := range want {
		if !strings.Contains(notes[i], w) {
			t.Fatalf("%s: note %q; expected it to contain %q", what, notes[i], w)
		}
	}
}

func TestPacing_NormalIsQuiet(t *testing.T) {
	s := newPacingSim(t)
	expectNotes(t, "normal", s.run(5*time.Minute, 1.0))
	// a display refresh of 60Hz instead of 60.0988Hz:
	expectNotes(t, "60Hz", s.run(5*time.Minute, 60/snesFPS))
}

func TestPacing_SlowThenNormal(t *testing.T) {
	s := newPacingSim(t)
	s.run(time.Minute, 1.0)
	expectNotes(t, "slow", s.run(30*time.Second, 0.8), "running slow at")
	expectNotes(t, "recovered", s.run(30*time.Second, 1.0), "back to normal after")
	expectNotes(t, "normal", s.run(time.Minute, 1.0))
}

func TestPacing_PausedThenNormal(t *testing.T) {
	s := newPacingSim(t)
	s.run(time.Minute, 1.0)
	expectNotes(t, "paused", s.run(30*time.Second, 0), "paused")
	expectNotes(t, "resumed", s.run(30*time.Second, 1.0), "back to normal after")
}

func TestPacing_FastThenNormal(t *testing.T) {
	s := newPacingSim(t)
	s.run(time.Minute, 1.0)
	expectNotes(t, "fast", s.run(30*time.Second, 1.1), "running fast at")
	expectNotes(t, "recovered", s.run(30*time.Second, 1.0), "back to normal after")
}

func TestPacing_Jumps(t *testing.T) {
	s := newPacingSim(t)
	s.run(time.Minute, 1.0)
	s.nmi -= 600
	expectNotes(t, "rewound", s.run(time.Minute, 1.0), "went back")
	s.nmi += 600
	expectNotes(t, "ahead", s.run(time.Minute, 1.0), "jumped ahead")
}

func TestPacing_GapIsQuiet(t *testing.T) {
	s := newPacingSim(t)
	s.run(time.Minute, 1.0)
	// no samples for a minute (the SNES disconnected), with the counter still running:
	s.at = s.at.Add(time.Minute)
	s.nmi += 60 * snesFPS
	expectNotes(t, "gap", s.run(time.Minute, 1.0))
}
