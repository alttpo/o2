package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

const (
	mnRoom      = uint16(0x0012)
	mnOtherRoom = uint16(0x0002)
	mnPotPos    = uint16(0x0410)
	mnBlockPos  = uint16(0x0820)
	mnBlockObjX = uint16(0x0008)
	mnPotObjX   = uint16(0x0030)
	mnRoomBelow = uint8(0x22)
)

var mnPotTiles = [4]uint16{0x0D0E, 0x0D1E, 0x4D0E, 0x4D1E}
var mnFloorTiles = [4]uint16{0x0101, 0x0102, 0x0103, 0x0104}

// newManipScenario stubs RoomDraw_16x16Single: draw the slot's replacement tiles at its position and set the
// attributes to $00 (floor).
func newManipScenario(t *testing.T, players int) *skScenario {
	t.Helper()
	h := newSKScenario(t, players)
	for _, gs := range h.gs {
		addr := gs.g.romFunctions[fnRoomDraw16x16Single]
		pak, _ := lorom.BusAddressToPak(addr)
		copy(gs.e.ROM[pak:], []byte{
			0xB9, 0x40, 0x05, // LDA $0540,Y
			0x29, 0xFF, 0x3F, // AND #$3FFF
			0xAA,             // TAX
			0xB9, 0x60, 0x05, // LDA $0560,Y
			0x9F, 0x00, 0x20, 0x7E, // STA $7E2000,X
			0xB9, 0x80, 0x05, // LDA $0580,Y
			0x9F, 0x80, 0x20, 0x7E, // STA $7E2080,X
			0xB9, 0xA0, 0x05, // LDA $05A0,Y
			0x9F, 0x02, 0x20, 0x7E, // STA $7E2002,X
			0xB9, 0xC0, 0x05, // LDA $05C0,Y
			0x9F, 0x82, 0x20, 0x7E, // STA $7E2082,X
			0x8A,             // TXA
			0x4A,             // LSR
			0xAA,             // TAX
			0xA9, 0x00, 0x00, // LDA #$0000
			0x9F, 0x00, 0x20, 0x7F, // STA $7F2000,X
			0x9F, 0x40, 0x20, 0x7F, // STA $7F2040,X
			0x60, // RTS
		})
	}
	return h
}

type mnObj struct {
	typ, objx, pos uint16
	tiles          [4]uint16
}

// loadRoom does what the room loader does for the given manipulable objects.
func (h *skScenario) loadRoom(i int, room uint16, objs ...mnObj) {
	h.inRoom(i, room)
	e := h.gs[i].e
	e.WRAM[0xC000] = mnRoomBelow
	for o := uint32(0); o < 0x4000; o++ {
		e.WRAM[0x2000+o] = 0
		if o < 0x2000 {
			e.WRAM[0x12000+o] = 0
		}
	}
	for k := uint32(0); k < 0x100; k++ {
		e.WRAM[0x0500+k] = 0
	}
	for j, ob := range objs {
		o := uint32(j << 1)
		h.setWRAMU16(i, uint32(manipType)+o, ob.typ)
		h.setWRAMU16(i, uint32(manipObjX)+o, ob.objx)
		h.setWRAMU16(i, uint32(manipPos)+o, ob.pos)
		for t := uint32(0); t < 4; t++ {
			h.setWRAMU16(i, uint32(manipTiles)+t<<5+o, ob.tiles[t])
		}
		if manipIsLiftable(ob.typ) {
			a := 0x12000 + manipAttrIndex(ob.pos)
			e.WRAM[a], e.WRAM[a+1], e.WRAM[a+0x40], e.WRAM[a+0x41] = 0x70+uint8(j), 0x70+uint8(j), 0x70+uint8(j), 0x70+uint8(j)
		}
	}
	h.setWRAMU16(i, uint32(manipCount), uint16(len(objs)<<1))
}

func (h *skScenario) slotOf(i int, objx uint16) uint32 {
	n := uint32(h.wramU16(i, uint32(manipCount)))
	for o := uint32(0); o < n; o += 2 {
		if h.wramU16(i, uint32(manipObjX)+o) == objx {
			return o
		}
	}
	h.t.Fatalf("g%d: no slot with objx $%04x", i+1, objx)
	return 0
}

