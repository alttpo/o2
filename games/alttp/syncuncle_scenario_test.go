package alttp

import (
	"testing"
)

const (
	ucRoom        = uint16(0x0055) // the secret passage
	ucSlot        = uint16(5)
	ucProgress    = uint16(0xF3C6)
	ucFollower    = uint16(0xF3CC)
	ucAIState     = uint16(0x0D80)
	ucGraphics    = uint16(0x0DC0)
	ucSubtype2    = uint16(0x0E80)
	ucE           = uint16(0x0E90)
	ucSettleFrame = 64
)

// inPassageWithUncle puts the player in the secret passage with uncle lying in defeat, before talking to him.
func (h *skScenario) inPassageWithUncle(i int) {
	h.inRoom(i, ucRoom)
	e := h.gs[i].e
	e.WRAM[ucProgress] = 0x10
	e.WRAM[ucFollower] = 0x05
	e.WRAM[hpSpriteState+ucSlot] = 0x09
	e.WRAM[hpSpriteType+ucSlot] = 0x73
	e.WRAM[ucE+ucSlot] = 0
	e.WRAM[ucSubtype2+ucSlot] = 1
	e.WRAM[ucAIState+ucSlot] = 0
	e.WRAM[ucGraphics+ucSlot] = 0
}

// talkToUncle does what Uncle_LyingInDefeat and Uncle_GrantEquipment do.
func (h *skScenario) talkToUncle(i int) {
	e := h.gs[i].e
	e.WRAM[ucFollower] = 0
	e.WRAM[ucAIState+ucSlot] = 2
	e.WRAM[ucGraphics+ucSlot] = 1
	e.WRAM[0xF359] = 1
	e.WRAM[0xF35A] = 1
	e.WRAM[ucProgress] |= 0x01
	e.WRAM[0xF3C5] = 1
}

func (h *skScenario) expectUncle(when string, i int, aiState, graphics uint8) {
	h.t.Helper()
	e := h.gs[i].e
	if e.WRAM[ucAIState+ucSlot] != aiState || e.WRAM[ucGraphics+ucSlot] != graphics {
		h.t.Errorf("%s: g%d: expected uncle in AI state %d with graphics %d; got %d and %d",
			when, i+1, aiState, graphics, e.WRAM[ucAIState+ucSlot], e.WRAM[ucGraphics+ucSlot])
	}
}

func TestUncleScenario_GearTakenWhenRemotePlayerReceivesIt(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inPassageWithUncle(0)
	h.inPassageWithUncle(1)
	h.frames(ucSettleFrame)
	h.expectUncle("initial", 1, 0, 0)

	h.talkToUncle(0)
	h.frames(ucSettleFrame)

	h.expectUncle("after g1 received the gear", 0, 2, 1)
	h.expectUncle("after g1 received the gear", 1, 2, 1)
	if v := h.gs[1].e.WRAM[ucProgress]; v&0x01 == 0 {
		t.Errorf("g2: expected uncle's gear progress flag; got $%02x", v)
	}
	if v := h.gs[1].e.WRAM[ucFollower]; v != 0 {
		t.Errorf("g2: expected no follower; got $%02x", v)
	}
}

func TestUncleScenario_UncleLeftAloneWhileOnOverworld(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inPassageWithUncle(0)
	h.inPassageWithUncle(1)
	h.onOverworld(1, 0)
	h.gs[1].e.WRAM[0x1B] = 0
	h.frames(ucSettleFrame)

	h.talkToUncle(0)
	h.frames(ucSettleFrame)

	h.expectUncle("on the overworld", 1, 0, 0)
	if v := h.gs[1].e.WRAM[ucProgress]; v&0x01 == 0 {
		t.Errorf("g2: expected uncle's gear progress flag; got $%02x", v)
	}
}
