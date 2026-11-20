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

func TestEnemiesKilledScenario_UnderworldKillsLive(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	// g2 has live sprites with indexes 2 and 5 in slots 1 and 4:
	e2 := h.gs[1].e.WRAM[:]
	e2[0x0DD0+1], e2[0x0BC0+1] = 0x09, 2
	e2[0x0DD0+4], e2[0x0BC0+4] = 0x09, 5
	h.settleRoom(skSettleFrames)

	// g1 kills sprite 2:
	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0004)
	h.settleRoom(skSettleFrames)
	if e2[0x0DD0+1] != 0x06 || e2[0x0DF0+1] != 0x1F {
		t.Errorf("g2: expected sprite index 2 in slot 1 dying with the poof timer, got state $%02x timer $%02x", e2[0x0DD0+1], e2[0x0DF0+1])
	}
	if e2[0x0DD0+4] == 0 {
		t.Errorf("g2: expected sprite index 5 in slot 4 left alone")
	}
}

func TestEnemiesKilledScenario_UnderworldKillDeadEnemiesOff(t *testing.T) {
	h := newSSScenario(t, 2)
	h.gs[1].g.KillDeadEnemies = false
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	e2 := h.gs[1].e.WRAM[:]
	e2[0x0DD0+1], e2[0x0BC0+1] = 0x09, 2
	h.settleRoom(skSettleFrames)
	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0004)
	h.settleRoom(skSettleFrames)
	if e2[0x0DD0+1] == 0 {
		t.Errorf("g2: expected the live sprite left alone")
	}
}

func TestEnemiesKilledScenario_OverworldKillsLive(t *testing.T) {
	h := newOweScenario(t, 2)
	h.ekInArea(0, ekArea)
	h.ekInArea(1, ekArea)
	h.frames(oweSettleFrames)

	e2 := h.gs[1].e.WRAM[:]
	ekSetLoaded(e2, ekBlkKilled)
	e2[0x0DD0+2] = 0x09
	setWram16(e2, 0x0BC0+2*2, ekBlkKilled)
	ekSetLoaded(h.gs[0].e.WRAM[:], ekBlkKilled)
	h.frames(oweSettleFrames)
	if e2[0x0DD0+2] != 0x06 || e2[0x0DF0+2] != 0x1F {
		t.Errorf("g2: expected the live sprite at the killed position dying with the poof timer")
	}
}

func TestEnemiesKilledScenario_UnderworldLeavesDropsAlone(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	// g2's slot 1 became a small key drop ($E4) and slot 3 a heart ($D8), both keeping the room index of the enemy
	// they replaced; slot 4 is still a stalfos:
	e2 := h.gs[1].e.WRAM[:]
	e2[0x0DD0+1], e2[0x0BC0+1], e2[0x0E20+1] = 0x09, 2, 0xE4
	e2[0x0DD0+3], e2[0x0BC0+3], e2[0x0E20+3] = 0x09, 2, 0xD8
	e2[0x0DD0+4], e2[0x0BC0+4], e2[0x0E20+4] = 0x09, 2, 0xA7
	h.settleRoom(skSettleFrames)

	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0x0004)
	h.settleRoom(skSettleFrames)
	if e2[0x0DD0+1] != 0x09 || e2[0x0DD0+3] != 0x09 {
		t.Errorf("g2: expected the key and heart drops left alone, got states $%02x $%02x", e2[0x0DD0+1], e2[0x0DD0+3])
	}
	if e2[0x0DD0+4] != 0x06 {
		t.Errorf("g2: expected the stalfos killed, got state $%02x", e2[0x0DD0+4])
	}
}

func TestEnemiesKilledScenario_UnderworldKillsAllSlots(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	// all 16 sprite slots alive with the room's sprite indexes 0-15, all killed by g1; the routine is long and its
	// guards must reach the end of it:
	e2 := h.gs[1].e.WRAM[:]
	for x := 0; x < 16; x++ {
		e2[0x0DD0+x], e2[0x0BC0+x], e2[0x0E20+x] = 0x09, uint8(x), 0xA7
	}
	h.settleRoom(skSettleFrames)
	setWram16(h.gs[0].e.WRAM[:], enemiesKilledRoomMasks+uint32(ekRoom)*2, 0xFFFF)
	h.settleRoom(skSettleFrames)
	for x := 0; x < 16; x++ {
		if e2[0x0DD0+x] != 0x06 {
			t.Errorf("g2: expected slot %d dying, got state $%02x", x, e2[0x0DD0+x])
		}
	}
}

func TestEnemiesKilledScenario_UnderworldKeyCarrier(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekRoom)
	h.settleRoom(skSettleFrames)

	e2 := h.gs[1].e.WRAM[:]
	e2[0x0DD0+2], e2[0x0BC0+2], e2[0x0E20+2] = 0x09, 3, 0x84
	h.settleRoom(skSettleFrames)

	// g1 killed the eyegore of sprite index 3 that carried a key: its slot is now the key drop, no death flag is set:
	e1 := h.gs[0].e.WRAM[:]
	e1[0x0DD0+5], e1[0x0BC0+5], e1[0x0E20+5], e1[0x0E30+5] = 0x09, 0xFF, 0xE4, 3
	h.settleRoom(skSettleFrames)
	if e2[0x0DD0+2] != 0x06 {
		t.Errorf("g2: expected the key carrier killed, got state $%02x", e2[0x0DD0+2])
	}
}

func TestEnemiesKilledScenario_UnderworldRecentRoom(t *testing.T) {
	h := newSSScenario(t, 2)
	h.ekInRoom(0, ekRoom)
	h.ekInRoom(1, ekOtherRoom)
	h.settleRoom(skSettleFrames)

	// g1 is in ekRoom and remembers ekOtherRoom (where it killed enemies) as the room visited before; g2 is in
	// ekOtherRoom, which g1 has left:
	e1 := h.gs[0].e.WRAM[:]
	setWram16(e1, enemiesKilledRecentAddr+0, ekRoom)
	setWram16(e1, enemiesKilledRecentAddr+2, ekOtherRoom)
	setWram16(e1, enemiesKilledRecentAddr+4, 0xFFFF)
	setWram16(e1, enemiesKilledRecentAddr+6, 0xFFFF)
	setWram16(e1, enemiesKilledRoomMasks+uint32(ekOtherRoom)*2, 0x0006)

	e2 := h.gs[1].e.WRAM[:]
	e2[0x0DD0+1], e2[0x0BC0+1], e2[0x0E20+1] = 0x09, 1, 0xA7
	e2[0x0DD0+3], e2[0x0BC0+3], e2[0x0E20+3] = 0x09, 3, 0xA7
	h.settleRoom(skSettleFrames * 4)
	h.expectRoomMask("recent room", ekOtherRoom, 0x0006, 0x0006)
	if e2[0x0DD0+1] != 0x06 || e2[0x0DD0+3] != 0x09 {
		t.Errorf("g2: expected the sprite of index 1 killed and index 3 left alone, got states $%02x $%02x", e2[0x0DD0+1], e2[0x0DD0+3])
	}
}
