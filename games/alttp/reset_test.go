package alttp

import (
	"io"
	"testing"
	"time"
)

type testViewModels map[string]interface{}

func (v testViewModels) NotifyView(view string, viewModel interface{})   { v[view] = viewModel }
func (v testViewModels) SetViewModel(view string, viewModel interface{}) { v[view] = viewModel }
func (v testViewModels) GetViewModel(view string) (interface{}, bool) {
	vm, ok := v[view]
	return vm, ok
}

type resetTestHarness struct {
	t   *testing.T
	s   *testServer
	gs  [3]gameSync
	now time.Time
}

func newResetTestHarness(t *testing.T) *resetTestHarness {
	h := &resetTestHarness{t: t, now: time.Now()}

	names := []string{"g1", "g2", "g3"}
	clients := make([]*testClient, 0, len(h.gs))
	for i := range h.gs {
		var err error
		h.gs[i], err = createTestGameSync("ZELDANODENSETSU", names[i], io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, h.gs[i].c)
		h.gs[i].g.viewModels = testViewModels{}
	}

	h.s = &testServer{Clients: clients, Now: h.now}

	// join group and exchange names:
	h.exchange()
	for i := range h.gs {
		// local player is normally marked active when reading from the SNES:
		h.gs[i].g.SetTTL(h.gs[i].g.local, 255)
		h.gs[i].g.sendPlayerName()
	}
	h.exchange()

	for i := range h.gs {
		if n := len(h.gs[i].g.RemotePlayers()); n != len(h.gs)-1 {
			t.Fatalf("g[%d]: expected %d remote players; got %d", i, len(h.gs)-1, n)
		}
	}

	return h
}

// exchange routes all pending messages through the server and lets each game handle them.
func (h *resetTestHarness) exchange() {
	h.s.HandleAllClients(nil)
	for i := range h.gs {
		g := h.gs[i].g
		for len(g.client.Read()) > 0 {
			msg := <-g.client.Read()
			if err := g.handleNetMessage(msg); err != nil {
				h.t.Fatal(err)
			}
			g.processResetMessages(h.now)
		}
	}
}

func (h *resetTestHarness) tick(d time.Duration) {
	h.now = h.now.Add(d)
	h.s.AdvanceTime(d)
	for i := range h.gs {
		h.gs[i].g.tickReset(h.now)
	}
	h.exchange()
}

func (h *resetTestHarness) setRunTimes() {
	for i := range h.gs {
		g := h.gs[i].g
		g.local.GameStartTime = h.now.Add(-time.Hour)
		g.local.GameFinishTime = h.now.Add(-time.Minute)
		g.sendPlayerName()
	}
	h.exchange()
	for i := range h.gs {
		g := h.gs[i].g
		g.updatePlayersList()
		if g.runMinStart.IsZero() {
			h.t.Fatalf("g[%d]: expected run timer start to be set", i)
		}
	}
}

func (h *resetTestHarness) assertPhase(i int, phase resetPhase) {
	h.t.Helper()
	if actual := h.gs[i].g.reset.phase; actual != phase {
		h.t.Fatalf("g[%d]: expected reset phase %s; got %s", i, phase, actual)
	}
}

func (h *resetTestHarness) assertRunTimerCleared(i int) {
	h.t.Helper()
	g := h.gs[i].g
	g.updatePlayersList()
	if !g.runMinStart.IsZero() || !g.runMaxFinish.IsZero() {
		h.t.Fatalf("g[%d]: expected run timer cleared; got start=%v finish=%v", i, g.runMinStart, g.runMaxFinish)
	}
	if timer, _ := g.viewModels.GetViewModel("game/run/timer"); timer != "" {
		h.t.Fatalf("g[%d]: expected run timer view cleared; got %v", i, timer)
	}
	for _, p := range g.ActivePlayers() {
		if !p.GameStartTime.IsZero() || !p.GameFinishTime.IsZero() {
			h.t.Fatalf("g[%d]: player[%d] expected cleared run times", i, p.Index())
		}
	}
}

