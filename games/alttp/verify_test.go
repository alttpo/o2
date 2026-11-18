package alttp

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// rvSim simulates another player's 0x84 messages as the local client receives them.
type rvSim struct {
	t   *testing.T
	g   *Game
	p   *Player
	m   runTimingRemote
	t0  time.Time
	nmi uint32
}

func newRVSim(t *testing.T) *rvSim {
	s := &rvSim{t: t, g: &Game{}, p: &Player{}, t0: time.Unix(1_700_000_000, 0), nmi: 0xFFF000}
	s.m.OK = true
	copy(s.m.Signature[:], "O2-PATCH12345678")
	return s
}

// send delivers the message with the sample taken at sampleAt and stamped by the server delay later.
func (s *rvSim) send(sampleAt time.Time, delay time.Duration) {
	s.t.Helper()
	s.m.NMIFrames = s.nmi & 0xFFFFFF
	s.m.SampleAt = sampleAt
	var b bytes.Buffer
	if err := SerializeRunTiming(&b, &s.m); err != nil {
		s.t.Fatal(err)
	}
	s.p.lastServerStamp = sampleAt.Add(delay)
	if err := s.g.DeserializeRunTiming(s.p, bytes.NewReader(b.Bytes()[1:])); err != nil {
		s.t.Fatal(err)
	}
}

// play runs d of frames at the nominal rate times speed, sending a message every 500ms of wall time from at.
func (s *rvSim) play(at time.Time, d time.Duration, speed float64, delay time.Duration) time.Time {
	end := at.Add(d)
	for at.Before(end) {
		at = at.Add(500 * time.Millisecond)
		s.nmi += uint32(float64(durationToFrames(500*time.Millisecond)) * speed)
		s.send(at, delay)
	}
	return at
}

func (s *rvSim) location(at time.Time, m Module) {
	s.p.Module = m
	s.p.lastServerStamp = at
	s.p.RunVerify.observeLocation(s.p)
}

func (s *rvSim) reasons() []string {
	return s.p.RunVerify.verifyReasons(&s.p.RunTiming)
}

func (s *rvSim) expectVerified() {
	s.t.Helper()
	if r := s.reasons(); len(r) != 0 {
		s.t.Errorf("unverified: %v", r)
	}
}

func (s *rvSim) expectReason(substr string) {
	s.t.Helper()
	for _, r := range s.reasons() {
		if strings.Contains(r, substr) {
			return
		}
	}
	s.t.Errorf("reasons %v; expected one with %q", s.reasons(), substr)
}

// run plays an honest run: before the start, the start, an hour, the finish.
func (s *rvSim) run(delay time.Duration) time.Time {
	at := s.t0
	s.location(at, 0x01)
	at = s.play(at, 5*time.Second, 1, delay)
	// the start, latched 1 frame after the sample and reported with the next message:
	startAt := at.Add(snesFramePeriod)
	s.location(startAt.Add(delay+30*time.Millisecond), 0x07)
	s.m.StartAt = startAt
	s.m.Flags |= rtFlagStarted
	at = s.play(at, time.Hour, 1, delay)
	finishAt := at.Add(snesFramePeriod)
	s.location(finishAt.Add(delay+30*time.Millisecond), runFinishModule)
	s.m.FinishAt = finishAt
	s.m.Flags |= rtFlagFinished
	return s.play(at, 5*time.Second, 1, delay)
}

func TestVerify_Honest(t *testing.T) {
	s := newRVSim(t)
	s.run(40 * time.Millisecond)
	s.expectVerified()
}

func TestVerify_LateMessagesAndPauses(t *testing.T) {
	s := newRVSim(t)
	at := s.t0
	at = s.play(at, 5*time.Second, 1, 40*time.Millisecond)
	s.m.StartAt = at.Add(snesFramePeriod)
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	// a message held up for 3 seconds, then normal ones:
	at = s.play(at, 500*time.Millisecond, 1, 3*time.Second)
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	// paused for 30 seconds, still sending:
	at = s.play(at, 30*time.Second, 0, 40*time.Millisecond)
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	// a savestate rewound by a minute:
	s.nmi -= uint32(durationToFrames(time.Minute))
	s.play(at, time.Minute, 1, 40*time.Millisecond)
	s.expectVerified()
}

func TestVerify_BackdatedStart(t *testing.T) {
	s := newRVSim(t)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.m.StartAt = at.Add(-5 * time.Second)
	s.play(at, 5*time.Second, 1, 40*time.Millisecond)
	s.expectReason("start reported")
}