// liftPot does what lifting a pot does to the room.
func (h *skScenario) liftPot(i int, objx uint16) {
	o := h.slotOf(i, objx)
	pos := h.wramU16(i, uint32(manipPos)+o)
	for t, d := range []uint32{0x00, 0x80, 0x02, 0x82} {
		h.setWRAMU16(i, 0x2000+uint32(pos)+d, h.wramU16(i, uint32(manipTiles)+uint32(t)<<5+o))
	}
	a := 0x12000 + manipAttrIndex(pos)
	e := h.gs[i].e
	e.WRAM[a], e.WRAM[a+1], e.WRAM[a+0x40], e.WRAM[a+0x41] = 0, 0, 0, 0
}

// pushBlock does what pushing a block does, ending with it in its new place.
func (h *skScenario) pushBlock(i int, objx, to uint16) {
	o := h.slotOf(i, objx)
	h.setWRAMU16(i, uint32(manipPos)+o, to)
	h.setWRAMU16(i, uint32(manipType)+o, 0xFFFF)
	h.setWRAMU16(i, 0x2000+uint32(to), 0x0922)
	a := 0x12000 + manipAttrIndex(to)
	e := h.gs[i].e
	e.WRAM[a], e.WRAM[a+1], e.WRAM[a+0x40], e.WRAM[a+0x41] = 0x27, 0x27, 0x27, 0x27
	// PushBlock_CheckForPit toggles $0641 for upper layer blocks:
	if to&0x4000 == 0 {
		e.WRAM[0x0641] ^= 1
	}
}

func (h *skScenario) expectPotGone(when string, i int, objx uint16, tiles [4]uint16) {
	h.t.Helper()
	o := h.slotOf(i, objx)
	pos := h.wramU16(i, uint32(manipPos)+o)
	for t, d := range []uint32{0x00, 0x80, 0x02, 0x82} {
		if actual := h.wramU16(i, 0x2000+uint32(pos)+d); actual != tiles[t] {
			h.t.Errorf("%s: g%d: expected tile $%04x at $%04x; got $%04x", when, i+1, tiles[t], uint32(pos)+d, actual)
		}
	}
	if a := h.gs[i].e.WRAM[0x12000+manipAttrIndex(pos)]; a == 0x70+uint8(o>>1) {
		h.t.Errorf("%s: g%d: expected the pot's attribute to be gone", when, i+1)
	}
}

func (h *skScenario) expectPotThere(when string, i int, objx uint16) {
	h.t.Helper()
	o := h.slotOf(i, objx)
	pos := h.wramU16(i, uint32(manipPos)+o)
	if a := h.gs[i].e.WRAM[0x12000+manipAttrIndex(pos)]; a != 0x70+uint8(o>>1) {
		h.t.Errorf("%s: g%d: expected the pot to still be there; attribute $%02x", when, i+1, a)
	}
}

func TestManipScenario_LiftPot(t *testing.T) {
	h := newManipScenario(t, 2)
	pot := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	h.loadRoom(0, mnRoom, pot)
	h.loadRoom(1, mnRoom, pot)
	h.settleStar(skSettleFrames)

	h.liftPot(0, mnPotObjX)
	h.settleStar(skSettleFrames)
	h.expectPotGone("g1 lifted", 1, mnPotObjX, mnPotTiles)
}

func TestManipScenario_RevealedTilesAndSlotOrder(t *testing.T) {
	h := newManipScenario(t, 2)
	potA := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	potB := mnObj{0x1111, mnPotObjX, mnPotPos + 4, mnPotTiles}
	peg := mnObj{0x4040, 0x0040, 0x0610, [4]uint16{0x19D8, 0x19D9, 0x59D8, 0x59D9}}
	// g2's room has an extra object first (e.g. different room state), so slots differ:
	h.loadRoom(0, mnRoom, potA, potB, peg)
	h.loadRoom(1, mnRoom, mnObj{0x0000, 0x0010, 0x0C20, mnFloorTiles}, potA, potB, peg)
	h.settleStar(skSettleFrames)

	// g1's pot hid a hole; the replacement tiles changed before it was drawn:
	o := h.slotOf(0, mnPotObjX) + 2
	for t := uint32(0); t < 4; t++ {
		h.setWRAMU16(0, uint32(manipTiles)+t<<5+o, 0x0480+uint16(t))
	}
	pos := h.wramU16(0, uint32(manipPos)+o)
	for t, d := range []uint32{0x00, 0x80, 0x02, 0x82} {
		h.setWRAMU16(0, 0x2000+uint32(pos)+d, 0x0480+uint16(t))
	}
	a := 0x12000 + manipAttrIndex(pos)
	h.gs[0].e.WRAM[a] = 0x20
	h.liftPot(0, peg.objx)
	h.settleStar(skSettleFrames * 2)

	// potB at mnPotPos+4 is revealed with the hole tiles; potA stays:
	for t, d := range []uint32{0x00, 0x80, 0x02, 0x82} {
		if actual := h.wramU16(1, 0x2000+uint32(mnPotPos+4)+d); actual != 0x0480+uint16(t) {
			t := t
			h.t.Errorf("g2: expected hole tile $%04x; got $%04x", 0x0480+t, actual)
		}
	}
	h.expectPotThere("g2", 1, mnPotObjX)
	h.expectPotGone("g2 peg", 1, peg.objx, peg.tiles)
}

