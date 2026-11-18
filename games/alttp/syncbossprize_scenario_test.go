package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

const bpRoom = uint16(0x00C8) // Eastern Palace's boss room

// newBossPrizeScenario stubs RoomTag_GetHeartForPrize's spawn_prize: put a falling prize in ancilla slot 0 and delete
// the tag in slot $0E.
func newBossPrizeScenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := newSKScenario(t, players)
	for _, gs := range h.gs {
		addr := gs.g.romFunctions[fnRoomTagSpawnPrize]
		pak, _ := lorom.BusAddressToPak(addr)
		copy(gs.e.ROM[pak:], []byte{
			0xA9, ancillaFallingPrize, // LDA #$29
			0x8D, 0x4A, 0x0C, // STA $0C4A
			0xA6, 0x0E, // LDX $0E
			0x74, 0xAE, // STZ $AE,X
			0x60, // RTS
		})
	}
	return h
}

// inBossRoom enters the boss room with its prize tag in slot 1 and the given room flags.
func (h *skScenario) inBossRoom(i int, flags uint8) {
	h.inRoom(i, bpRoom)
	e := h.gs[i].e
	e.WRAM[0xAE] = 0
	e.WRAM[0xAF] = roomTagGetHeartForPrize
	e.WRAM[roomFlagsOffs] = flags
	e.WRAM[0x0E] = 0x5A
	for k := uint32(0); k < ancillaSlots; k++ {
		e.WRAM[uint32(ancillaTypes)+k] = 0
	}
}

// beatBoss takes the boss's heart container and lets the prize tag run; spawned puts a prize in ancilla slot 3 like
// the game does when the prize was not collected yet.
func (h *skScenario) beatBoss(i int, spawned bool) {
	e := h.gs[i].e
	e.WRAM[roomFlagsOffs] |= roomFlagBoss
	e.WRAM[0xAF] = 0
	if spawned {
		e.WRAM[uint32(ancillaTypes)+3] = ancillaFallingPrize
	}
}

func (h *skScenario) expectAncillae(when string, i int, expected ...uint8) {
	h.t.Helper()
	e := h.gs[i].e
	for k, x := range expected {
		if actual := e.WRAM[uint32(ancillaTypes)+uint32(k)]; actual != x {
			h.t.Errorf("%s: g%d: expected ancilla %d type $%02x; got $%02x", when, i+1, k, x, actual)
		}
	}
}

func TestBossPrizeScenario_SpawnsCollectedPrize(t *testing.T) {
	h := newBossPrizeScenario(t, 1)
	h.inBossRoom(0, 0)
	h.frames(skSettleFrames)
	h.expectAncillae("fighting", 0, 0)

	// the prize was collected already, so the game deletes the tag without spawning it:
	h.beatBoss(0, false)
	h.frames(skSettleFrames)
	h.expectAncillae("boss beaten", 0, ancillaFallingPrize)
	if e := h.gs[0].e; e.WRAM[0x0E] != 0x5A || e.WRAM[0xAF] != 0 {
		t.Errorf("expected $0E = $5A and tag slot 1 cleared; got $%02x, $%02x", e.WRAM[0x0E], e.WRAM[0xAF])
	}

	// only once:
	h.gs[0].e.WRAM[ancillaTypes] = 0
	h.frames(skSettleFrames)
	h.expectAncillae("picked up", 0, 0)
}

func TestBossPrizeScenario_GameSpawnedPrize(t *testing.T) {
	h := newBossPrizeScenario(t, 1)
	h.inBossRoom(0, 0)
	h.frames(skSettleFrames)

	h.beatBoss(0, true)
	h.frames(skSettleFrames)
	h.expectAncillae("boss beaten", 0, 0, 0, 0, ancillaFallingPrize)

	// it was picked up; nothing to spawn:
	h.gs[0].e.WRAM[uint32(ancillaTypes)+3] = 0
	h.frames(skSettleFrames)
	h.expectAncillae("picked up", 0, 0, 0, 0, 0)
}

func TestBossPrizeScenario_BeatenBeforeVisit(t *testing.T) {
	h := newBossPrizeScenario(t, 1)
	h.inBossRoom(0, roomFlagBoss)
	h.frames(skSettleFrames)
	h.gs[0].e.WRAM[0xAF] = 0
	h.frames(skSettleFrames)
	h.expectAncillae("revisit", 0, 0)
}

func TestBossPrizeScenario_WaitsForItemReceipt(t *testing.T) {
	h := newBossPrizeScenario(t, 1)
	h.inBossRoom(0, 0)
	h.frames(skSettleFrames)

	h.beatBoss(0, false)
	h.gs[0].e.WRAM[uint32(ancillaTypes)+5] = ancillaItemReceipt
	h.frames(skSettleFrames)
	h.expectAncillae("holding the heart container", 0, 0)

	h.gs[0].e.WRAM[uint32(ancillaTypes)+5] = 0
	h.frames(skSettleFrames)
	h.expectAncillae("receipt done", 0, ancillaFallingPrize)
}

func TestBossPrizeScenario_LeftRoom(t *testing.T) {
	h := newBossPrizeScenario(t, 1)
	h.inBossRoom(0, 0)
	h.frames(skSettleFrames)

	// a different room with the boss flag set and no tag:
	h.inRoom(0, bpRoom+1)
	h.gs[0].e.WRAM[roomFlagsOffs] = roomFlagBoss
	h.gs[0].e.WRAM[0xAF] = 0
	h.frames(skSettleFrames)
	h.expectAncillae("other room", 0, 0)
}
