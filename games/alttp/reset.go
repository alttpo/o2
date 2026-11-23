package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"
)

// Group-wide reset state machine
//
// Because every player continuously re-broadcasts its full state to every other player, simply clearing local
// state when the "reset" button is pressed does not work: the other players immediately broadcast their old state
// back and the reset is undone. Instead, all players in the group are walked through a small state machine:
//
//	idle --(initiate)--> initiated --(clear state)--> cleared --(finalize)--> idle
//
//  1. initiate: the player pressing "reset" becomes the initiator and broadcasts an Initiate message identified by
//     a unique reset id. Every player receiving it (including the initiator itself) enters the state machine and
//     immediately stops sending its own state and stops accepting state from anyone else.
//  2. cleared: each player clears its local sync state and run timer, then acknowledges the reset id with a
//     Cleared message. While in this phase no state is exchanged at all, so nothing can leak back in.
//  3. finalize: once the initiator has collected acknowledgements from all participants (or a timeout elapsed) and
//     a short quiet period has passed to let any in-flight state packets drain, it broadcasts Finalize. Every player
//     receiving it exits the state machine and resumes normal syncing from the freshly cleared state.
//
// All messages are re-sent periodically while the state machine is active to tolerate packet loss. If two players
// initiate a reset at the same time, the reset with the lowest (id, initiator) pair wins and everyone converges on it.

type resetPhase uint8

const (
	// resetPhaseIdle means no reset is in progress; normal syncing occurs.
	resetPhaseIdle resetPhase = iota
	// resetPhaseInitiate is a message from the initiator announcing a reset; locally it means we have entered the
	// state machine and stopped syncing but have not yet cleared state.
	resetPhaseInitiate
	// resetPhaseCleared is a message from a participant acknowledging it has cleared its state; locally it means our
	// state is cleared and we are waiting for finalization.
	resetPhaseCleared
	// resetPhaseFinalize is a message from the initiator signaling all players to exit the state machine.
	resetPhaseFinalize
)

func (p resetPhase) String() string {
	switch p {
	case resetPhaseIdle:
		return "idle"
	case resetPhaseInitiate:
		return "initiate"
	case resetPhaseCleared:
		return "cleared"
	case resetPhaseFinalize:
		return "finalize"
	default:
		return fmt.Sprintf("resetPhase(%d)", uint8(p))
	}
}

const (
	// resetQuietPeriod is the minimum time to remain in the cleared state after the last acknowledgement arrives so
	// that any state packets still in flight from before the reset are received (and discarded) before syncing resumes.
	resetQuietPeriod = 750 * time.Millisecond
	// resetAckTimeout is the maximum time the initiator waits for acknowledgements before finalizing anyway.
	resetAckTimeout = 5 * time.Second
	// resetParticipantTimeout is the maximum time a participant waits for finalization before giving up.
	resetParticipantTimeout = 15 * time.Second
	// resetFinalizeRepeat is how long the initiator keeps re-sending Finalize after it has finalized.
	resetFinalizeRepeat = 1 * time.Second
)

// resetKey uniquely identifies a reset operation.
type resetKey struct {
	id        uint64
	initiator uint16
}

func (k resetKey) isZero() bool { return k.id == 0 && k.initiator == 0 }

// less orders resets so that concurrently initiated resets converge on a single winner.
func (k resetKey) less(o resetKey) bool {
	if k.id != o.id {
		return k.id < o.id
	}
	return k.initiator < o.initiator
}

type resetMessage struct {
	from  int
	phase resetPhase
	key   resetKey
}

type resetState struct {
	phase       resetPhase
	key         resetKey
	isInitiator bool
	startedAt   time.Time
	lastAckAt   time.Time
	// acks maps participant player index to whether it has acknowledged; only used by the initiator:
	acks map[int]bool

	// lastCompleted is the most recently completed reset; used to ignore stale re-sent messages:
	lastCompleted resetKey
	// finalizeUntil is when the initiator stops re-sending Finalize for lastCompleted:
	finalizeUntil time.Time

	// inbox holds received reset messages to be processed outside of deserialization:
	inbox []resetMessage
}

// IsResetting returns true while the group reset state machine is active; no game state is sent or received.
func (g *Game) IsResetting() bool {
	return g.reset.phase != resetPhaseIdle
}

// RequestReset asks the game loop to initiate a group-wide reset. Safe to call from any goroutine.
func (g *Game) RequestReset() {
	if !g.running {
		// no game loop to drive the state machine; just clear local state:
		g.resetClearState()
		g.NotifyView()
		g.PushNotification("reset game")
		return
	}

	select {
	case g.resetRequests <- struct{}{}:
	default:
		// a request is already pending
	}
}

func (g *Game) resetNow() time.Time {
	if g.lastServerTime.IsZero() {
		return time.Now()
	}
	return g.ServerNow()
}

