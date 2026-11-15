package alttp

import (
	"fmt"
	"io"
	"o2/interfaces"
	"testing"
	"time"
)

// These scenario tests are black-box: they only manipulate and inspect each player's emulated WRAM and let the full
// read -> network -> update routine -> emulated frame loop run.

const (
	skEastern      = uint8(0x04) // $040C value for Eastern Palace
	skEasternSlot  = smallKeyFirst + 2
	skCurrentKeys  = uint16(0xF36F)
	skDungeonID    = uint16(0x040C)
	skModule       = uint16(0x0010)
	skSubModule    = uint16(0x0011)
	skSettleFrames = 12
)

type skScenario struct {
	t  *testing.T
	s  *testServer
	gs []*gameSync
	n  [][]string

	// gameStripes is the size of the game's own VRAM stripes queued every frame (see settleStripes):
	gameStripes uint8
}

func newSKScenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := &skScenario{t: t}

	clients := make([]*testClient, 0, players)
	for i := 0; i < players; i++ {
		gs, err := createTestGameSync("VT test", fmt.Sprintf("g%d", i+1), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		h.gs = append(h.gs, &gs)
		clients = append(clients, gs.c)
	}
	h.n = make([][]string, players)

	h.s = &testServer{Clients: clients, Now: time.Now()}
	h.s.HandleAllClients(nil)
	for _, gs := range h.gs {
		gameHandleNet(gs.g)
	}

	for i := range h.gs {
		i := i
		h.gs[i].g.Notifications.Subscribe(interfaces.ObserverImpl(func(object interface{}) {
			h.n[i] = append(h.n[i], object.(string))
		}))
	}

	return h
}

func (h *skScenario) inDungeon(i int, keys uint8) {
	e := h.gs[i].e
	e.WRAM[skModule] = 0x07
	e.WRAM[skSubModule] = 0x00
	e.WRAM[skDungeonID] = skEastern
	e.WRAM[skCurrentKeys] = keys
	// slot is stale while inside the dungeon; the game only writes it back on exit:
	e.WRAM[skEasternSlot] = keys
}

func (h *skScenario) onOverworld(i int, keys uint8) {
	e := h.gs[i].e
	e.WRAM[skModule] = 0x09
	e.WRAM[skSubModule] = 0x00
	e.WRAM[skDungeonID] = 0xFF
	e.WRAM[skCurrentKeys] = 0xFF
	e.WRAM[skEasternSlot] = keys
}

// keys returns the player's current Eastern Palace key count from wherever the game keeps it.
func (h *skScenario) keys(i int) uint8 {
	e := h.gs[i].e
	if e.WRAM[skDungeonID] == skEastern {
		return e.WRAM[skCurrentKeys]
	}
	return e.WRAM[skEasternSlot]
}

func (h *skScenario) frame() {
	for _, gs := range h.gs {
		// test frames run much faster than real time; don't let the wall-clock update cooldown throttle updates:
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(h.t)
		h.s.HandleAllClients(nil)
	}
	h.s.AdvanceTime(17 * time.Millisecond)
}

func (h *skScenario) frames(n int) {
	for f := 0; f < n; f++ {
		h.frame()
	}
}

func (h *skScenario) expectKeys(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		if actual := h.keys(i); actual != x {
			h.t.Errorf("%s: g%d: expected %d Eastern Palace keys; got %d", when, i+1, x, actual)
		}
	}
}

func TestSmallKeysScenario_Pickup(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.frames(skSettleFrames)
	h.expectKeys("initial", 0, 0)

	h.gs[0].e.WRAM[skCurrentKeys] = 1
	h.frames(skSettleFrames)
	h.expectKeys("after g1 pickup", 1, 1)

	h.gs[1].e.WRAM[skCurrentKeys] = 0
	h.frames(skSettleFrames)
	h.expectKeys("after g2 spend", 0, 0)
}

func TestSmallKeysScenario_PickupWhileOnOverworld(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.onOverworld(1, 0)
	h.frames(skSettleFrames)

	h.gs[0].e.WRAM[skCurrentKeys] = 1
	h.frames(skSettleFrames)
	h.expectKeys("after g1 pickup", 1, 1)
}

