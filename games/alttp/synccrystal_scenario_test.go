package alttp

import (
	"testing"
)

const csAnimationTimer = uint16(0x0112)

func (h *skScenario) crystal(i int) uint8 {
	return h.gs[i].e.WRAM[crystalSwitchOffs]
}

// hitCrystal toggles the crystal switch state like the game does when a crystal switch is hit.
func (h *skScenario) hitCrystal(i int) {
	h.gs[i].e.WRAM[crystalSwitchOffs] ^= 1
}

// settleCrystal runs frames, finishing the peg redraw submodule ($16) the update routine switches to like the game would.
func (h *skScenario) settleCrystal(n int) {
	for f := 0; f < n; f++ {
		h.frame()
		for _, gs := range h.gs {
			if gs.e.WRAM[skModule] == 0x07 && gs.e.WRAM[skSubModule] == 0x16 {
				gs.e.WRAM[skSubModule] = 0x00
			}
		}
	}
}

func (h *skScenario) expectCrystal(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		if actual := h.crystal(i); actual != x {
			h.t.Errorf("%s: g%d: expected crystal switch state %d; got %d", when, i+1, x, actual)
		}
	}
}

func TestCrystalSwitchScenario_Toggle(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("initial", 0, 0)

	h.hitCrystal(0)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("g1 hit", 1, 1)

	h.hitCrystal(1)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("g2 hit", 0, 0)

	// nothing flips back and forth afterwards:
	h.settleCrystal(skSettleFrames * 4)
	h.expectCrystal("settled", 0, 0)
}

func TestCrystalSwitchScenario_WaitsForAnimation(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.settleCrystal(skSettleFrames)

	h.gs[1].e.WRAM[csAnimationTimer] = 0x10
	h.hitCrystal(0)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("during animation", 1, 0)

	h.gs[1].e.WRAM[csAnimationTimer] = 0
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("after animation", 1, 1)
}

func TestCrystalSwitchScenario_OtherDungeon(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.inDungeon(1, 0)
	h.gs[1].e.WRAM[skDungeonID] = skEastern + 2
	h.settleCrystal(skSettleFrames)

	h.hitCrystal(0)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("other dungeon", 1, 0)
}

func TestCrystalSwitchScenario_EnterDungeonCatchesUp(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inDungeon(0, 0)
	h.onOverworld(1, 0)
	h.settleCrystal(skSettleFrames)

	h.hitCrystal(0)
	h.settleCrystal(skSettleFrames)

	// the overworld uses $C172 as scratch space; it must not be synced:
	h.gs[1].e.WRAM[crystalSwitchOffs] = 0x55
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("g2 on overworld", 1, 0x55)

	// g2 enters the dungeon; the dungeon load resets the state, which must not overwrite g1's:
	h.inDungeon(1, 0)
	h.gs[1].e.WRAM[crystalSwitchOffs] = 0
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("g2 entered", 1, 1)

	h.settleCrystal(skSettleFrames * 4)
	h.expectCrystal("settled", 1, 1)
}

func TestAlttpoCompat_CrystalSwitchFromAlttpo(t *testing.T) {
	h := newSKScenario(t, 1)
	g := h.gs[0].g
	h.inDungeon(0, 0)
	h.settleCrystal(skSettleFrames)

	frame := uint8(0)
	send := func(value uint8, ts uint32) {
		frame++
		p := newAlttpoPacket(3, frame)
		p.location(0x07, skEastern, 0x010012, 0, 0)
		// LocalGameState::serialize_crystals:
		p.u8(0x05, 0x01).u16(crystalSwitchOffs).u32(ts).u16(uint16(value))
		p.name("alttpo")
		p.send(t, g)
	}

	send(0, 0)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("alttpo baseline", 0)

	// alttpo's timestamps are its local clock in msec, truncated to 32 bits. Make it older than any change the o2 player
	// makes below since test frames run faster than real time:
	ts := timestampFromTime(g.ServerSNESTimestamp()) - 1000
	send(1, ts)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("alttpo hit", 1)

	// o2 player hits the switch later; alttpo's older state must not win:
	h.settleCrystal(2)
	h.hitCrystal(0)
	h.settleCrystal(skSettleFrames)
	send(1, ts)
	h.settleCrystal(skSettleFrames)
	h.expectCrystal("o2 hit", 0)
}
