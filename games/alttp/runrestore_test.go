package alttp

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"o2/snes"
)

func TestFormatTime_UTC(t *testing.T) {
	loc := time.FixedZone("UTC+5", 5*3600)
	at := time.Date(2026, 11, 20, 14, 5, 9, 123_456_789, loc)
	if got, want := FormatTime(at), "09:05:09.123Z"; got != want {
		t.Errorf("FormatTime %q; expected %q", got, want)
	}
	if got, want := FormatTimeFull(at), "2026-11-20 09:05:09.123 UTC"; got != want {
		t.Errorf("FormatTimeFull %q; expected %q", got, want)
	}
	if FormatTime(time.Time{}) != "" || FormatTimeFull(time.Time{}) != "" {
		t.Errorf("expected no text for no time")
	}
	if got, want := FormatDur(at, at.Add(time.Hour+2*time.Minute+3*time.Second+4567*time.Microsecond)), "01:02:03.004"; got != want {
		t.Errorf("FormatDur %q; expected %q (milliseconds)", got, want)
	}
}

// The run timer doesn't run while no player has a start time, runs from the earliest start while any player that
// started hasn't finished, and stops at the latest finish.
func TestRunTimer_OnlyWhenStarted(t *testing.T) {
	h := newResetTestHarness(t)
	g := h.gs[0].g
	if s := g.runTimerText(); s != "" {
		t.Fatalf("run timer %q before any start; expected empty", s)
	}

	// the server clock is whatever the last message said:
	g.lastServerTime, g.lastServerRecvTime = h.now, time.Now()
	start := h.now.Add(-time.Minute)
	setLocalRunTimes(g, start, time.Time{})
	if s := g.runTimerText(); s == "" {
		t.Fatalf("run timer empty with a player running")
	}

	finish := start.Add(30 * time.Second)
	setLocalRunTimes(g, start, finish)
	if s, want := g.runTimerText(), "00:00:30.000"; s != want {
		t.Fatalf("run timer %q after the only player that started finished; expected %q", s, want)
	}

	// a start that is cleared stops the timer:
	setLocalRunTimes(g, time.Time{}, time.Time{})
	if s := g.runTimerText(); s != "" {
		t.Fatalf("run timer %q after the start was cleared; expected empty", s)
	}
}

// Players away from the server (all players are marked away when the connection to the server closes) keep their start.
func TestRunTimer_KeepsPlayersAway(t *testing.T) {
	h := newResetTestHarness(t)
	h.setRunTimes()
	g := h.gs[1].g
	for i := range g.players {
		g.DecTTL(&g.players[i], 255)
		g.players[i].IndexF = -1
	}
	g.updatePlayersList()
	if g.runMinStart.IsZero() || g.runPlayersStarted != 3 || g.runPlayersFinished != 3 {
		t.Fatalf("start %v, started %d, finished %d; expected the run kept for all 3 players", g.runMinStart, g.runPlayersStarted, g.runPlayersFinished)
	}
}

func (h *resetTestHarness) exchangeN(n int) {
	for i := 0; i < n; i++ {
		h.exchange()
	}
}

// restart makes g forget its run and its sync state as a restarted client does, and has it tell the others.
func restartRun(g *Game) {
	g.runTiming = runTimingLocal{}
	g.local.GameStartTime, g.local.GameFinishTime = time.Time{}, time.Time{}
}

// A player that was away and lost its state (it was restarted) is given its start back by the others, who didn't
// clear what they saw.
func TestRunRestore_GivenBackByOthers(t *testing.T) {
	h := newResetTestHarness(t)
	h.setRunTimes()
	g0, g1 := h.gs[0].g, h.gs[1].g
	start, finish := g0.local.GameStartTime, g0.local.GameFinishTime

	restartRun(g0)
	g0.sendRunTimingBeat()
	h.exchangeN(3)

	p := &g1.players[g0.local.Index()]
	r := &g0.runTiming
	if !r.started || !r.startAt.Equal(start) || !r.finished || !r.finishAt.Equal(finish) {
		t.Fatalf("g0 started %v at %v, finished %v at %v; expected the run given back: %v, %v", r.started, r.startAt, r.finished, r.finishAt, start, finish)
	}
	if !g0.local.GameStartTime.Equal(start) || !g0.local.GameFinishTime.Equal(finish) {
		t.Errorf("g0's player times %v, %v; expected %v, %v", g0.local.GameStartTime, g0.local.GameFinishTime, start, finish)
	}
	// the others never lost it, and see it started again:
	if !p.GameStartTime.Equal(start) || !p.GameFinishTime.Equal(finish) {
		t.Errorf("g1 sees g0's start %v finish %v; expected %v %v", p.GameStartTime, p.GameFinishTime, start, finish)
	}
	if !p.RunTiming.StartAt.Equal(start) || p.RunTiming.Flags&rtFlagStarted == 0 {
		t.Errorf("g1 sees g0 as not started again: %+v", p.RunTiming)
	}
}