func TestManipScenario_OtherRoom(t *testing.T) {
	h := newManipScenario(t, 2)
	pot := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	h.loadRoom(0, mnRoom, pot)
	h.loadRoom(1, mnOtherRoom, pot)
	h.settleStar(skSettleFrames)

	h.liftPot(0, mnPotObjX)
	h.settleStar(skSettleFrames)
	h.expectPotThere("other room", 1, mnPotObjX)
}

func TestManipScenario_EnterRoomCatchesUp(t *testing.T) {
	h := newManipScenario(t, 2)
	pot := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	h.loadRoom(0, mnRoom, pot)
	h.loadRoom(1, mnOtherRoom, pot)
	h.settleStar(skSettleFrames)

	h.liftPot(0, mnPotObjX)
	h.settleStar(skSettleFrames)

	h.gs[1].e.WRAM[skSubModule] = 0x02
	h.settleStar(2)
	h.loadRoom(1, mnRoom, pot)
	h.settleStar(skSettleFrames)
	h.expectPotGone("g2 entered", 1, mnPotObjX, mnPotTiles)
}

func TestManipScenario_PushBlock(t *testing.T) {
	h := newManipScenario(t, 2)
	block := mnObj{0x0000, mnBlockObjX, mnBlockPos, mnFloorTiles}
	h.loadRoom(0, mnRoom, block)
	h.loadRoom(1, mnRoom, block)
	h.settleStar(skSettleFrames)

	h.pushBlock(0, mnBlockObjX, mnBlockPos+4)
	h.settleStar(skSettleFrames)

	if v := h.wramU16(1, uint32(manipPos)); v != mnBlockPos+4 {
		t.Errorf("g2: expected block at $%04x; got $%04x", mnBlockPos+4, v)
	}
	if v := h.wramU16(1, uint32(manipType)); v != 0x0000 {
		t.Errorf("g2: expected block done moving like PushBlock_Main leaves it ($0000); got $%04x", v)
	}
	for k, d := range []uint32{0x00, 0x80, 0x02, 0x82} {
		if v := h.wramU16(1, 0x2000+uint32(mnBlockPos)+d); v != mnFloorTiles[k] {
			t.Errorf("g2: expected floor tile $%04x where the block was; got $%04x", mnFloorTiles[k], v)
		}
		if v := h.wramU16(1, 0x2000+uint32(mnBlockPos+4)+d); v != [4]uint16{0x0922, 0x0932, 0x0923, 0x0933}[k] {
			t.Errorf("g2: expected block tile at new position; got $%04x", v)
		}
	}
	if a := h.wramU16(1, 0x12000+manipAttrIndex(mnBlockPos+4)); a != 0x2727 {
		t.Errorf("g2: expected block attributes $2727; got $%04x", a)
	}
	// the push block door tags ($0B-$13) open the trap doors from $0641:
	if v := h.gs[1].e.WRAM[0x0641]; v != 1 {
		t.Errorf("g2: expected $0641 toggled to 1 like g1's; got $%02x", v)
	}
	if v := h.gs[1].e.WRAM[0x0642]; v != 0 {
		t.Errorf("g2: expected $0642 untouched; got $%02x", v)
	}

	// nothing moves afterwards:
	h.settleStar(skSettleFrames * 4)
	if v := h.wramU16(1, uint32(manipPos)); v != mnBlockPos+4 {
		t.Errorf("g2: expected block to stay at $%04x; got $%04x", mnBlockPos+4, v)
	}
	if v := h.wramU16(0, uint32(manipPos)); v != mnBlockPos+4 {
		t.Errorf("g1: expected block to stay at $%04x; got $%04x", mnBlockPos+4, v)
	}
	if v0, v1 := h.gs[0].e.WRAM[0x0641], h.gs[1].e.WRAM[0x0641]; v0 != 1 || v1 != 1 {
		t.Errorf("expected $0641 = 1 for both; got $%02x, $%02x", v0, v1)
	}
}

