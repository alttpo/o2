package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

const (
	oweTurtleRock = uint16(0x47)
	oweSkullWoods = uint16(0x40)
	oweDesert     = uint16(0x30)
	oweOtherArea  = uint16(0x45)
	oweSlot       = uint16(5)
	// a few frames more than skSettleFrames: overworld flags arrive with the SRAM syncables
	oweSettleFrames = 16
	// test stubs mark that a routine was called by incrementing this byte:
	oweStubCalled = uint32(0x7F5000)
)

// newOweScenario stubs the routines called by the overworld event sync to count their calls at oweStubCalled.
func newOweScenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := newSKScenario(t, players)
	for _, gs := range h.gs {
		for _, fn := range []romFunction{fnSkullWoodsFire, fnOverworldOpenGargoylesDomain, fnOverworldAlterWeathervane, fnOverworldCreatePyramidHole} {
			pak, _ := lorom.BusAddressToPak(gs.g.romFunctions[fn])
			stub := []byte{
				0xAF, 0x00, 0x50, 0x7F, // LDA.l $7F5000
				0x1A,                   // INC
				0x8F, 0x00, 0x50, 0x7F, // STA.l $7F5000
			}
			if fn == fnSkullWoodsFire {
				// LDA #$02 : STA $04C6 like FireRodShot_BecomeSkullWoodsFire:
				stub = append(stub, 0xA9, 0x02, 0x8D, 0xC6, 0x04)
			}
			copy(gs.e.ROM[pak:], append(stub, 0x6B)) // RTL
		}
	}
	return h
}

// inOwArea puts the player in an overworld area as freshly loaded, standing on the ground.
func (h *skScenario) inOwArea(i int, area uint16) {
	h.onOverworld(i, 0)
	e := h.gs[i].e
	e.WRAM[0x1B] = 0
	e.WRAM[0x8A] = uint8(area)
	e.WRAM[0x8B] = 0
	e.WRAM[0x0FFF] = uint8(area>>6) & 1
	e.WRAM[0x5D] = 0
	e.WRAM[0x4D] = 0
	e.WRAM[0x02E4] = 0
	e.WRAM[owEntranceOffs] = 0
	e.WRAM[owDesertOffs] = 0
	e.WRAM[0xB0] = 0xFF
	e.WRAM[0xC8] = 0xFF
	e.WRAM[oweStubCalled-0x7E0000] = 0
	for s := uint16(0); s < 0x10; s++ {
		e.WRAM[0x0DD0+s] = 0
	}
}

func (h *skScenario) stubCalls(i int) uint8 {
	return h.gs[i].e.WRAM[oweStubCalled-0x7E0000]
}

// putSprite puts a sprite in slot oweSlot.
func (h *skScenario) putSprite(i int, typ, gfx uint8) {
	e := h.gs[i].e
	e.WRAM[0x0DD0+oweSlot] = 0x09
	e.WRAM[0x0E20+oweSlot] = typ
	e.WRAM[0x0DC0+oweSlot] = gfx
	e.WRAM[0x0D80+oweSlot] = 0
	e.WRAM[0x0F80+oweSlot] = 0
}

func (h *skScenario) expectWRAM(when string, i int, addr uint16, v uint8) {
	h.t.Helper()
	if actual := h.gs[i].e.WRAM[addr]; actual != v {
		h.t.Errorf("%s: g%d: expected $%04x = $%02x; got $%02x", when, i+1, addr, v, actual)
	}
}

// startEntrance does what Overworld_AnimateEntrance does at its start (setting the flag right away, like Turtle Rock).
func (h *skScenario) startEntrance(i int, entrance uint8) {
	e := h.gs[i].e
	e.WRAM[owEntranceOffs] = entrance
	e.WRAM[0xB0] = 0
	e.WRAM[0xC8] = 0
	e.WRAM[0xF280+uint16(e.WRAM[0x8A])] |= 0x20
}

func TestOwEventsScenario_EntranceAnimationPlaysForPlayersInTheArea(t *testing.T) {
	h := newOweScenario(t, 3)
	h.inOwArea(0, oweTurtleRock)
	h.inOwArea(1, oweTurtleRock)
	h.inOwArea(2, oweOtherArea)
	h.frames(oweSettleFrames)

	h.startEntrance(0, 4)
	h.frames(oweSettleFrames)
	h.expectWRAM("g1 opened Turtle Rock", 1, owEntranceOffs, 4)
	h.expectWRAM("g1 opened Turtle Rock", 1, 0xB0, 0)
	h.expectWRAM("g1 opened Turtle Rock", 1, 0xC8, 0)
	// g3 is in another area:
	h.expectWRAM("g1 opened Turtle Rock", 2, owEntranceOffs, 0)

	// the animations end; nothing starts again although g1 keeps sending for a while:
	h.gs[0].e.WRAM[owEntranceOffs] = 0
	h.gs[1].e.WRAM[owEntranceOffs] = 0
	h.frames(oweSettleFrames)
	h.expectWRAM("after the animations", 1, owEntranceOffs, 0)
	h.expectWRAM("after the animations", 0, owEntranceOffs, 0)
}

func TestOwEventsScenario_EntranceNotReplayedWhenOverlayLoaded(t *testing.T) {
	h := newOweScenario(t, 2)
	h.inOwArea(0, oweTurtleRock)
	h.inOwArea(1, oweTurtleRock)
	// g2 loaded the area with the entrance already open:
	h.gs[1].e.WRAM[0xF280+oweTurtleRock] |= 0x20
	h.frames(oweSettleFrames)

	h.startEntrance(0, 4)
	h.frames(oweSettleFrames)
	h.expectWRAM("overlay was loaded", 1, owEntranceOffs, 0)
}