func TestReset_StateMachine(t *testing.T) {
	h := newResetTestHarness(t)
	h.setRunTimes()

	// give every player some SRAM state to sync:
	for i := range h.gs {
		h.gs[i].g.local.SRAM.data[0x340] = 1
	}

	g1 := h.gs[0].g
	localIndices := [3]int{}
	for i := range h.gs {
		localIndices[i] = h.gs[i].g.local.Index()
	}

	// g1 presses reset:
	g1.initiateReset(h.now)
	h.assertPhase(0, resetPhaseCleared)
	if !g1.reset.isInitiator {
		t.Fatal("g1 expected to be initiator")
	}
	if g1.local.Index() != localIndices[0] {
		t.Fatalf("g1: local index changed after clear")
	}
	if g1.local.SRAM.data[0x340] != 0 {
		t.Fatalf("g1: expected local SRAM cleared")
	}

	// other players receive Initiate, clear and acknowledge:
	h.exchange()
	// initiator receives acknowledgements:
	h.exchange()
	for i := 1; i < len(h.gs); i++ {
		h.assertPhase(i, resetPhaseCleared)
		if h.gs[i].g.reset.isInitiator {
			t.Fatalf("g[%d]: should not be initiator", i)
		}
		if h.gs[i].g.local.Index() != localIndices[i] {
			t.Fatalf("g[%d]: local index changed after clear", i)
		}
	}
	for idx, acked := range g1.reset.acks {
		if !acked {
			t.Fatalf("g1: expected ack from [%d]", idx)
		}
	}

	// while resetting, a player sending old state must not be accepted:
	h.gs[1].g.local.GameStartTime = h.now.Add(-time.Hour)
	{
		m := h.gs[1].g.makeBroadcastMessage()
		m.WriteByte(byte(MsgPlayerName))
		var name [20]byte
		copy(name[:], "g2")
		m.Write(name[:])
		m.WriteByte(0)
		m.Write(make([]byte, 8))
		m.WriteByte(1)
		h.gs[1].g.send(m)
	}
	// and the normal senders must not send anything:
	h.gs[2].g.sendPlayerName()
	h.gs[2].g.sendPackets()
	if len(h.gs[2].c.Wr) != 0 {
		t.Fatalf("g3: expected no state sent while resetting")
	}
	h.exchange()
	for i := range h.gs {
		if i == 1 {
			continue
		}
		p := &h.gs[i].g.players[localIndices[1]]
		if !p.GameStartTime.IsZero() {
			t.Fatalf("g[%d]: accepted run timer state while resetting", i)
		}
	}
	h.gs[1].g.local.GameStartTime = time.Time{}

	// not finalized before the quiet period elapses:
	h.tick(resetQuietPeriod / 4)
	for i := range h.gs {
		h.assertPhase(i, resetPhaseCleared)
	}

	// finalize after the quiet period:
	h.tick(resetQuietPeriod)
	for i := range h.gs {
		h.assertPhase(i, resetPhaseIdle)
		h.assertRunTimerCleared(i)
	}

	// re-sent finalize/initiate messages after completion must not re-enter the state machine:
	h.gs[0].g.sendResetMessage(resetPhaseInitiate, h.gs[0].g.reset.lastCompleted)
	h.tick(100 * time.Millisecond)
	for i := range h.gs {
		h.assertPhase(i, resetPhaseIdle)
	}

	// normal syncing resumes:
	h.gs[1].g.local.GameStartTime = h.now
	h.gs[1].g.sendPlayerName()
	h.exchange()
	if h.gs[0].g.players[localIndices[1]].GameStartTime.IsZero() {
		t.Fatal("g1: expected to receive g2's state after reset finalized")
	}
}

func TestReset_ConcurrentInitiators(t *testing.T) {
	h := newResetTestHarness(t)

	// g1 and g3 both press reset before seeing each other's messages:
	h.gs[0].g.initiateReset(h.now)
	h.gs[2].g.initiateReset(h.now)
	h.exchange()
	h.tick(10 * time.Millisecond)

	// all must converge on exactly one reset:
	winner := h.gs[0].g.reset.key
	initiators := 0
	for i := range h.gs {
		h.assertPhase(i, resetPhaseCleared)
		if h.gs[i].g.reset.key != winner {
			t.Fatalf("g[%d]: did not converge on the same reset", i)
		}
		if h.gs[i].g.reset.isInitiator {
			initiators++
		}
	}
	if initiators != 1 {
		t.Fatalf("expected exactly one initiator; got %d", initiators)
	}

	h.tick(resetQuietPeriod * 2)
	for i := range h.gs {
		h.assertPhase(i, resetPhaseIdle)
	}
}

func TestReset_AckTimeout(t *testing.T) {
	h := newResetTestHarness(t)

	// g3 goes silent (e.g. disconnected) but is still in the participant list:
	h.s.Clients = h.s.Clients[:2]
	g3 := h.gs[2]
	go func() {
		for range g3.c.Wr {
		}
	}()

	h.gs[0].g.initiateReset(h.now)
	h.exchange()
	h.tick(resetQuietPeriod * 2)
	h.assertPhase(0, resetPhaseCleared)

	h.tick(resetAckTimeout)
	h.assertPhase(0, resetPhaseIdle)
	h.assertPhase(1, resetPhaseIdle)
}