func TestSmallKeysScenario_ConcurrentPickups(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 1)
	h.inDungeon(1, 1)
	h.frames(skSettleFrames)

	// both players pick up a different key in the same frame:
	h.gs[0].e.WRAM[skCurrentKeys] = 2
	h.gs[1].e.WRAM[skCurrentKeys] = 2
	h.frames(skSettleFrames)
	h.expectKeys("after concurrent pickups", 3, 3)
}

func TestSmallKeysScenario_ConcurrentPickupAndSpend(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 1)
	h.inDungeon(1, 1)
	h.frames(skSettleFrames)

	// g1 picks up a key while g2 spends one in the same frame:
	h.gs[0].e.WRAM[skCurrentKeys] = 2
	h.gs[1].e.WRAM[skCurrentKeys] = 0
	h.frames(skSettleFrames)
	h.expectKeys("after concurrent pickup and spend", 1, 1)
}

func TestSmallKeysScenario_ThreePlayers(t *testing.T) {
	h := newSKScenario(t, 3)
	h.inDungeon(0, 2)
	h.inDungeon(1, 2)
	h.onOverworld(2, 2)
	h.frames(skSettleFrames)

	h.gs[0].e.WRAM[skCurrentKeys]-- // spend
	h.gs[1].e.WRAM[skCurrentKeys]++ // pickup
	h.frame()
	h.gs[1].e.WRAM[skCurrentKeys]-- // spend
	h.frames(skSettleFrames)
	h.expectKeys("after mixed activity", 1, 1, 1)
}

// TestSmallKeysScenario_StaleSlotInDungeon verifies the stale per-dungeon slot is never mistaken for a key change
// while the player is inside the dungeon but not in submodule 0 (room transitions, dialogs, item menu).
func TestSmallKeysScenario_StaleSlotInDungeon(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.frames(skSettleFrames)

	// g1 picks up a key; like the real game, only $7EF36F changes, the slot stays stale:
	h.gs[0].e.WRAM[skCurrentKeys] = 1
	h.frames(skSettleFrames)
	h.expectKeys("after g1 pickup", 1, 1)

	// g1 walks through room transitions and opens the item menu a few times:
	for _, state := range [][2]uint8{{0x07, 0x02}, {0x07, 0x00}, {0x0E, 0x01}, {0x07, 0x00}, {0x07, 0x07}, {0x07, 0x00}} {
		h.gs[0].e.WRAM[skModule] = state[0]
		h.gs[0].e.WRAM[skSubModule] = state[1]
		for f := 0; f < 4; f++ {
			h.frame()
			h.expectKeys(fmt.Sprintf("during g1 module $%02x,$%02x", state[0], state[1]), 1, 1)
		}
	}
}

// TestSmallKeysScenario_LocalChangeWhileUpdateInFlight verifies that a key the local player spends after an update
// routine was generated but before it executed is not clobbered by the update.
func TestSmallKeysScenario_LocalChangeWhileUpdateInFlight(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 1)
	h.inDungeon(1, 1)
	h.frames(skSettleFrames)

	// g1 picks up a key; in the same frame g2 receives it and generates an update routine. Right before that routine
	// executes, g2 spends a key:
	h.gs[0].e.WRAM[skCurrentKeys] = 2
	fired := false
	h.gs[1].beforeEmulate = func() {
		if !fired {
			fired = true
			h.gs[1].e.WRAM[skCurrentKeys]--
		}
	}
	h.frames(skSettleFrames)
	h.expectKeys("after g1 pickup and g2 spend", 1, 1)
}

// TestSmallKeysScenario_ReloadDoesNotSpreadStaleCounts verifies that a player who goes back to the file select screen
// and loads a game with different key counts does not overwrite everyone else's key counts.
func TestSmallKeysScenario_ReloadDoesNotSpreadStaleCounts(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.frames(skSettleFrames)

	h.gs[0].e.WRAM[skCurrentKeys] = 2
	h.frames(skSettleFrames)
	h.expectKeys("after g1 pickups", 2, 2)

	// g2 save & quits to the file select screen:
	h.gs[1].e.WRAM[skModule] = 0x01
	h.frames(4)
	// g2 loads a save file that has no keys:
	h.onOverworld(1, 0)
	h.frames(skSettleFrames)
	if actual := h.keys(0); actual != 2 {
		t.Errorf("g1: expected to keep 2 Eastern Palace keys; got %d", actual)
	}
	if actual := h.keys(1); actual != 2 {
		t.Errorf("g2: expected to adopt the group's 2 Eastern Palace keys; got %d", actual)
	}
}