func TestOwEventsScenario_EntranceWaitsForLinkOnTheGround(t *testing.T) {
	h := newOweScenario(t, 2)
	h.inOwArea(0, oweTurtleRock)
	h.inOwArea(1, oweTurtleRock)
	h.frames(oweSettleFrames)

	// g2 is swimming:
	h.gs[1].e.WRAM[0x5D] = 0x04
	h.startEntrance(0, 4)
	h.frames(oweSettleFrames)
	h.expectWRAM("swimming", 1, owEntranceOffs, 0)

	h.gs[1].e.WRAM[0x5D] = 0x00
	h.frames(oweSettleFrames)
	h.expectWRAM("back on the ground", 1, owEntranceOffs, 4)
}

func TestOwEventsScenario_SkullWoodsFire(t *testing.T) {
	h := newOweScenario(t, 2)
	h.inOwArea(0, oweSkullWoods)
	h.inOwArea(1, oweSkullWoods)
	h.frames(oweSettleFrames)

	h.startEntrance(0, 2)
	h.frames(oweSettleFrames)
	if h.stubCalls(1) != 1 {
		t.Errorf("g2: expected FireRodShot_BecomeSkullWoodsFire called once; got %d", h.stubCalls(1))
	}
	h.expectWRAM("g1 burned Skull Woods", 1, owEntranceOffs, 2)
}

func TestOwEventsScenario_DesertStatues(t *testing.T) {
	h := newOweScenario(t, 3)
	h.inOwArea(0, oweDesert)
	h.inOwArea(1, oweDesert)
	h.inOwArea(2, oweOtherArea)
	h.frames(oweSettleFrames)

	h.gs[0].e.WRAM[owDesertOffs] = 1
	h.frames(oweSettleFrames)
	h.expectWRAM("g1 read the tablet", 1, owDesertOffs, 1)
	h.expectWRAM("g1 read the tablet", 2, owDesertOffs, 0)

	// a player entering the area later while g1 is still there sees the statues move too:
	h.inOwArea(2, oweDesert)
	h.frames(oweSettleFrames)
	h.expectWRAM("g3 entered the area", 2, owDesertOffs, 1)
}

func TestOwEventsScenario_LumberjackTree(t *testing.T) {
	h := newOweScenario(t, 2)
	h.inOwArea(0, owLumberjackArea)
	h.inOwArea(1, owLumberjackArea)
	h.putSprite(0, owTreeSprite, 2)
	h.putSprite(1, owTreeSprite, 2)
	h.frames(oweSettleFrames)
	h.expectWRAM("initial", 1, 0x0D80+oweSlot, 0)

	// g1 dashes into the tree:
	h.gs[0].e.WRAM[0x0D80+oweSlot] = 1
	h.gs[0].e.WRAM[0x0F80+oweSlot] = 20
	h.frames(oweSettleFrames)
	h.expectWRAM("g1 dashed into the tree", 1, 0x0D80+oweSlot, 1)
	h.expectWRAM("g1 dashed into the tree", 1, 0x0F80+oweSlot, 20)

	// the tree falls apart; it is not made to hop again:
	h.gs[1].e.WRAM[0x0D80+oweSlot] = 2
	h.frames(oweSettleFrames)
	h.expectWRAM("tree fell apart", 1, 0x0D80+oweSlot, 2)
}

func TestOwEventsScenario_ThievesTownGrate(t *testing.T) {
	h := newOweScenario(t, 3)
	h.inOwArea(0, 0x58)
	h.inOwArea(1, 0x58)
	h.inOwArea(2, oweOtherArea)
	h.putSprite(1, 0x14, 0)
	h.frames(oweSettleFrames)

	// g1 pulls the grate (OpenGargoylesDomain):
	h.gs[0].e.WRAM[0xF280+0x58] |= 0x20
	h.gs[0].e.WRAM[0x0DD0+oweSlot] = 0
	h.frames(oweSettleFrames)
	if h.stubCalls(1) != 1 {
		t.Errorf("g2: expected OpenGargoylesDomain called once; got %d", h.stubCalls(1))
	}
	h.expectWRAM("g1 pulled the grate", 1, 0x0DD0+oweSlot, 0)
	// g3 only gets the flag:
	if h.stubCalls(2) != 0 {
		t.Errorf("g3: expected OpenGargoylesDomain not called; got %d", h.stubCalls(2))
	}
	if h.gs[2].e.WRAM[0xF280+0x58]&0x20 == 0 {
		t.Errorf("g3: expected the overlay flag of area $58 to be set")
	}
}

func TestOwEventsScenario_AgahnimsBarrier(t *testing.T) {
	h := newOweScenario(t, 2)
	h.inOwArea(0, 0x1B)
	h.inOwArea(1, 0x1B)
	h.putSprite(1, 0x40, 0)
	h.frames(oweSettleFrames)

	h.gs[0].e.WRAM[0xF280+0x1B] |= 0x40
	h.frames(oweSettleFrames)
	h.expectWRAM("g1 destroyed the barrier", 1, 0x0DC0+oweSlot, 4)
	h.expectWRAM("g1 destroyed the barrier", 1, 0x0DD0+oweSlot, 0x09)
}