func TestManipScenario_PushBlockIntoPit(t *testing.T) {
	h := newManipScenario(t, 2)
	block := mnObj{0x0000, mnBlockObjX, mnBlockPos, mnFloorTiles}
	h.loadRoom(0, mnRoom, block)
	h.loadRoom(1, mnRoom, block)
	// a pit below the block:
	for _, i := range []int{0, 1} {
		a := 0x12000 + manipAttrIndex(mnBlockPos+0x100)
		h.gs[i].e.WRAM[a] = 0x20
	}
	h.settleStar(skSettleFrames)

	// g1's block falls in; it ends up stationary at the pit and recorded for the room below:
	o := h.slotOf(0, mnBlockObjX)
	h.setWRAMU16(0, uint32(manipPos)+o, mnBlockPos+0x100)
	h.setWRAMU16(0, (pushBlockTable&0x1FFFF)+uint32(mnBlockObjX), uint16(mnRoomBelow))
	h.setWRAMU16(0, (pushBlockTable&0x1FFFF)+uint32(mnBlockObjX)+2, mnBlockPos+0x100)
	h.settleStar(skSettleFrames)

	if v := h.wramU16(1, uint32(manipPos)); v != mnBlockPos+0x100 {
		t.Errorf("g2: expected block at $%04x; got $%04x", mnBlockPos+0x100, v)
	}
	if v := h.wramU16(1, uint32(manipType)); v != 0x0000 {
		t.Errorf("g2: expected block state 0 after falling; got $%04x", v)
	}
	if v := h.wramU16(1, (pushBlockTable&0x1FFFF)+uint32(mnBlockObjX)); v != uint16(mnRoomBelow) {
		t.Errorf("g2: expected push block table room $%02x; got $%04x", mnRoomBelow, v)
	}
	if v := h.wramU16(1, (pushBlockTable&0x1FFFF)+uint32(mnBlockObjX)+2); v != mnBlockPos+0x100 {
		t.Errorf("g2: expected push block table position $%04x; got $%04x", mnBlockPos+0x100, v)
	}
	if v := h.wramU16(1, 0x2000+uint32(mnBlockPos+0x100)); v != 0 {
		t.Errorf("g2: expected no block drawn over the pit; got $%04x", v)
	}
}

func TestAlttpoCompat_ManipFromAlttpo(t *testing.T) {
	h := newManipScenario(t, 1)
	g := h.gs[0].g
	pot := mnObj{0x1111, mnPotObjX, mnPotPos, mnPotTiles}
	h.loadRoom(0, mnRoom, pot)
	h.settleStar(skSettleFrames)

	frame := uint8(0)
	send := func(flags uint8) {
		frame++
		p := newAlttpoPacket(3, frame)
		p.location(0x07, skEastern, 0x010000|uint32(mnRoom), 0, 0)
		// StarSwitchLocal::serialize then ManipSync::serialize:
		p.u8(0x19).u16(mnRoom).u32(0).u8(0, 0)
		p.u8(0x18).u16(mnRoom).u8(1).u16(pot.typ).u16(pot.objx).u16(pot.pos).u8(flags)
		if flags&manipFlagTiles != 0 {
			for _, tile := range pot.tiles {
				p.u16(tile)
			}
		}
		p.name("alttpo")
		p.send(t, g)
	}

	send(0)
	h.settleStar(skSettleFrames)
	h.expectPotThere("alttpo baseline", 0, mnPotObjX)

	send(manipFlagRevealed | manipFlagTiles)
	h.settleStar(skSettleFrames)
	h.expectPotGone("alttpo lifted", 0, mnPotObjX, mnPotTiles)
}

