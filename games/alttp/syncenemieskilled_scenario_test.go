package alttp

import "testing"

const (
	ekRoom      = uint16(0x0098)
	ekOtherRoom = uint16(0x00A8)
	ekArea      = uint16(0x0018)
	ekOtherArea = uint16(0x0028)
	ekBlkKilled = uint16(0x0123) // a killed overworld sprite position
	ekBlkAlive  = uint16(0x0045) // a live overworld sprite position
	ekBlkOlord  = uint16(0x0067) // a live overlord position
)

// ekInRoom puts the player in a freshly loaded dungeon room with no enemy killed.
func (h *skScenario) ekInRoom(i int, room uint16) {
	h.inRoom(i, room)
	e := h.gs[i].e
	setWram16(e.WRAM[:], 0x048E, room)
	for r := uint32(0); r < 0x128; r++ {
		setWram16(e.WRAM[:], enemiesKilledRoomMasks+r*2, 0)
	}
}

// ekInArea puts the player in a freshly loaded overworld area with no sprite loaded.
func (h *skScenario) ekInArea(i int, area uint16) {
	h.inOwArea(i, area)
	e := h.gs[i].e
	for k := uint32(0); k < 0x10; k++ {
		e.WRAM[0x0DD0+k] = 0
		setWram16(e.WRAM[:], 0x0BC0+k*2, 0xFFFF)
	}
	for k := uint32(0); k < 8; k++ {
		e.WRAM[0x0B00+k] = 0
		setWram16(e.WRAM[:], 0x0B48+k*2, 0xFFFF)
	}
	for b := uint32(0); b < 0x200; b++ {
		e.WRAM[enemiesKilledOwLoaded+b] = 0
	}
}

func ekLoaded(e []byte, blk uint16) bool {
	return e[enemiesKilledOwLoaded+uint32(blk>>3)]&(0x80>>(blk&7)) != 0
}

func ekSetLoaded(e []byte, blk uint16) {
	e[enemiesKilledOwLoaded+uint32(blk>>3)] |= 0x80 >> (blk & 7)
}

func (h *skScenario) expectRoomMask(when string, room uint16, expected ...uint16) {
	h.t.Helper()
	for i, x := range expected {
		if actual := wram16(h.gs[i].e.WRAM[:], enemiesKilledRoomMasks+uint32(room)*2); actual != x {
			h.t.Errorf("%s: g%d: expected room $%03x killed mask $%04x; got $%04x", when, i+1, room, x, actual)
		}
	}
}

func TestEnemiesKilledScenario_Underworld(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	// g1 kills the room's sprites 2 and 9:
	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0204)
	h.settleRoom(skSettleFrames)
	h.expectRoomMask("g1 killed", ekRoom, 0x0204, 0x0204)

	// g2 kills sprite 0:
	setWram16(h.gs[1].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0205)
	h.settleRoom(skSettleFrames)
	h.expectRoomMask("g2 killed", ekRoom, 0x0205, 0x0205)

	// nothing more is written:
	h.settleRoom(skSettleFrames)
	if ek := &h.gs[1].g.enemiesKilled; ek.inflight != nil {
		t.Errorf("g2: expected no update in flight")
	}
}

func TestEnemiesKilledScenario_UnderworldOtherRoom(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekOtherRoom)
	h.settleRoom(skSettleFrames)

	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0001)
	h.settleRoom(skSettleFrames)
	h.expectRoomMask("g1 killed", ekRoom, 0x0001, 0x0000)
	h.expectRoomMask("g1 killed", ekOtherRoom, 0x0000, 0x0000)
}

func TestEnemiesKilledScenario_UnderworldDisabled(t *testing.T) {
	h := newSSScenario(t, 2)
	h.gs[1].g.SyncEnemiesKilled = false
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0001)
	h.settleRoom(skSettleFrames)
	h.expectRoomMask("g1 killed", ekRoom, 0x0001, 0x0000)
}

func TestEnemiesKilledScenario_Overworld(t *testing.T) {
	h := newOweScenario(t, 2)
	h.ekInArea(0, ekArea)
	h.ekInArea(1, ekArea)
	h.frames(oweSettleFrames)

	e := h.gs[0].e.WRAM[:]
	// a live sprite and a live overlord, loaded:
	ekSetLoaded(e, ekBlkAlive)
	e[0x0DD0+3] = 0x09
	setWram16(e, 0x0BC0+3*2, ekBlkAlive)
	ekSetLoaded(e, ekBlkOlord)
	e[0x0B00+1] = 0x05
	setWram16(e, 0x0B48+1*2, ekBlkOlord)
	// a sprite that died: its slot is free and its position stays loaded:
	ekSetLoaded(e, ekBlkKilled)
	h.frames(oweSettleFrames)

	e2 := h.gs[1].e.WRAM[:]
	if !ekLoaded(e2, ekBlkKilled) {
		t.Errorf("g2: expected the killed sprite's position $%03x marked loaded", ekBlkKilled)
	}
	if ekLoaded(e2, ekBlkAlive) || ekLoaded(e2, ekBlkOlord) {
		t.Errorf("g2: expected the live sprite's and overlord's positions left alone")
	}
}

func TestEnemiesKilledScenario_OverworldOtherArea(t *testing.T) {
	h := newOweScenario(t, 2)
	h.ekInArea(0, ekArea)
	h.ekInArea(1, ekOtherArea)
	h.frames(oweSettleFrames)

	ekSetLoaded(h.gs[0].e.WRAM[:], ekBlkKilled)
	h.frames(oweSettleFrames)
	if ekLoaded(h.gs[1].e.WRAM[:], ekBlkKilled) {
		t.Errorf("g2: expected nothing applied in another area")
	}
}
