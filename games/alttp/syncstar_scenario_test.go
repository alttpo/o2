package alttp

import (
	"testing"
)

const (
	ssRoom      = uint16(0x0098) // a Misery Mire room
	ssOtherRoom = uint16(0x00A8)
	ssOverlayA  = uint8(0x0A) // RoomTag_Holes7: star tile type $3A
	ssOverlayB  = uint8(0x0B) // RoomTag_Holes7: star tile type $3B
	ssNMIFlag   = uint16(0x0017)
)

func newSSScenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := newSKScenario(t, players)
	for _, gs := range h.gs {
		// stub ToggleStarTileGraphics: LDA #$18 : STA $17 : RTL
		for i, b := range []byte{0xA9, 0x18, 0x85, 0x17, 0x6B} {
			gs.e.Bus.EaWrite(0x00_FDA7+uint32(i), b)
		}
	}
	return h
}

// inRoom puts the player in a dungeon room as freshly loaded (no star tile stepped on).
func (h *skScenario) inRoom(i int, room uint16) {
	h.inDungeon(i, 0)
	e := h.gs[i].e
	e.WRAM[0x1B] = 1
	e.WRAM[0xA0] = uint8(room)
	e.WRAM[0xA1] = uint8(room >> 8)
	e.WRAM[starOverlayOffs] = 0
	e.WRAM[starSwitchOffs] = 0
	e.WRAM[ssNMIFlag] = 0
}

// stepOnStar does what RoomTag_TriggerHoles does when the player steps on a star tile that changes the overlay.
func (h *skScenario) stepOnStar(i int, overlay uint8) {
	e := h.gs[i].e
	e.WRAM[starOverlayOffs] = overlay
	e.WRAM[skSubModule] = 0x03
	e.WRAM[starSwitchOffs] ^= 1
	e.WRAM[ssNMIFlag] = 0x18
}

// settleStar runs frames, finishing the overlay submodule ($03) the update routine switches to like the game would.
func (h *skScenario) settleStar(n int) {
	for f := 0; f < n; f++ {
		h.frame()
		for _, gs := range h.gs {
			if gs.e.WRAM[skModule] == 0x07 && gs.e.WRAM[skSubModule] == 0x03 {
				gs.e.WRAM[skSubModule] = 0x00
			}
		}
	}
}

func (h *skScenario) expectStar(when string, expected ...uint16) {
	h.t.Helper()
	for i, x := range expected {
		e := h.gs[i].e
		actual := uint16(e.WRAM[starSwitchOffs]) | uint16(e.WRAM[starOverlayOffs])<<8
		if actual != x {
			h.t.Errorf("%s: g%d: expected star switch $%02x overlay $%02x; got $%02x overlay $%02x",
				when, i+1, uint8(x), uint8(x>>8), uint8(actual), uint8(actual>>8))
		}
	}
}

func star(value, overlay uint8) uint16 { return uint16(value) | uint16(overlay)<<8 }

func TestStarSwitchScenario_Toggle(t *testing.T) {
	h := newSSScenario(t, 2)
	h.inRoom(0, ssRoom)
	h.inRoom(1, ssRoom)
	h.settleStar(skSettleFrames)
	h.expectStar("initial", star(0, 0), star(0, 0))

	h.stepOnStar(0, ssOverlayB)
	h.settleStar(skSettleFrames)
	h.expectStar("g1 stepped", star(1, ssOverlayB), star(1, ssOverlayB))
	if v := h.gs[1].e.WRAM[ssNMIFlag]; v != 0x18 {
		t.Errorf("g2: expected ToggleStarTileGraphics to be called; $17 = $%02x", v)
	}

	h.stepOnStar(1, ssOverlayA)
	h.settleStar(skSettleFrames)
	h.expectStar("g2 stepped", star(0, ssOverlayA), star(0, ssOverlayA))

	// nothing flips back and forth afterwards:
	h.settleStar(skSettleFrames * 4)
	h.expectStar("settled", star(0, ssOverlayA), star(0, ssOverlayA))
}

func TestStarSwitchScenario_OtherRoom(t *testing.T) {
	h := newSSScenario(t, 2)
	h.inRoom(0, ssRoom)
	h.inRoom(1, ssOtherRoom)
	h.settleStar(skSettleFrames)

	h.stepOnStar(0, ssOverlayB)
	h.settleStar(skSettleFrames)
	h.expectStar("other room", star(1, ssOverlayB), star(0, 0))
	if h.gs[1].g.players[0].WRAM[starSwitchOffs] != nil {
		t.Errorf("g2: expected star switch state only to be sent to players in the same room")
	}
}

func TestStarSwitchScenario_EnterRoomCatchesUp(t *testing.T) {
	h := newSSScenario(t, 2)
	h.inRoom(0, ssRoom)
	h.inRoom(1, ssOtherRoom)
	h.settleStar(skSettleFrames)

	h.stepOnStar(0, ssOverlayB)
	h.settleStar(skSettleFrames)

	// g2 enters g1's room; the room load resets the state, which must not overwrite g1's:
	h.gs[1].e.WRAM[skSubModule] = 0x02
	h.settleStar(2)
	h.inRoom(1, ssRoom)
	h.settleStar(skSettleFrames)
	h.expectStar("g2 entered", star(1, ssOverlayB), star(1, ssOverlayB))

	// g1 leaves and comes back; its room load must not overwrite g2's state either:
	h.gs[0].e.WRAM[skSubModule] = 0x02
	h.inRoom(0, ssOtherRoom)
	h.gs[0].e.WRAM[skSubModule] = 0x02
	h.settleStar(2)
	h.inRoom(0, ssRoom)
	h.settleStar(skSettleFrames)
	h.expectStar("g1 returned", star(1, ssOverlayB), star(1, ssOverlayB))

	h.settleStar(skSettleFrames * 4)
	h.expectStar("settled", star(1, ssOverlayB), star(1, ssOverlayB))
}

func TestAlttpoCompat_StarSwitchFromAlttpo(t *testing.T) {
	h := newSSScenario(t, 1)
	g := h.gs[0].g
	h.inRoom(0, ssRoom)
	h.settleStar(skSettleFrames)

	frame := uint8(0)
	send := func(value uint16, ts uint32) {
		frame++
		p := newAlttpoPacket(3, frame)
		p.location(0x07, skEastern, 0x010000|uint32(ssRoom), 0, 0)
		// LocalGameState::serialize_star:
		p.u8(0x05, 0x01).u16(starSwitchOffs).u32(ts).u16(value)
		p.name("alttpo")
		p.send(t, g)
	}

	send(0, 0)
	h.settleStar(skSettleFrames)
	h.expectStar("alttpo baseline", star(0, 0))

	ts := timestampFromTime(g.ServerSNESTimestamp()) - 1000
	send(star(1, ssOverlayB), ts)
	h.settleStar(skSettleFrames)
	h.expectStar("alttpo stepped", star(1, ssOverlayB))

	// o2 player steps on a star later; alttpo's older state must not win:
	h.stepOnStar(0, ssOverlayA)
	h.settleStar(skSettleFrames)
	send(star(1, ssOverlayB), ts)
	h.settleStar(skSettleFrames)
	h.expectStar("o2 stepped", star(0, ssOverlayA))
}
