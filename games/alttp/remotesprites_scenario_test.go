package alttp

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/alttpo/snes/emulator"
	"github.com/alttpo/snes/mapping/lorom"
)

const (
	rsRoom      = uint16(0x0012)
	rsOtherRoom = uint16(0x0013)
	// dma_var6/7 values the game left from an earlier frame:
	rsVar6, rsVar7 = uint8(0x40), uint8(0x60)
)

// newRSScenario sets up g1 (the viewer) and g2 (the remote player) in the same room. g2's Link is drawn at (x=$50,
// y=$58) on its screen with head and body sprites from LinkGraphics $8400/$8600 (head) and $9000/$9200 (body).
func newRSScenario(t *testing.T) *skScenario {
	t.Helper()
	h := newSKScenario(t, 2)
	for i := range h.gs {
		h.inRoom(i, rsRoom)
		e := h.gs[i].e
		e.WRAM[0x0AE8], e.WRAM[0x0AEA] = rsVar6, rsVar7
	}
	// g2's world coordinates and scroll; g1's screen is scrolled by (+$10, +$08):
	h.setScroll(1, 0x1050, 0x0250, 0x1000, 0x0200)
	h.setScroll(0, 0x1070, 0x0270, 0x1010, 0x0208)

	// g2's Link: OAM entries start at index $64:
	e := h.gs[1].e
	e.WRAM[0x0352], e.WRAM[0x0353] = 0x90, 0x01
	for i := 0; i < 0x80; i++ {
		e.WRAM[0x0800+i*4+1] = 0xF0
	}
	setOAM := func(i int, x, y, chr, attr, ext uint8) {
		copy(e.WRAM[0x0800+i*4:], []byte{x, y, chr, attr})
		e.WRAM[0x0A20+i] = ext
	}
	// head in front of the body (lower OAM index), body, then Link's shadow (two 8x8 sprites, the second flipped):
	setOAM(0x64, 0x50, 0x58, 0x00, 0x2E, 0x02)
	setOAM(0x65, 0x50, 0x60, 0x02, 0x6E, 0x02)
	setOAM(0x66, 0x50, 0x68, 0x6C, 0x28, 0x00)
	setOAM(0x67, 0x58, 0x68, 0x6C, 0x68, 0x00)
	for k, v := range []uint16{0x8400, 0x8600, 0x9000, 0x9200, 0x9400, 0x9420} {
		binary.LittleEndian.PutUint16(e.WRAM[0x0ACC+k*2:], v)
	}

	// g1's game clears the OAM buffer every frame (ClearOAMBuffer); the test module draws nothing:
	g1 := h.gs[0]
	g1.beforeEmulate = func() {
		for i := 0; i < 0x80; i++ {
			copy(g1.e.WRAM[0x0800+i*4:], []byte{0, 0xF0, 0, 0})
			g1.e.WRAM[0x0A20+i] = 0
		}
	}
	return h
}

func (h *skScenario) oamEntry(i, index int) (entry [4]byte, ext uint8) {
	e := h.gs[i].e
	copy(entry[:], e.WRAM[0x0800+index*4:])
	return entry, e.WRAM[0x0A20+index]
}

func (h *skScenario) remoteState(i int, o uint32) []byte {
	return h.gs[i].e.SRAM[(remoteStateAddr+o)&0x7FFF:]
}

func (h *skScenario) expectOAM(when string, i, index int, entry [4]byte, ext uint8) {
	h.t.Helper()
	if actual, actualExt := h.oamEntry(i, index); actual != entry || actualExt != ext {
		h.t.Errorf("%s: g%d OAM[$%02x] = % x ext %02x; expected % x ext %02x", when, i+1, index, actual[:], actualExt, entry[:], ext)
	}
}

// expectShadow expects g2's shadow in g1's OAM[$7E] and OAM[$7F] (drawn back to front), at x (+8), y:
func (h *skScenario) expectShadow(when string, x, y uint8) {
	h.t.Helper()
	h.expectOAM(when+": shadow", 0, 0x7E, [4]byte{x, y, 0x6C, 0x28}, 0x00)
	h.expectOAM(when+": shadow", 0, 0x7F, [4]byte{x + 8, y, 0x6C, 0x68}, 0x00)
}