// TestSmallKeysScenario_SewersAndHyruleCastle verifies Sewers ($040C=$00) and Hyrule Castle ($040C=$02) share one key
// count which is written to both of their slots.
func TestSmallKeysScenario_SewersAndHyruleCastle(t *testing.T) {
	for _, dungeons := range [][2]uint8{{0x00, 0x00}, {0x02, 0x02}, {0x00, 0x02}, {0x02, 0xFF}} {
		t.Run(fmt.Sprintf("g1_%02x_g2_%02x", dungeons[0], dungeons[1]), func(t *testing.T) {
			h := newSKScenario(t, 2)
			for i, d := range dungeons {
				e := h.gs[i].e
				e.WRAM[skModule] = 0x07
				e.WRAM[skSubModule] = 0x00
				e.WRAM[skDungeonID] = d
				e.WRAM[skCurrentKeys] = 0
				if d == 0xFF {
					e.WRAM[skModule] = 0x09
					e.WRAM[skCurrentKeys] = 0xFF
				}
			}
			h.frames(skSettleFrames)

			h.gs[0].e.WRAM[skCurrentKeys]++
			h.frames(skSettleFrames)

			e := h.gs[1].e
			if dungeons[1] != 0xFF {
				if actual := e.WRAM[skCurrentKeys]; actual != 1 {
					t.Errorf("g2: expected current key count 1; got %d", actual)
				}
			}
			if actual := e.WRAM[smallKeyFirst]; actual != 1 {
				t.Errorf("g2: expected Sewers slot 1; got %d", actual)
			}
			if actual := e.WRAM[smallKeyFirst+1]; actual != 1 {
				t.Errorf("g2: expected Hyrule Castle slot 1; got %d", actual)
			}
		})
	}
}

// TestSmallKeysScenario_CatchUpOnJoin verifies players with no key history converge on the highest key count.
func TestSmallKeysScenario_CatchUpOnJoin(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 1)
	h.inDungeon(1, 2)
	h.frames(skSettleFrames)
	h.expectKeys("after joining", 2, 2)

	// and deltas flow normally afterward:
	h.gs[0].e.WRAM[skCurrentKeys]--
	h.frames(skSettleFrames)
	h.expectKeys("after g1 spend", 1, 1)
}

// TestSmallKeysScenario_DoubleSpend verifies two players spending the last key at the same time converge on 0 and are
// told about it.
func TestSmallKeysScenario_DoubleSpend(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 1)
	h.inDungeon(1, 1)
	h.frames(skSettleFrames)

	h.gs[0].e.WRAM[skCurrentKeys]--
	h.gs[1].e.WRAM[skCurrentKeys]--
	h.frames(skSettleFrames)
	h.expectKeys("after double spend", 0, 0)

	found := false
	for i := range h.n {
		for _, n := range h.n[i] {
			if n == "Eastern Palace small key double-spend detected" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected a double-spend notification")
	}
}

// TestSmallKeysScenario_AbortedUpdate verifies a dungeon's key sync recovers after an update routine could not be
// confirmed to have executed.
func TestSmallKeysScenario_AbortedUpdate(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.frames(skSettleFrames)

	// g1 picks up a key; g2 generates an update routine but switches to a module where the routine won't run:
	h.gs[0].e.WRAM[skCurrentKeys]++
	g2 := h.gs[1]
	fired := false
	g2.beforeEmulate = func() {
		if !fired && g2.g.updateStage > 0 {
			fired = true
			g2.e.WRAM[skModule] = 0x08
		}
	}
	h.frames(4)
	if !fired {
		t.Fatal("g2 never generated an update")
	}
	// time out waiting for the update to execute:
	g2.g.lastUpdateTime = time.Now().Add(-time.Minute)
	h.frames(2)
	// g2 cancels the update routine as if it was replaced by the next one, and goes back into the dungeon:
	g2.e.WRAM[skModule] = 0x07
	h.frames(skSettleFrames)
	h.expectKeys("after aborted update", 1, 1)

	// key sync continues to work for that dungeon:
	h.gs[0].e.WRAM[skCurrentKeys]++
	h.frames(skSettleFrames)
	h.expectKeys("after next pickup", 2, 2)
}