// initiateReset is called on the game loop goroutine when the local player presses "reset".
func (g *Game) initiateReset(now time.Time) {
	r := &g.reset
	localIndex := g.local.Index()
	if localIndex < 0 || g.client == nil || !g.client.IsConnected() {
		// not connected to a group; there is nobody to coordinate with, so just clear our own state, dropping any
		// group reset that the disconnect left unfinished:
		log.Println("alttp: reset: not connected; resetting local state only")
		r.phase = resetPhaseIdle
		r.key = resetKey{}
		r.isInitiator = false
		r.acks = nil
		g.resetClearState()
		g.shouldUpdatePlayersList = true
		g.clean = false
		g.NotifyView()
		g.PushNotification("reset local state (offline)")
		return
	}

	if r.phase != resetPhaseIdle {
		log.Printf("alttp: reset: already in progress (phase=%s); ignoring request\n", r.phase)
		return
	}

	key := resetKey{
		id:        uint64(g.resetNow().UnixNano()),
		initiator: uint16(localIndex),
	}

	// snapshot the participants we expect acknowledgements from before clearing removes them:
	acks := make(map[int]bool)
	for _, p := range g.RemotePlayers() {
		acks[p.Index()] = false
	}

	g.enterReset(key, true, now)
	r.acks = acks

	log.Printf("alttp: reset: initiated %016x by [%02x]; waiting on %d participants\n", key.id, key.initiator, len(acks))
	g.sendResetMessage(resetPhaseInitiate, key)

	g.clearForReset()
	g.PushNotification("reset initiated")
}

// enterReset transitions into the initiate phase which stops all state syncing.
func (g *Game) enterReset(key resetKey, isInitiator bool, now time.Time) {
	r := &g.reset
	r.phase = resetPhaseInitiate
	r.key = key
	r.isInitiator = isInitiator
	r.startedAt = now
	r.lastAckAt = time.Time{}
	r.acks = nil
}

// clearForReset transitions from the initiate phase to the cleared phase by clearing all local state.
func (g *Game) clearForReset() {
	r := &g.reset
	if r.phase != resetPhaseInitiate {
		return
	}

	g.resetClearState()
	r.phase = resetPhaseCleared

	if !r.isInitiator {
		g.sendResetMessage(resetPhaseCleared, r.key)
	}
}

// finishReset exits the state machine and resumes normal syncing.
func (g *Game) finishReset(now time.Time, notification string) {
	r := &g.reset
	log.Printf("alttp: reset: %016x by [%02x] finished: %s\n", r.key.id, r.key.initiator, notification)

	r.lastCompleted = r.key
	if r.isInitiator {
		r.finalizeUntil = now.Add(resetFinalizeRepeat)
	}

	r.phase = resetPhaseIdle
	r.key = resetKey{}
	r.isInitiator = false
	r.acks = nil

	g.shouldUpdatePlayersList = true
	g.clean = false
	g.NotifyView()
	g.PushNotification(notification)
}

// resetClearState clears all synchronized game state, including the run timer, while preserving local player
// identity (name, team, color and server-assigned index) so that we can keep communicating with the group.
func (g *Game) resetClearState() {
	var localIndex = -1
	if g.local != nil {
		localIndex = g.local.Index()
	}

	g.SoftReset()

	// keep our server-assigned player index so we don't need to rejoin the group:
	if localIndex >= 0 && localIndex < MaxPlayers {
		g.players[localIndex] = *g.local
		g.local = &g.players[localIndex]
		g.local.IndexF = localIndex
	}
	g.activePlayersClean = false
	// the others follow our frame pacing afresh, as we do theirs:
	g.local.Pacing.reset()

	// clear the run timer, and have the latches in SRAM cleared so the run starts again with the next file load:
	g.runTiming.resetRun()
	g.runMinStart = time.Time{}
	g.runMaxFinish = time.Time{}
	g.runPlayersFinished = 0
	g.runPlayersStarted = 0
	g.shouldUpdatePlayersList = true

	if g.viewModels != nil {
		g.viewModels.NotifyView("game/run/timer", "")
		g.viewModels.NotifyView("game/run/abs", RunTimesViewModel{})
	}
}

// processResetMessages handles all reset messages received since the last call. Called on the game loop goroutine.
func (g *Game) processResetMessages(now time.Time) {
	r := &g.reset
	inbox := r.inbox
	r.inbox = nil

	for _, m := range inbox {
		g.handleResetMessage(m, now)
	}
}

