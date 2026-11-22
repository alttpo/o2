package alttp

import (
	"encoding/binary"
	"testing"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

const (
	rpRoom      = uint16(0x0099)
	rpOtherRoom = uint16(0x009A)
	// rpCaller is a `JSL DeleteRupeeTile : STP` in the test ROM, for a player to pick up a rupee:
	rpCaller = uint32(0x00F000)
	// the tile and attribute DeleteRupeeTile leaves where a rupee was:
	rpFloorTile = uint16(0x190F)
	rpFloorAttr = uint8(0x00)
	// the rupee tiles drawn by the test:
	rpRupeeTile = uint16(0x0D78)
	rpSettle    = 24
)

// writeTestDeleteRupeeTile writes a version of DeleteRupeeTile at its address in the ROM's version of the game: it
// replaces the rupee's two tiles and attributes like the game does, but queues no VRAM stripe; it ends with the code
// the patch hooks (rupeeHookExpected), and a `JSL DeleteRupeeTile : STP` at rpCaller.
func writeTestDeleteRupeeTile(rom *snes.ROM) {
	fns := map[romFunction]uint32{}
	fillRomFunctions(rom, fns)
	fn := fns[fnDeleteRupeeTile]
	pak, err := lorom.BusAddressToPak(fn)
	if err != nil {
		panic(err)
	}
	a := asm.NewEmitter(rom.Slice(pak, rupeeHookOffset+uint32(len(rupeeHookExpected))), true)
	a.SetBase(fn)
	a.AssumeSEP(0x30)
	a.PHB()
	a.REP(0x30)
	a.LDA_dp(0x00)
	a.AND_imm16_w(0x01F8)
	a.ASL()
	a.ASL()
	a.ASL()
	a.STA_dp(0x06)
	a.LDA_dp(0x02)
	a.AND_imm16_w(0x01F8)
	a.LSR()
	a.LSR()
	a.LSR()
	a.ORA_dp(0x06)
	a.ASL()
	a.STA_dp(0x06)
	a.TAX()
	a.LDA_imm16_w(rpFloorTile)
	a.STA_long_x(0x7E2000)
	a.STA_long_x(0x7E2080)
	a.TXA()
	a.LSR()
	a.TAX()
	a.SEP(0x20)
	a.LDA_imm8_b(rpFloorAttr)
	a.STA_long_x(0x7F2000)
	a.STA_long_x(0x7F2040)
	a.SEP(0x30)
	a.LDA_abs(0x0403)
	a.ORA_imm8_b(0x10)
	a.STA_abs(0x0403)
	for a.PC() < fn+rupeeHookOffset {
		a.NOP()
	}
	a.EmitBytes(rupeeHookExpected)
	if err = a.Finalize(); err != nil {
		panic(err)
	}

	pak, err = lorom.BusAddressToPak(rpCaller)
	if err != nil {
		panic(err)
	}
	a = asm.NewEmitter(rom.Slice(pak, 5), true)
	a.SetBase(rpCaller)
	a.JSL(fn)
	a.STP()
	if err = a.Finalize(); err != nil {
		panic(err)
	}
}

// rpPos is the tilemap offset of the 8x8 tile at x, y.
func rpPos(x, y uint16) uint16 { return (y>>3<<6 | x>>3) << 1 }

// drawRupees draws rupees at the tile positions of a rupee floor into player i's tilemap, with their attributes.
func (h *skScenario) drawRupees(i int, pos ...uint16) {
	e := h.gs[i].e
	for _, p := range pos {
		for _, o := range []uint32{0, 0x80} {
			binary.LittleEndian.PutUint16(e.WRAM[0x2000+uint32(p)+o:], rpRupeeTile)
			e.WRAM[0x12000+uint32(p>>1)+o>>1] = rupeeTileAttr
		}
	}
}

// pickUpRupee does what Link touching a rupee does: DeleteRupeeTile with $00/$02 = Y/X.
func (h *skScenario) pickUpRupee(i int, x, y uint16) {
	h.t.Helper()
	e := h.gs[i].e
	binary.LittleEndian.PutUint16(e.WRAM[0x00:], y)
	binary.LittleEndian.PutUint16(e.WRAM[0x02:], x)
	e.CPU.Reset()
	e.SetPC(rpCaller)
	if !e.RunUntil(rpCaller+4, 0x1_000) {
		h.t.Fatalf("DeleteRupeeTile did not return; PC=$%06x", e.GetPC())
	}
}

func (h *skScenario) rupeeThere(i int, pos uint16) bool {
	e := h.gs[i].e
	return binary.LittleEndian.Uint16(e.WRAM[0x2000+uint32(pos):]) == rpRupeeTile &&
		e.WRAM[0x12000+uint32(pos>>1)] == rupeeTileAttr
}

func (h *skScenario) expectRupees(when string, i int, there bool, pos ...uint16) {
	h.t.Helper()
	for _, p := range pos {
		if got := h.rupeeThere(i, p); got != there {
			h.t.Errorf("%s: g%d: rupee at $%04x there = %v; expected %v", when, i+1, p, got, there)
		}
	}
}

// A rupee picked up by one player disappears for the other players in the room, who don't get the rupees for it.
func TestRupeesScenario_PickedUpRemotely(t *testing.T) {
	h := newSKScenario(t, 2)
	a, b, c := rpPos(0x150, 0x30), rpPos(0x160, 0x30), rpPos(0x150, 0x48)
	for i := range h.gs {
		h.inRoom(i, rpRoom)
		h.drawRupees(i, a, b, c)
	}
	h.frames(rpSettle)
	h.expectRupees("before", 0, true, a, b, c)
	h.expectRupees("before", 1, true, a, b, c)

	h.pickUpRupee(0, 0x150, 0x30)
	h.expectRupees("picked up", 0, false, a)
	if room := binary.LittleEndian.Uint16(h.gs[0].e.SRAM[rupeeLogRoom&0x7FFF:]); room != rpRoom {
		t.Errorf("log room $%03x; expected $%03x", room, rpRoom)
	}
	h.frames(rpSettle)
	h.expectRupees("remote", 1, false, a)
	h.expectRupees("remote", 1, true, b, c)
	if f := h.gs[1].e.WRAM[0x0403]; f&0x10 == 0 {
		t.Errorf("g2: $0403 = $%02x; expected bit $10", f)
	}

	// the second player's pickups go the other way:
	h.pickUpRupee(1, 0x160, 0x30)
	h.pickUpRupee(1, 0x150, 0x48)
	h.frames(rpSettle)
	h.expectRupees("both", 0, false, a, b, c)
}

// Rupees are only deleted in the same room, and a room's log is emptied by a rupee picked up in another room.
func TestRupeesScenario_OtherRoom(t *testing.T) {
	h := newSKScenario(t, 2)
	a := rpPos(0x150, 0x30)
	h.inRoom(0, rpRoom)
	h.inRoom(1, rpOtherRoom)
	h.drawRupees(1, a)
	h.frames(rpSettle)

	h.pickUpRupee(0, 0x150, 0x30)
	h.frames(rpSettle)
	h.expectRupees("other room", 1, true, a)

	// the first player picks one up in the other room; the earlier one is forgotten:
	h.inRoom(0, rpOtherRoom)
	h.drawRupees(0, rpPos(0x40, 0x40))
	h.frames(4)
	h.pickUpRupee(0, 0x40, 0x40)
	h.frames(rpSettle)
	h.expectRupees("same room", 1, true, a)
	if n := binary.LittleEndian.Uint16(h.gs[0].e.SRAM[rupeeLogLen&0x7FFF:]); n != 2 {
		t.Errorf("log length %d; expected 2", n)
	}
}