func TestVerify_ForwardDatedStart(t *testing.T) {
	s := newRVSim(t)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	// claims a start 30ms before its message arrives, though its messages take 40ms:
	s.m.StartAt = at.Add(500*time.Millisecond + 40*time.Millisecond - 30*time.Millisecond + rvClockSlack)
	s.play(at, 5*time.Second, 1, 40*time.Millisecond)
	s.expectReason("later than its message")
}

func TestVerify_LateJoinerIsNotBackdating(t *testing.T) {
	s := newRVSim(t)
	// the first message received already has a start from long ago:
	s.m.StartAt = s.t0.Add(-time.Hour)
	s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.expectVerified()
}

func TestVerify_StartAfterLocationShowedIt(t *testing.T) {
	s := newRVSim(t)
	s.location(s.t0, 0x01)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.location(at.Add(-2*time.Second), 0x07)
	s.m.StartAt = at
	s.play(at, 5*time.Second, 1, 40*time.Millisecond)
	s.expectReason("after its location showed it")
}

func TestVerify_FinishAfterTriforce(t *testing.T) {
	s := newRVSim(t)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.m.StartAt = at
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	s.location(at.Add(-time.Second), runFinishModule)
	s.m.FinishAt = at
	s.play(at, 5*time.Second, 1, 40*time.Millisecond)
	s.expectReason("finish is")
}

func TestVerify_FastForward(t *testing.T) {
	s := newRVSim(t)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.m.StartAt = at
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	// 30 seconds at double speed saves 30 seconds:
	at = s.play(at, 30*time.Second, 2, 40*time.Millisecond)
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	s.expectReason("ahead of the wall time")

	// unless the player charged itself for it:
	s.m.Penalty = 30 * time.Second
	s.play(at, time.Second, 1, 40*time.Millisecond)
	s.expectVerified()
}

func TestVerify_ForwardJump(t *testing.T) {
	s := newRVSim(t)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.m.StartAt = at
	at = s.play(at, time.Minute, 1, 40*time.Millisecond)
	s.nmi += uint32(durationToFrames(time.Minute))
	s.play(at, time.Minute, 1, 40*time.Millisecond)
	s.expectReason("ahead of the wall time")
}

func TestVerify_SlightlyFastIsTolerated(t *testing.T) {
	s := newRVSim(t)
	at := s.play(s.t0, 5*time.Second, 1, 40*time.Millisecond)
	s.m.StartAt = at
	s.play(at, 10*time.Minute, 1.005, 40*time.Millisecond)
	s.expectVerified()
}

func TestVerify_OtherResetIgnored(t *testing.T) {
	s := newRVSim(t)
	s.g.reset.lastCompleted = resetKey{id: 5, initiator: 1}
	s.m.ResetID, s.m.ResetInitiator = 4, 1
	s.m.StartAt = s.t0
	s.send(s.t0, 40*time.Millisecond)
	if s.p.RunTiming.OK || !s.p.GameStartTime.IsZero() {
		t.Errorf("accepted the run timing of another reset")
	}
	s.m.ResetID = 5
	s.send(s.t0, 40*time.Millisecond)
	if !s.p.RunTiming.OK || !s.p.GameStartTime.Equal(s.t0) {
		t.Errorf("didn't accept the run timing of the same reset")
	}
}

func TestVerify_CommonRunCost(t *testing.T) {
	g := &Game{}
	g.players[0].IndexF = 0
	g.local = &g.players[0]
	ps := make([]*Player, 4)
	for i := range ps {
		ps[i] = &g.players[i]
	}
	var sigA, sigB patchSignature
	copy(sigA[:], "A")
	copy(sigB[:], "B")
	g.ROMVerified, g.patchSignature = true, sigA
	g.runTiming.gates = gateSprites
	for i, sig := range []patchSignature{sigB, sigB, sigB} {
		p := ps[i+1]
		p.RunTiming = runTimingRemote{OK: true, Signature: sig, Gates: gateSprites}
	}
	common, differ := g.commonRunCost(ps)
	if !differ || common.signature != sigB {
		t.Errorf("common %v, differ %v; expected B and true", common, differ)
	}
	if r := g.verifyPlayer(g.local, common); len(r) != 1 || !strings.Contains(r[0], "patched ROM") {
		t.Errorf("local player reasons %v", r)
	}
	if r := g.verifyPlayer(ps[2], common); len(r) != 0 {
		t.Errorf("player 2 reasons %v", r)
	}

	// a tie goes to the local player's:
	ps[3].RunTiming.Signature = sigA
	common, _ = g.commonRunCost(ps)
	if common != (runCost{sigA, gateSprites}) {
		t.Errorf("common %v; expected the local player's", common)
	}
}