// A different player that took the slot of the one that left isn't given its run, and the others forget it.
func TestRunRestore_NotForAnotherPlayer(t *testing.T) {
	h := newResetTestHarness(t)
	h.setRunTimes()
	g0, g1 := h.gs[0].g, h.gs[1].g

	restartRun(g0)
	g0.local.NameF = "someone else"
	// the new name goes out in a new join-group, as on the fastbeat:
	g0.tickNet(time.Now())
	g0.sendRunTimingBeat()
	h.exchangeN(3)

	if g0.runTiming.started {
		t.Errorf("another player took the run of the one before it")
	}
	if p := &g1.players[g0.local.Index()]; !p.GameStartTime.IsZero() {
		t.Errorf("g1 still shows the start %v for the player that replaced the one that started", p.GameStartTime)
	}
}

// A late message from before a group reset doesn't bring back a run the reset cleared.
func TestRunRestore_NotAfterReset(t *testing.T) {
	h := newResetTestHarness(t)
	g0 := h.gs[0].g
	g0.reset.lastCompleted = resetKey{id: 5, initiator: 1}
	m := &runTimingRemote{Flags: rtFlagStarted, StartAt: h.now.Add(-time.Hour), StartFrame: 7}
	sender := &g0.players[h.gs[1].g.local.Index()]
	target := &Player{IndexF: g0.local.Index(), NameF: g0.local.NameF}

	try := func(key resetKey) {
		t.Helper()
		var b bytes.Buffer
		if err := SerializeRunRestore(&b, target, key, m, m.StartAt, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if err := g0.DeserializeRunRestore(sender, bytes.NewReader(b.Bytes()[1:])); err != nil {
			t.Fatal(err)
		}
	}
	try(resetKey{id: 4, initiator: 1})
	if g0.runTiming.started {
		t.Fatalf("took a run from before the last group reset")
	}
	try(resetKey{id: 5, initiator: 1})
	if !g0.runTiming.started || !g0.runTiming.startAt.Equal(m.StartAt) || g0.runTiming.startFrame != 7 {
		t.Fatalf("didn't take the run of the current group reset: started %v", g0.runTiming.started)
	}
}

// A start in the SNES that o2 didn't follow isn't cleared right away when others could give the run back.
func TestRunRestore_WaitsBeforeClearing(t *testing.T) {
	h := newResetTestHarness(t)
	g := h.gs[0].g
	g.ROMVerified = true
	t0 := time.Unix(1_700_000_000, 0)
	read := func(at time.Duration) {
		nmi := 0x1000 + uint32(durationToFrames(at))
		d := make([]byte, runTimingReadSize)
		setU24(d[rtmNMIFrames:], nmi)
		setU24(d[rtmLoopFrames:], nmi)
		setU24(d[rtmStartFrame:], 0x0100)
		d[rtmLatches] = rtmLatchStart
		g.readRunTimingResponse(snes.Response{Address: runTimingPak, Size: uint8(len(d)), Data: d}, t0.Add(at))
	}
	for _, at := range []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond} {
		read(at)
	}
	if g.runTiming.clearLatches {
		t.Fatalf("cleared the SRAM's start at once; expected to wait for the others")
	}
	for _, at := range []time.Duration{runRestoreGrace + time.Second, runRestoreGrace + 1100*time.Millisecond} {
		read(at)
	}
	if !g.runTiming.clearLatches {
		t.Fatalf("still waiting after %v; expected the start cleared", runRestoreGrace)
	}
}

func countNotes(g *Game, substr string) (n int) {
	hist, _ := g.viewModels.GetViewModel("game/notification/history")
	list, _ := hist.([]TimestampedNotification)
	for _, note := range list {
		if strings.Contains(note.Message, substr) {
			n++
		}
	}
	return
}

// A player that lost its SNES or server connection announces how long it was gone, and every client notes it once.
func TestRunGap_NotifiesEveryone(t *testing.T) {
	h := newResetTestHarness(t)
	g0 := h.gs[0].g

	g0.noteSNESQueue(true)
	g0.noteSNESQueue(false)
	g0.absence.snesLostAt = g0.absence.snesLostAt.Add(-12345 * time.Millisecond)
	g0.noteSNESQueue(true)
	g0.announceGaps(h.now)
	h.exchangeN(2)

	for i := range h.gs {
		if n := countNotes(h.gs[i].g, "SNES connection lost for 00:00:12.3"); n != 1 {
			t.Errorf("g[%d]: %d notes of the gap; expected 1", i, n)
		}
	}

	// announced again while it is repeated, but noted once:
	g0.announceGaps(h.now.Add(time.Second))
	h.exchangeN(2)
	if n := countNotes(h.gs[1].g, "connection lost"); n != 1 {
		t.Errorf("g[1]: %d notes after a repeat; expected 1", n)
	}
}