func (g *Game) handleResetMessage(m resetMessage, now time.Time) {
	r := &g.reset

	// ignore re-sent messages for resets that already completed (or older ones):
	if !r.lastCompleted.isZero() && !r.lastCompleted.less(m.key) {
		return
	}

	switch m.phase {
	case resetPhaseInitiate:
		if r.phase != resetPhaseIdle {
			if m.key == r.key {
				// re-sent initiate for the reset we're already in; re-acknowledge:
				if !r.isInitiator && r.phase == resetPhaseCleared {
					g.sendResetMessage(resetPhaseCleared, r.key)
				}
				return
			}
			if !m.key.less(r.key) {
				// a concurrent reset that loses to ours; its initiator will adopt ours when it sees our Initiate:
				return
			}
			// a concurrent reset that wins over ours; abandon ours and adopt it:
			log.Printf(
				"alttp: reset: abandoning %016x by [%02x] in favor of %016x by [%02x]\n",
				r.key.id, r.key.initiator, m.key.id, m.key.initiator,
			)
		}

		name := g.players[m.from].Name()
		if name == "" {
			name = fmt.Sprintf("player #%02x", m.from)
		}
		log.Printf("alttp: reset: joining %016x initiated by [%02x] %s\n", m.key.id, m.key.initiator, name)

		g.enterReset(m.key, false, now)
		g.clearForReset()
		g.PushNotification(fmt.Sprintf("reset initiated by %s", name))
		break

	case resetPhaseCleared:
		if r.phase == resetPhaseIdle || !r.isInitiator || m.key != r.key {
			return
		}
		if acked, ok := r.acks[m.from]; !ok || !acked {
			log.Printf("alttp: reset: [%02x] acknowledged\n", m.from)
			r.acks[m.from] = true
			r.lastAckAt = now
		}
		break

	case resetPhaseFinalize:
		if r.phase == resetPhaseIdle || r.isInitiator || m.key != r.key {
			return
		}
		g.finishReset(now, "reset complete")
		break
	}
}

// tickReset drives timeouts and periodic re-sends of the state machine. Called periodically on the game loop goroutine.
func (g *Game) tickReset(now time.Time) {
	r := &g.reset

	if r.phase == resetPhaseIdle {
		// keep re-sending Finalize for a short while to make sure all participants see it:
		if !r.lastCompleted.isZero() && now.Before(r.finalizeUntil) {
			g.sendResetMessage(resetPhaseFinalize, r.lastCompleted)
		}
		return
	}

	if r.phase == resetPhaseInitiate {
		g.clearForReset()
	}

	elapsed := now.Sub(r.startedAt)

	if !r.isInitiator {
		if elapsed >= resetParticipantTimeout {
			g.finishReset(now, "reset timed out waiting for initiator; resuming sync")
			return
		}

		// keep acknowledging in case our previous acks were lost:
		g.sendResetMessage(resetPhaseCleared, r.key)
		return
	}

	// initiator:
	allAcked := true
	for _, acked := range r.acks {
		if !acked {
			allAcked = false
			break
		}
	}

	quietSince := r.startedAt
	if r.lastAckAt.After(quietSince) {
		quietSince = r.lastAckAt
	}

	if (allAcked && now.Sub(quietSince) >= resetQuietPeriod) || elapsed >= resetAckTimeout {
		if !allAcked {
			log.Printf("alttp: reset: timed out waiting for acknowledgements; finalizing anyway\n")
		}
		g.sendResetMessage(resetPhaseFinalize, r.key)
		g.finishReset(now, "reset complete")
		return
	}

	// keep announcing the reset in case our previous announcements were lost:
	g.sendResetMessage(resetPhaseInitiate, r.key)
}

func (g *Game) sendResetMessage(phase resetPhase, key resetKey) {
	if g.local == nil || g.local.Index() < 0 {
		return
	}

	// alttpo takes part in the reset too (RunTiming.as):
	m := g.makeBroadcastMessage()
	if err := m.add(func(w io.Writer) error { return SerializeReset(w, phase, key) }); err != nil {
		log.Printf("alttp: reset: serialize: %v\n", err)
		return
	}
	g.send(m)
}

func SerializeReset(w io.Writer, phase resetPhase, key resetKey) (err error) {
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgReset)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, uint8(phase)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, key.id); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, key.initiator); err != nil {
		return
	}
	return
}

func (g *Game) DeserializeReset(p *Player, r io.Reader) (err error) {
	var phase uint8
	var key resetKey
	if err = binary.Read(r, binary.LittleEndian, &phase); err != nil {
		return fmt.Errorf("error deserializing reset: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &key.id); err != nil {
		return fmt.Errorf("error deserializing reset: %w", err)
	}
	if err = binary.Read(r, binary.LittleEndian, &key.initiator); err != nil {
		return fmt.Errorf("error deserializing reset: %w", err)
	}

	if p.Index() < 0 || resetPhase(phase) == resetPhaseIdle || resetPhase(phase) > resetPhaseFinalize {
		return
	}

	// defer processing until after deserialization since handling may clear the players array:
	g.reset.inbox = append(g.reset.inbox, resetMessage{
		from:  p.Index(),
		phase: resetPhase(phase),
		key:   key,
	})
	return
}