// A push block door (RoomTag_OperateBlockDoor) only runs while Link is in its quadrant, so a block pushed by another
// player must open the doors here right away; e.g. Ganon's Tower room $8B (NE block door).
func TestManipScenario_PushBlockOpensBlockDoor(t *testing.T) {
	h := newManipScenario(t, 2)
	block := mnObj{0x0000, mnBlockObjX, mnBlockPos, mnFloorTiles}
	for i := range h.gs {
		h.loadRoom(i, mnRoom, block)
		h.gs[i].e.WRAM[0xAF] = 0x0C // NE block door
		h.gs[i].e.WRAM[roomDoorOffs] = 1
	}
	h.settleSubmodules(skSettleFrames, nil)

	// g1 pushes the block in the NE quadrant; its tag opens its doors:
	h.pushBlock(0, mnBlockObjX, mnBlockPos+4)
	h.gs[0].e.WRAM[roomDoorOffs] = 0
	opened := false
	for f := 0; f < skSettleFrames && !opened; f++ {
		h.frame()
		e := h.gs[1].e.WRAM[:]
		if e[skSubModule] == 0x05 {
			opened = true
			if e[roomDoorOffs] != 0 || wram16(e, 0x068E) != 0 || wram16(e, 0x0690) != 0 || e[0x012F] != 0x25 {
				t.Errorf("g2: expected shutters to open; $0468 $%02x, $068E $%04x, $0690 $%04x, $012F $%02x",
					e[roomDoorOffs], wram16(e, 0x068E), wram16(e, 0x0690), e[0x012F])
			}
			e[skSubModule] = 0
		}
	}
	if !opened {
		t.Fatalf("g2: expected the push block door to open")
	}
	h.settleSubmodules(skSettleFrames*2, nil)
	h.expectDoors("block pushed", 0, 0)
	if v := h.gs[1].e.WRAM[0x0641]; v != 1 {
		t.Errorf("g2: expected $0641 = 1; got $%02x", v)
	}
}

// A block in Ganon's Tower room $8B is surrounded by hammer pegs; it must not be moved onto a peg that is still up
// here, only once the peg was pounded (which the remote player's reveal of it does).
func TestManipScenario_PushBlockWaitsForPeg(t *testing.T) {
	h := newManipScenario(t, 2)
	peg := mnObj{0x4040, mnPotObjX, mnBlockPos + 4, mnFloorTiles}
	block := mnObj{0x0000, mnBlockObjX, mnBlockPos, mnFloorTiles}
	// g1's peg is already down (not part of its room any more for this test):
	h.loadRoom(0, mnRoom, block)
	h.loadRoom(1, mnRoom, peg, block)
	h.settleStar(skSettleFrames)

	h.pushBlock(0, mnBlockObjX, mnBlockPos+4)
	h.settleStar(skSettleFrames)
	o := h.slotOf(1, mnBlockObjX)
	if v := h.wramU16(1, uint32(manipPos)+o); v != mnBlockPos {
		t.Errorf("g2: expected the block to wait for the peg; at $%04x", v)
	}

	// even if o2 thinks the peg is gone, the update routine checks the attribute:
	h.gs[1].g.manip.slots[0].revealed = true
	h.settleStar(skSettleFrames)
	if v := h.wramU16(1, uint32(manipPos)+o); v != mnBlockPos {
		t.Errorf("g2: expected the block to wait for the peg's attribute; at $%04x", v)
	}
	if a := h.gs[1].e.WRAM[0x12000+manipAttrIndex(mnBlockPos+4)]; a != 0x70 {
		t.Errorf("g2: expected the peg to stay; attribute $%02x", a)
	}

	// g2 pounds the peg:
	h.liftPot(1, mnPotObjX)
	h.settleStar(skSettleFrames)
	if v := h.wramU16(1, uint32(manipPos)+o); v != mnBlockPos+4 {
		t.Errorf("g2: expected the block at $%04x once the peg is down; got $%04x", mnBlockPos+4, v)
	}
}

// A block pushed onto a floor switch ends up in state $0005, which the hold switch door tag looks for.
func TestManipScenario_PushBlockOntoSwitch(t *testing.T) {
	h := newManipScenario(t, 2)
	block := mnObj{0x0000, mnBlockObjX, mnBlockPos, mnFloorTiles}
	for i := range h.gs {
		h.loadRoom(i, mnRoom, block)
		h.gs[i].e.WRAM[0x12000+manipAttrIndex(mnBlockPos+4)] = 0x23 // floor switch
		h.gs[i].e.WRAM[roomDoorOffs] = 1
	}
	h.settleStar(skSettleFrames)

	h.pushBlock(0, mnBlockObjX, mnBlockPos+4)
	h.setWRAMU16(0, uint32(manipType), 0x0005)
	h.settleStar(skSettleFrames)

	if v := h.wramU16(1, uint32(manipPos)); v != mnBlockPos+4 {
		t.Errorf("g2: expected block at $%04x; got $%04x", mnBlockPos+4, v)
	}
	if v := h.wramU16(1, uint32(manipType)); v != 0x0005 {
		t.Errorf("g2: expected block state $0005 on the switch; got $%04x", v)
	}
	if v := h.wramU16(1, 0x0466); v != 0 {
		t.Errorf("g2: expected $0466 = $0468 ^ 1 = 0; got $%04x", v)
	}
}