func (h *skScenario) expectOAMUnused(when string, i int, from int) {
	h.t.Helper()
	for index := from; index >= 0; index-- {
		if entry, _ := h.oamEntry(i, index); entry[1] != 0xF0 {
			h.t.Errorf("%s: g%d OAM[$%02x] = % x; expected unused", when, i+1, index, entry[:])
		}
	}
}

func TestRemoteSpritesScenario_BodyInSameRoom(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)

	if !h.gs[0].g.remoteBodyOK() {
		t.Fatalf("NMI hook not patched in")
	}

	// table written by o2:
	tb := h.gs[0].e.SRAM[remoteTableAddr&0x7FFF:]
	if tb[rtTTL] == 0 || tb[rtIndoors] != 1 || binary.LittleEndian.Uint16(tb[rtLocation:]) != rsRoom {
		t.Fatalf("unexpected table header % x", tb[:rtEntries])
	}
	if shadowEnd, bodyEnd, end := binary.LittleEndian.Uint16(tb[rtExtrasEnd:]), binary.LittleEndian.Uint16(tb[rtBodyEnd:]), binary.LittleEndian.Uint16(tb[rtEnd:]); shadowEnd != 2*rtEntrySize || bodyEnd != 4*rtEntrySize || end != 5*rtEntrySize {
		t.Fatalf("expected 2 shadow sprites, 2 body sprites and 1 blob; got entry offsets %d, %d, %d", shadowEnd, bodyEnd, end)
	}

	// the body (drawn behind the head) gets chrs $20/$21/$30/$31, the head chrs $22/$23/$32/$33; screen coordinates
	// are shifted by the difference in scroll offsets; the name table bit is cleared:
	h.expectShadow("in room", 0x40, 0x60)
	h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	h.expectOAMUnused("no blob", 0, 0x7B)

	if en := h.remoteState(0, rsNMIEnable)[0]; en == 0 {
		t.Errorf("NMI upload not enabled")
	}
	var src [4]uint16
	for k := range src {
		src[k] = binary.LittleEndian.Uint16(h.remoteState(0, rsNMISrc+uint32(k)*2))
	}
	if expected := [4]uint16{0x9000, 0x8400, 0x9200, 0x8600}; src != expected {
		t.Errorf("NMI sources %04x; expected %04x", src, expected)
	}

	// dma_var6/7 are as the game left them:
	for i, gs := range h.gs {
		if v6, v7 := gs.e.WRAM[0x0AE8], gs.e.WRAM[0x0AEA]; v6 != rsVar6 || v7 != rsVar7 {
			t.Errorf("g%d: dma_var6/7 = $%02x/$%02x; expected $%02x/$%02x", i+1, v6, v7, rsVar6, rsVar7)
		}
	}

	// the NMI hook uploads the tiles from ROM:
	vram := runNMIHook(t, h.gs[0].e, h.gs[0].g)
	rom := func(addr uint16) []byte {
		pak, _ := lorom.BusAddressToPak(0x100000 | uint32(addr))
		return h.gs[0].g.rom.Contents[pak : pak+0x40]
	}
	for _, c := range []struct {
		vram uint16
		src  uint16
	}{{0x4200, 0x9000}, {0x4220, 0x8400}, {0x4300, 0x9200}, {0x4320, 0x8600}} {
		if actual := vram.bytes(c.vram, 0x40); !bytes.Equal(actual, rom(c.src)) {
			t.Errorf("VRAM $%04x = % x; expected LinkGraphics $%04x = % x", c.vram, actual, c.src, rom(c.src))
		}
	}
	if vram.dmaBank(0) != 0x7E || vram.dmaBank(1) != 0x7E {
		t.Errorf("DMA source banks not restored to $7E: $%02x $%02x", vram.dmaBank(0), vram.dmaBank(1))
	}
	if vram.dmaAddr(0) != 0x1234 {
		t.Errorf("patched over code did not run: DMA0ADDR = $%04x", vram.dmaAddr(0))
	}

	// the remote player moves 4 pixels right on its screen:
	e := h.gs[1].e
	for i := 0x64; i <= 0x67; i++ {
		e.WRAM[0x0800+i*4] += 4
	}
	h.frames(4)
	h.expectShadow("moved", 0x44, 0x60)
	h.expectOAM("body moved", 0, 0x7D, [4]byte{0x44, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head moved", 0, 0x7C, [4]byte{0x44, 0x50, 0x22, 0x2E}, 0x02)

	// the local screen scrolls 8 pixels right and the game draws them in place right away:
	h.setScroll(0, 0x1078, 0x0270, 0x1018, 0x0208)
	h.frames(1)
	h.expectOAM("scrolled", 0, 0x7D, [4]byte{0x3C, 0x58, 0x20, 0x6E}, 0x02)
	// 4 pixels off the left edge: x bit 8 set:
	h.setScroll(0, 0x1078, 0x0270, 0x1058, 0x0208)
	h.frames(1)
	h.expectOAM("off the left edge", 0, 0x7D, [4]byte{0xFC, 0x58, 0x20, 0x6E}, 0x03)
	// entirely off screen: not drawn:
	h.setScroll(0, 0x1078, 0x0270, 0x1078, 0x0208)
	h.frames(1)
	h.expectOAMUnused("off screen", 0, 0x7F)
}

// When a follower or NPC uses chrs $20..$23 the remote player is drawn as a blob and nothing is uploaded.
func TestRemoteSpritesScenario_BlobWhenChrsInUse(t *testing.T) {
	h := newRSScenario(t)
	// g1's module draws a follower: LDA #$20 : STA $0AE8 : RTL
	for i, b := range []byte{0xA9, 0x20, 0x8D, 0xE8, 0x0A, 0x6B} {
		h.gs[0].e.Bus.EaWrite(testROMModuleRouting+uint32(i), b)
	}
	h.frames(8)

	// blob at g2's coordinates ($1050, $0250 + 8) relative to g1's scroll ($1010, $0208):
	for _, frame := range []uint8{0x10, 0x18} {
		h.gs[0].e.WRAM[0x1A] = frame - 1
		h.frames(1)
		chr := uint8(remoteBlobChr0)
		if frame&8 != 0 {
			chr += 2
		}
		h.expectShadow("blob", 0x40, 0x60)
		h.expectOAM("blob", 0, 0x7D, [4]byte{0x40, 0x50, chr, remoteBlobAttr}, 0x02)
		h.expectOAMUnused("only the shadow and blob", 0, 0x7C)
	}
	if en := h.remoteState(0, rsNMIEnable)[0]; en != 0 {
		t.Errorf("NMI upload enabled")
	}
	if v6, v7 := h.gs[0].e.WRAM[0x0AE8], h.gs[0].e.WRAM[0x0AEA]; v6 != 0x20 || v7 != rsVar7 {
		t.Errorf("dma_var6/7 = $%02x/$%02x; expected $20/$%02x", v6, v7, rsVar7)
	}
	vram := runNMIHook(t, h.gs[0].e, h.gs[0].g)
	if len(vram.writes) != 0 {
		t.Errorf("NMI hook uploaded %d bytes", len(vram.writes))
	}
}

// Remote players are only drawn in the same room, and the game stops drawing them immediately when the local player
// leaves the room, before o2 updates the table.
func TestRemoteSpritesScenario_OtherRoom(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	h.expectOAM("same room", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

	// g1 walks into another room; the game itself notices before o2 does:
	e := h.gs[0].e
	e.WRAM[0xA0] = uint8(rsOtherRoom)
	h.gs[0].beforeEmulate()
	e.CPU.Reset()
	e.SetPC(testROMMainGameLoop)
	if !e.RunUntil(testROMBreakPoint, 0x2_000) {
		t.Fatalf("frame did not finish")
	}
	h.expectOAMUnused("other room, same table", 0, 0x7F)

	h.frames(8)
	h.expectOAMUnused("other room", 0, 0x7F)
	if tb := e.SRAM[remoteTableAddr&0x7FFF:]; tb[rtTTL] != 0 {
		t.Errorf("table TTL %d; expected 0", tb[rtTTL])
	}

	// the remote player follows:
	h.inRoom(1, rsOtherRoom)
	h.frames(8)
	h.expectOAM("same room again", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
}

// When o2 stops updating the table, the game stops drawing remote players once its TTL runs out.
func TestRemoteSpritesScenario_TTL(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	e := h.gs[0].e
	ttl := e.SRAM[remoteTableAddr&0x7FFF+rtTTL]
	if ttl == 0 {
		t.Fatalf("TTL is 0")
	}
	emulate := func() {
		h.gs[0].beforeEmulate()
		e.CPU.Reset()
		e.SetPC(testROMMainGameLoop)
		if !e.RunUntil(testROMBreakPoint, 0x2_000) {
			t.Fatalf("frame did not finish")
		}
	}
	for f := 0; f < int(ttl); f++ {
		emulate()
	}
	h.expectOAM("last frame", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	emulate()
	h.expectOAMUnused("TTL ran out", 0, 0x7F)
}

// dmaSim records VRAM DMA transfers from the CPU bus.
type dmaSim struct {
	e      *emulator.System
	regs   [0x10000]byte
	vram   map[uint16]uint16
	writes []uint16
}

func (d *dmaSim) Read(address uint32) byte { return d.regs[address&0xFFFF] }
func (d *dmaSim) Write(address uint32, value byte) {
	a := address & 0xFFFF
	d.regs[a] = value
	if a != 0x420B {
		return
	}
	for ch := uint16(0); ch < 8; ch++ {
		if value&(1<<ch) == 0 {
			continue
		}
		r := 0x4300 + ch<<4
		src := uint32(d.regs[r+4])<<16 | uint32(d.regs[r+3])<<8 | uint32(d.regs[r+2])
		size := int(d.regs[r+6])<<8 | int(d.regs[r+5])
		if size == 0 {
			size = 0x10000
		}
		vaddr := uint16(d.regs[0x2117])<<8 | uint16(d.regs[0x2116])
		for i := 0; i < size; i += 2 {
			lo := d.e.Bus.EaRead(src + uint32(i))
			hi := d.e.Bus.EaRead(src + uint32(i) + 1)
			d.vram[vaddr] = uint16(lo) | uint16(hi)<<8
			d.writes = append(d.writes, vaddr)
			vaddr++
		}
		d.regs[0x2116], d.regs[0x2117] = uint8(vaddr), uint8(vaddr>>8)
		d.regs[r+5], d.regs[r+6] = 0, 0
	}
}
func (d *dmaSim) Shutdown()                  {}
func (d *dmaSim) Size() uint32               { return 0x10000 }
func (d *dmaSim) Clear()                     {}
func (d *dmaSim) Dump(address uint32) []byte { return nil }
func (d *dmaSim) dmaBank(ch uint16) uint8    { return d.regs[0x4304+ch<<4] }
func (d *dmaSim) dmaAddr(ch uint16) uint16   { return binary.LittleEndian.Uint16(d.regs[0x4302+ch<<4:]) }
func (d *dmaSim) bytes(vaddr uint16, n int) []byte {
	b := make([]byte, 0, n)
	for i := 0; i < n/2; i++ {
		w := d.vram[vaddr+uint16(i)]
		b = append(b, uint8(w), uint8(w>>8))
	}
	return b
}

// runNMIHook calls the late NMI hook the way NMI_DoUpdates does, with the PPU and DMA registers simulated.
func runNMIHook(t *testing.T, e *emulator.System, g *Game) *dmaSim {
	t.Helper()
	return runNMIHookAt(t, e, g, nmiHookPatchAddr)
}

// runNMIHookAt calls the NMI hook patched in at addr the way NMI_DoUpdates does.
func runNMIHookAt(t *testing.T, e *emulator.System, g *Game, addr uint32) *dmaSim {
	t.Helper()
	pak, _ := lorom.BusAddressToPak(addr)
	c := g.rom.Contents[pak : pak+4]
	if c[0] != 0x22 {
		t.Fatalf("no JSL at $%06x: % x", addr, c)
	}

	d := &dmaSim{e: e, vram: map[uint16]uint16{}}
	for _, r := range [][2]uint32{{0x002110, 0x00211F}, {0x004200, 0x00437F}} {
		if err := e.Bus.Attach(d, "dmasim", r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	// NMI_DoUpdates set up the channels for VRAM writes from WRAM:
	for ch := uint16(0); ch < 2; ch++ {
		d.regs[0x4300+ch<<4], d.regs[0x4301+ch<<4], d.regs[0x4304+ch<<4] = 0x01, 0x18, 0x7E
	}
	e.WRAM[0x0ADC], e.WRAM[0x0ADD] = 0x34, 0x12

	stub := []byte{
		0x18, 0xFB, // CLC; XCE
		0xC2, 0x30, // REP #$30
		0xA9, 0x00, 0x00, // LDA #$0000
		0x5B,       // TCD
		0xE2, 0x20, // SEP #$20
		0x48, 0xAB, // PHA; PLB
		0x22, c[1], c[2], c[3], // JSL nmiHook
	}
	copy(e.WRAM[0x1F00:], stub)
	e.CPU.Reset()
	e.SetPC(0x001F00)
	if !e.RunUntil(0x001F00+uint32(len(stub)), 0x1_000) {
		t.Fatalf("NMI hook did not return; PC=$%06x", e.GetPC())
	}
	return d
}

// With more players in the room, the first one with Link's sprites is drawn with them and the others as blobs.
func TestRemoteSpritesScenario_ThirdPlayerIsBlob(t *testing.T) {
	h := newRSScenario(t)
	g3, err := createTestGameSync("VT test", "g3", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	h.gs = append(h.gs, &g3)
	h.n = append(h.n, nil)
	h.s.Clients = append(h.s.Clients, g3.c)
	h.s.HandleAllClients(nil)
	gameHandleNet(g3.g)
	h.inRoom(2, rsRoom)
	// g3 at ($1030, $0230): on g1's screen at ($20, $28) + 8 pixels down:
	h.setScroll(2, 0x1030, 0x0230, 0x1000, 0x0200)
	h.frames(8)

	h.expectShadow("3 players", 0x40, 0x60)
	h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	entry, ext := h.oamEntry(0, 0x7B)
	if entry[0] != 0x20 || entry[1] != 0x30 || entry[2]&^2 != remoteBlobChr0 || entry[3] != remoteBlobAttr || ext != 0x02 {
		t.Errorf("g3's blob: OAM[$7b] = % x ext %02x", entry[:], ext)
	}
	h.expectOAMUnused("nothing else", 0, 0x7A)
}

// With SyncSprites off nothing is drawn, but the local player's sparkles and dust are still captured for others.
func TestRemoteSpritesScenario_SyncSpritesOff(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	h.expectOAM("on", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

	h.gs[0].g.SyncSprites = false
	h.frames(2)
	h.expectOAMUnused("off", 0, 0x7F)
	tb := h.gs[0].e.SRAM[remoteTableAddr&0x7FFF:]
	if tb[rtTTL] != 0 || tb[rtCaptureTTL] == 0 {
		t.Errorf("TTL %d, capture TTL %d; expected 0 and non-zero", tb[rtTTL], tb[rtCaptureTTL])
	}

	h.gs[0].g.SyncSprites = true
	h.frames(2)
	h.expectOAM("on again", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
}

// Sword sparkles and dash dust are sent along with Link's sprites while one of their ancillae exists.
func TestRemoteSpritesScenario_SendsSparklesAndDust(t *testing.T) {
	h := newRSScenario(t)
	e := h.gs[1].e
	setOAM := func(i int, x, y, chr, attr, ext uint8) {
		copy(e.WRAM[0x0800+i*4:], []byte{x, y, chr, attr})
		e.WRAM[0x0A20+i] = ext
	}
	// dash dust in ancilla region A, one tile off the left edge:
	setOAM(0x10, 0x48, 0x70, 0xCF, 0x04, 0x00)
	setOAM(0x11, 0xFC, 0x72, 0xA9, 0x04, 0x01)
	// not captured: another chr in the common sheet, a chr in name table 1, and chrs that are DMA'd every frame (the
	// sword, a follower):
	setOAM(0x12, 0x40, 0x70, 0x85, 0x04, 0x00)
	setOAM(0x13, 0x40, 0x70, 0xCF, 0x05, 0x00)
	setOAM(0x14, 0x40, 0x70, 0x05, 0x04, 0x00)
	setOAM(0x15, 0x40, 0x70, 0x20, 0x04, 0x02)

	received := func() (extras []remoteSprite) {
		for _, sp := range h.gs[0].g.players[1].Sprites.list {
			if sp.b[2] >= 0x80 {
				extras = append(extras, sp)
			}
		}
		return
	}

	h.frames(8)
	if extras := received(); len(extras) != 0 {
		t.Fatalf("captured without a dust ancilla: %+v", extras)
	}

	// a dash dust ancilla in slot 3:
	e.WRAM[0x0C4A+3] = 0x1E
	h.frames(4)
	extras := received()
	if len(extras) != 2 {
		t.Fatalf("expected 2 dust sprites; got %+v", extras)
	}
	// the OAM is scanned from the end:
	for i, x := range []remoteSprite{
		{index: 0x11, b: [5]uint8{0xFC, 0x72, 0xA9, 0x04, 0x01}},
		{index: 0x10, b: [5]uint8{0x48, 0x70, 0xCF, 0x04, 0x00}},
	} {
		sp := extras[i]
		if sp.index != x.index || sp.b != x.b || sp.size != 0 || sp.absidx[0] != noUniqTile {
			t.Errorf("dust sprite %d: %+v; expected %+v", i, sp, x)
		}
	}

	// the ancilla is gone:
	e.WRAM[0x0C4A+3] = 0
	h.frames(4)
	if extras := received(); len(extras) != 0 {
		t.Errorf("captured after the ancilla is gone: %+v", extras)
	}

	// nobody else in the room: not captured:
	e.WRAM[0x0C4A+3] = 0x3C
	h.inRoom(0, rsOtherRoom)
	h.frames(8)
	if n := e.SRAM[(linkOAMCopyAddr+linkOAMCopyExtras)&0x7FFF]; n != 0 {
		t.Errorf("captured %d bytes with nobody in the room", n)
	}
}

// While recoiling or jumping off a ledge, Link's shadow is drawn with chr $28; o2 sends it and draws it for remote
// players, along with their dash dust.
func TestRemoteSpritesScenario_ElevatedShadowAndDust(t *testing.T) {
	h := newRSScenario(t)
	e := h.gs[1].e
	setOAM := func(i int, x, y, chr, attr, ext uint8) {
		copy(e.WRAM[0x0800+i*4:], []byte{x, y, chr, attr})
		e.WRAM[0x0A20+i] = ext
	}
	// recoiling: the shadow is $28 and Link is in the air above it:
	setOAM(0x66, 0x50, 0x78, 0x28, 0x28, 0x00)
	setOAM(0x67, 0x58, 0x78, 0x28, 0x68, 0x00)
	// dash dust from an ancilla:
	e.WRAM[0x0C4A+3] = 0x1E
	setOAM(0x10, 0x48, 0x70, 0xCF, 0x04, 0x00)
	setOAM(0x11, 0x50, 0x72, 0xA9, 0x04, 0x00)
	h.frames(8)

	// drawn back to front by the sender's OAM index:
	h.expectOAM("shadow", 0, 0x7F, [4]byte{0x48, 0x70, 0x28, 0x68}, 0x00)
	h.expectOAM("shadow", 0, 0x7E, [4]byte{0x40, 0x70, 0x28, 0x28}, 0x00)
	h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	// the dust has lower OAM indices than Link on g2, so it is drawn in front of him:
	h.expectOAM("dust", 0, 0x7B, [4]byte{0x40, 0x6A, 0xA9, 0x04}, 0x00)
	h.expectOAM("dust", 0, 0x7A, [4]byte{0x38, 0x68, 0xCF, 0x04}, 0x00)
	h.expectOAMUnused("nothing else", 0, 0x79)
}

// Carried and thrown objects, projectiles and debris are sent while whatever draws them exists, and drawn by o2.
func TestRemoteSpritesScenario_ObjectsAndProjectiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(e *emulator.System)
		chr   uint8
		ext   uint8
	}{
		{"carried pot", func(e *emulator.System) { e.WRAM[0x0DD0+5] = 0x0A }, 0x44, 0x02},
		{"thrown rock", func(e *emulator.System) { e.WRAM[0x0DD0+5], e.WRAM[0x0E20+5] = 0x0B, 0xEC }, 0x46, 0x02},
		{"pot debris", func(e *emulator.System) { e.WRAM[0x0FB4], e.WRAM[0x1F800+7] = 0x16, 0x16 }, 0x58, 0x00},
		{"arrow", func(e *emulator.System) { e.WRAM[0x0C4A+0] = 0x09 }, 0x2A, 0x00},
		{"boomerang", func(e *emulator.System) { e.WRAM[0x0C4A+1] = 0x05 }, 0x26, 0x02},
		{"bomb", func(e *emulator.System) { e.WRAM[0x0C4A+2] = 0x07 }, 0x6E, 0x02},
		{"fire rod shot", func(e *emulator.System) { e.WRAM[0x0C4A+4] = 0x02 }, 0x8D, 0x00},
		{"somaria block", func(e *emulator.System) { e.WRAM[0x0C4A+5] = 0x2C }, 0xE9, 0x02},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRSScenario(t)
			e := h.gs[1].e
			copy(e.WRAM[0x0800+0x20*4:], []byte{0x60, 0x40, tc.chr, 0x0C})
			e.WRAM[0x0A20+0x20] = tc.ext
			tc.setup(e)

			h.frames(8)
			var got []remoteSprite
			for _, sp := range h.gs[0].g.players[1].Sprites.list {
				if sp.b[2] == tc.chr {
					got = append(got, sp)
				}
			}
			if len(got) != 1 || got[0].index != 0x20 || got[0].size != tc.ext>>1 {
				t.Fatalf("expected chr $%02x at OAM index $20 to be sent; got %+v", tc.chr, got)
			}
			// drawn in front of Link like on g2 (lower OAM index), relative to g1's scroll:
			h.expectShadow("drawn", 0x40, 0x60)
			h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
			h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
			h.expectOAM("drawn", 0, 0x7B, [4]byte{0x50, 0x38, tc.chr, 0x0C}, tc.ext)
		})
	}

	// without anything drawing them, they are not sent:
	h := newRSScenario(t)
	e := h.gs[1].e
	copy(e.WRAM[0x0800+0x20*4:], []byte{0x60, 0x40, 0x44, 0x0C})
	e.WRAM[0x0A20+0x20] = 0x02
	h.frames(8)
	for _, sp := range h.gs[0].g.players[1].Sprites.list {
		if sp.b[2] == 0x44 {
			t.Errorf("sent a pot nobody carries: %+v", sp)
		}
	}
	h.expectShadow("nothing else", 0x40, 0x60)
}

// Remote players are drawn whenever the game draws the underworld or overworld sprites, including dialogs and menus;
// the body only where the NMI has time for it.
func TestRemoteSpritesScenario_Modules(t *testing.T) {
	const (
		nothing = iota
		blob
		body
	)
	for _, tc := range []struct {
		name                       string
		overworld                  bool
		module, submodule, mapMode uint8
		expected                   int
	}{
		{"underworld", false, 0x07, 0x00, 0, body},
		{"underworld transition", false, 0x07, 0x02, 0, blob},
		{"dialog", false, 0x0E, 0x02, 0, body},
		{"item menu", false, 0x0E, 0x01, 0, blob},
		{"dungeon map opening", false, 0x0E, 0x03, 0, blob},
		{"dungeon map shown", false, 0x0E, 0x03, 1, nothing},
		{"dungeon map closing", false, 0x0E, 0x03, 7, blob},
		{"spotlight open", false, 0x10, 0x01, 0, blob},
		{"pendant victory", false, 0x13, 0x00, 0, body},
		{"game over", false, 0x12, 0x00, 0, nothing},
		{"overworld", true, 0x09, 0x00, 0, body},
		{"overworld dialog", true, 0x0E, 0x02, 0, body},
		{"overworld map opening", true, 0x0E, 0x07, 0, blob},
		{"overworld map shown", true, 0x0E, 0x07, 1, nothing},
		{"flute menu shown", true, 0x0E, 0x0A, 1, nothing},
		{"special overworld", true, 0x0B, 0x00, 0, body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRSScenario(t)
			if tc.overworld {
				for i := range h.gs {
					h.inArea(i, 0x18)
					h.gs[i].e.WRAM[0x0AE8], h.gs[i].e.WRAM[0x0AEA] = rsVar6, rsVar7
				}
				h.setScroll(1, 0x1050, 0x0250, 0x1000, 0x0200)
				h.setScroll(0, 0x1070, 0x0270, 0x1010, 0x0208)
			}
			h.frames(8)
			h.expectOAM("in game", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

			// the module the local player was in before the interface:
			e := h.gs[0].e
			e.WRAM[0x010C] = e.WRAM[0x10]
			e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0x0200] = tc.module, tc.submodule, tc.mapMode
			h.frames(4)

			switch tc.expected {
			case nothing:
				h.expectOAMUnused("nothing", 0, 0x7F)
			case blob:
				h.expectShadow("blob", 0x40, 0x60)
				if entry, _ := h.oamEntry(0, 0x7D); entry[2]&^2 != remoteBlobChr0 {
					t.Errorf("expected a blob; OAM[$7d] = % x", entry[:])
				}
			case body:
				h.expectShadow("body", 0x40, 0x60)
				h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
				h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
			}
		})
	}
}

// Remote players in a dialog are still drawn: they don't send the module they were in before it, so o2 must not
// require it to know they are in the game.
func TestRemoteSpritesScenario_RemoteInDialog(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	h.expectOAM("before", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

	e := h.gs[1].e
	e.WRAM[0x010C] = e.WRAM[0x10]
	e.WRAM[0x10], e.WRAM[0x11] = 0x0E, 0x02
	h.frames(8)
	h.expectShadow("in dialog", 0x40, 0x60)
	h.expectOAM("in dialog", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("in dialog", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)

	// but not on the overworld map:
	e.WRAM[0x11] = 0x07
	h.frames(8)
	h.expectOAMUnused("on the map", 0, 0x7F)
}

// While a dialog renders text, the game disables its own sprite uploads ($0710 = 2, Text_Render) to upload the text;
// the early NMI hook uploads the body then, so the remote player keeps Link's graphics during the local player's
// dialogs.
func TestRemoteSpritesScenario_BodyDuringLocalDialog(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	g1 := h.gs[0]
	e := g1.e
	e.WRAM[0x010C] = e.WRAM[0x10]
	e.WRAM[0x10], e.WRAM[0x11] = 0x0E, 0x02
	clearOAM := g1.beforeEmulate
	g1.beforeEmulate = func() {
		clearOAM()
		// Text_Render:
		e.WRAM[0x0710] = 2
	}
	h.frames(4)
	h.expectOAM("dialog", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("dialog", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)

	vram := runNMIHookAt(t, e, g1.g, nmiEarlyPatchAddr)
	rom := func(addr uint16) []byte {
		pak, _ := lorom.BusAddressToPak(0x100000 | uint32(addr))
		return g1.g.rom.Contents[pak : pak+0x40]
	}
	for _, c := range []struct {
		vram uint16
		src  uint16
	}{{0x4200, 0x9000}, {0x4220, 0x8400}, {0x4300, 0x9200}, {0x4320, 0x8600}} {
		if actual := vram.bytes(c.vram, 0x40); !bytes.Equal(actual, rom(c.src)) {
			t.Errorf("VRAM $%04x = % x; expected LinkGraphics $%04x", c.vram, actual, c.src)
		}
	}
	if vmain := vram.regs[0x2115]; vmain != 0x80 {
		t.Errorf("patched over code did not run: VMAIN = $%02x", vmain)
	}
	if vram.dmaBank(0) != 0x7E || vram.dmaBank(1) != 0x7E {
		t.Errorf("DMA source banks not restored to $7E")
	}

	// with core updates enabled the early hook leaves the upload to the late hook:
	e.WRAM[0x0710] = 0
	if vram := runNMIHookAt(t, e, g1.g, nmiEarlyPatchAddr); len(vram.writes) != 0 {
		t.Errorf("early hook uploaded %d words with core updates enabled", len(vram.writes))
	}

	// outside of dialogs, disabled core updates mean the NMI is busy with something else: the blob is drawn:
	e.WRAM[0x10], e.WRAM[0x11] = 0x07, 0x00
	h.frames(2)
	if entry, _ := h.oamEntry(0, 0x7D); entry[2]&^2 != remoteBlobChr0 {
		t.Errorf("expected a blob with core updates disabled outside dialogs; OAM[$7d] = % x", entry[:])
	}
}
