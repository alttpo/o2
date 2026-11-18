package alttp

import (
	"bytes"
	"encoding/binary"
	"io"
	"sort"
	"testing"
	"time"

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

// nmiRows returns the rows of chrs remoteDraw listed for g's NMI hooks to upload: VRAM address, source address and
// partWRAM if the source is in WRAM.
func (h *skScenario) nmiRows(i int) (rows [][3]uint16) {
	n := int(binary.LittleEndian.Uint16(h.remoteState(i, rsNMILen)))
	l := h.remoteState(i, rsNMIList)
	for o := 0; o < n; o += nmiRowSize {
		v := binary.LittleEndian.Uint16(l[o:])
		rows = append(rows, [3]uint16{v & 0x7FFF, binary.LittleEndian.Uint16(l[o+2:]), (v >> 15) * partWRAM})
	}
	return
}

// expectBlob expects a blob at screen (x, y) in g1's OAM[index] and its shadow in OAM[index+1], behind it.
func (h *skScenario) expectBlob(when string, index int, x, y uint8) {
	h.t.Helper()
	h.expectOAM(when+": blob shadow", 0, index+1, [4]byte{x, y + remoteShadowYOffs, remoteShadowChr, remoteBlobAttr}, 0x02)
	entry, ext := h.oamEntry(0, index)
	if entry[0] != x || entry[1] != y || entry[2]&^2 != remoteBlobChr0 || entry[3] != remoteBlobAttr || ext != 0x02 {
		h.t.Errorf("%s: blob: g1 OAM[$%02x] = % x ext %02x", when, index, entry[:], ext)
	}
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
	var ends [5]uint16
	for k, f := range []int{rtExtrasEnd, rtSlottedEnd, rtP0BlobEnd, rtEnd, rtFrontEnd} {
		ends[k] = binary.LittleEndian.Uint16(tb[f:]) / rtEntrySize
	}
	if ends != [5]uint16{2, 4, 6, 6, 6} {
		t.Fatalf("expected 2 shadow sprites, 2 body sprites and a blob and its shadow; got entry ends %d", ends)
	}
	if parts, required := binary.LittleEndian.Uint16(tb[rtParts:]), tb[rtRequired]; parts != 2 || required != 2 {
		t.Fatalf("expected 2 parts, both required; got %d, %d", parts, required)
	}

	// the body (drawn behind the head) gets chrs $20/$21/$30/$31, the head chrs $22/$23/$32/$33; screen coordinates
	// are shifted by the difference in scroll offsets; the name table bit is cleared:
	h.expectShadow("in room", 0x40, 0x60)
	h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	h.expectOAMUnused("no blob", 0, 0x7B)

	// the body's rows into chrs $20/$21 and $30/$31, the head's into $22/$23 and $32/$33, from LinkGraphics:
	if rows, expected := h.nmiRows(0), [][3]uint16{
		{0x4200, 0x9000, 0}, {0x4300, 0x9200, 0}, {0x4220, 0x8400, 0}, {0x4320, 0x8600, 0},
	}; !equalRows(rows, expected) {
		t.Errorf("NMI rows %04x; expected %04x", rows, expected)
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
	// and not uploaded:
	if rows := h.nmiRows(0); len(rows) != 0 {
		t.Errorf("uploads %04x while off screen", rows)
	}
	if vram := runNMIHook(t, h.gs[0].e, h.gs[0].g); len(vram.writes) != 0 {
		t.Errorf("NMI hook uploaded %d words while off screen", len(vram.writes))
	}
}

func equalRows(a, b [][3]uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setModuleDraws makes g1's module set dma_var6 to $20 like a follower or NPC does, and draw OAM entries $00, $01...
// (x = $00, y = $20) with the chrs and sizes in ext: LDA #$20 : STA $0AE8 : (LDA #chr : STA $0802+i*4 : LDA #$20 :
// STA $0801+i*4 : LDA #ext : STA $0A20+i)... : RTL
func (h *skScenario) setModuleDraws(sprites ...[2]uint8) {
	code := []byte{0xA9, 0x20, 0x8D, 0xE8, 0x0A}
	for i, sp := range sprites {
		o, x := uint16(0x0800+i*4), uint16(0x0A20+i)
		code = append(code,
			0xA9, sp[0], 0x8D, uint8(o+2), uint8((o+2)>>8),
			0xA9, 0x20, 0x8D, uint8(o+1), uint8((o+1)>>8),
			0xA9, sp[1], 0x8D, uint8(x), uint8(x>>8))
	}
	code = append(code, 0x6B)
	for i, b := range code {
		h.gs[0].e.Bus.EaWrite(testROMModuleRouting+uint32(i), b)
	}
}

// expectBodyIn expects g2's body and head in g1's OAM[$7D] and OAM[$7C] with the chrs of the slots they got.
func (h *skScenario) expectBodyIn(when string, bodyChr, headChr uint8) {
	h.t.Helper()
	h.expectOAM(when+": body", 0, 0x7D, [4]byte{0x40, 0x58, bodyChr, 0x6E}, 0x02)
	h.expectOAM(when+": head", 0, 0x7C, [4]byte{0x40, 0x50, headChr, 0x2E}, 0x02)
}

// When a visible follower or NPC uses chrs $22/$23/$32/$33 the body and head get the next free VRAM slots; only the
// rows of those are uploaded.
func TestRemoteSpritesScenario_BodyInFreeSlots(t *testing.T) {
	h := newRSScenario(t)
	h.setModuleDraws([2]uint8{0x22, 0x02})
	h.frames(8)
	h.expectShadow("follower", 0x40, 0x60)
	h.expectBodyIn("follower", 0x20, 0x24)
	h.expectOAM("follower", 0, 0x00, [4]byte{0x00, 0x20, 0x22, 0x00}, 0x02)
	if used := h.remoteState(0, rsUsed)[0]; used != 0x02 {
		t.Errorf("slots in use $%02x; expected $02", used)
	}
	if rows, expected := h.nmiRows(0), [][3]uint16{
		{0x4200, 0x9000, 0}, {0x4300, 0x9200, 0}, {0x4240, 0x8400, 0}, {0x4340, 0x8600, 0},
	}; !equalRows(rows, expected) {
		t.Errorf("NMI rows %04x; expected %04x", rows, expected)
	}
	vram := runNMIHook(t, h.gs[0].e, h.gs[0].g)
	for _, w := range vram.writes {
		if w >= 0x4220 && w < 0x4240 || w >= 0x4320 && w < 0x4340 {
			t.Errorf("NMI hook overwrote the follower's chrs at VRAM $%04x", w)
		}
	}
	// the game's own upload of chrs $20..$23 is left as it is set:
	if v6, v7 := h.gs[0].e.WRAM[0x0AE8], h.gs[0].e.WRAM[0x0AEA]; v6 != 0x20 || v7 != rsVar7 {
		t.Errorf("dma_var6/7 = $%02x/$%02x; expected $20/$%02x", v6, v7, rsVar7)
	}
}

// Only when fewer than two VRAM slots are free is the remote player drawn as a blob, with a shadow under it, and
// nothing is uploaded.
func TestRemoteSpritesScenario_BlobWhenNoFreeSlots(t *testing.T) {
	h := newRSScenario(t)
	// sprites showing every slot but chrs $07..: $20, $22, $24 (8x8), $0C, $09 and $05 (8x8 chr $15):
	h.setModuleDraws([2]uint8{0x20, 0x02}, [2]uint8{0x33, 0x00}, [2]uint8{0x24, 0x00}, [2]uint8{0x0B, 0x02},
		[2]uint8{0x0A, 0x00}, [2]uint8{0x15, 0x00})
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
		h.expectOAM("blob shadow", 0, 0x7D, [4]byte{0x40, 0x50 + remoteShadowYOffs, remoteShadowChr, remoteBlobAttr}, 0x02)
		h.expectOAM("blob", 0, 0x7C, [4]byte{0x40, 0x50, chr, remoteBlobAttr}, 0x02)
		for index := 0x7B; index > 5; index-- {
			if entry, _ := h.oamEntry(0, index); entry[1] != 0xF0 {
				t.Errorf("only the shadows, blob and module's sprites: g1 OAM[$%02x] = % x; expected unused", index, entry[:])
			}
		}
	}
	if used := h.remoteState(0, rsUsed)[0]; used != 0x3F {
		t.Errorf("slots in use $%02x; expected $3f", used)
	}
	if rows := h.nmiRows(0); len(rows) != 0 {
		t.Errorf("NMI rows %04x; expected none", rows)
	}
	vram := runNMIHook(t, h.gs[0].e, h.gs[0].g)
	if len(vram.writes) != 0 {
		t.Errorf("NMI hook uploaded %d words", len(vram.writes))
	}
}

// While core updates are disabled, Link's own chrs are not handed out: the game won't upload them again before Link
// may show them.
func TestRemoteSpritesScenario_NoLinkSlotsWhileCoreUpdatesDisabled(t *testing.T) {
	h := newRSScenario(t)
	// the follower slots are in use:
	h.setModuleDraws([2]uint8{0x20, 0x02}, [2]uint8{0x22, 0x02}, [2]uint8{0x24, 0x00})
	h.frames(8)
	h.expectBodyIn("core updates", 0x0C, 0x09)

	g1 := h.gs[0]
	clearOAM := g1.beforeEmulate
	g1.beforeEmulate = func() {
		clearOAM()
		g1.e.WRAM[0x0710] = 1
	}
	h.frames(2)
	h.expectBlob("core updates disabled", 0x7C, 0x40, 0x50)
}

// The slots that sprites in the OAM buffer show, 8x8 or 16x16 (which also show the chrs right of and below theirs),
// are not handed out; the body and head get the first two free ones.
func TestRemoteSpritesScenario_SlotsInUse(t *testing.T) {
	for _, c := range []struct {
		name     string
		chr, ext uint8
		used     uint8
	}{
		{"nothing drawn", 0x00, 0x00, 0x00},
		{"8x8 chr $24", 0x24, 0x00, 0x04},
		{"8x8 chr $1F", 0x1F, 0x00, 0x00},
		{"8x8 chr $40", 0x40, 0x00, 0x00},
		{"16x16 chr $02", 0x02, 0x02, 0x00},
		{"16x16 chr $04", 0x04, 0x02, 0x20},
		{"16x16 chr $34", 0x34, 0x02, 0x04},
		{"16x16 chr $40", 0x40, 0x02, 0x00},
		{"8x8 chr $20", 0x20, 0x00, 0x01},
		{"8x8 chr $33", 0x33, 0x00, 0x02},
		{"16x16 chr $20", 0x20, 0x02, 0x01},
		{"16x16 chr $12", 0x12, 0x02, 0x02},
		{"16x16 chr $1F", 0x1F, 0x02, 0x01},
		{"16x16 chr $33", 0x33, 0x02, 0x06},
		{"8x8 chr $0A", 0x0A, 0x00, 0x10},
		{"16x16 chr $0B", 0x0B, 0x02, 0x08},
		{"8x8 chr $18", 0x18, 0x00, 0x40},
		{"16x16 chr $08", 0x08, 0x02, 0x50},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRSScenario(t)
			if c.chr != 0 {
				h.setModuleDraws([2]uint8{c.chr, c.ext})
			} else {
				h.setModuleDraws()
			}
			h.frames(8)
			h.expectShadow(c.name, 0x40, 0x60)
			if used := h.remoteState(0, rsUsed)[0]; used != c.used {
				t.Errorf("slots in use $%02x; expected $%02x", used, c.used)
			}
			var free []uint8
			for i, chr := range remoteSlots {
				if c.used&(1<<i) == 0 {
					free = append(free, chr)
				}
			}
			h.expectBodyIn(c.name, free[0], free[1])
		})
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
	// the channels started, of every write to MDMAEN:
	started uint8
}

func (d *dmaSim) Read(address uint32) byte { return d.regs[address&0xFFFF] }
func (d *dmaSim) Write(address uint32, value byte) {
	a := address & 0xFFFF
	d.regs[a] = value
	if a != 0x420B {
		return
	}
	d.started |= value
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

// runNMIHookAt calls the NMI hook patched in at addr the way NMI_DoUpdates does; setup, if any, sets up the simulated
// registers before.
func runNMIHookAt(t *testing.T, e *emulator.System, g *Game, addr uint32, setup ...func(d *dmaSim)) *dmaSim {
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
	for _, f := range setup {
		f(d)
	}

	stub := []byte{
		0x18, 0xFB, // CLC; XCE
		0xC2, 0x30, // REP #$30
		0xA2, 0x40, 0x00, // LDX #$0040: the size of a row, as NMI_DoUpdates has it
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
	// g3's Link, so g3 is visible (players that send no sprites are not drawn at all):
	e3 := g3.e
	e3.WRAM[0x0352], e3.WRAM[0x0353] = 0x90, 0x01
	for i := 0; i < 0x80; i++ {
		e3.WRAM[0x0800+i*4+1] = 0xF0
	}
	copy(e3.WRAM[0x0800+0x64*4:], []byte{0x30, 0x38, 0x00, 0x2E})
	copy(e3.WRAM[0x0800+0x65*4:], []byte{0x30, 0x40, 0x02, 0x6E})
	e3.WRAM[0x0A20+0x64], e3.WRAM[0x0A20+0x65] = 0x02, 0x02
	for k, v := range []uint16{0x8400, 0x8600, 0x9000, 0x9200, 0x9400, 0x9420} {
		binary.LittleEndian.PutUint16(e3.WRAM[0x0ACC+k*2:], v)
	}
	h.frames(8)

	h.expectShadow("3 players", 0x40, 0x60)
	h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	h.expectBlob("g3", 0x7A, 0x20, 0x30)
	h.expectOAMUnused("nothing else", 0, 0x79)
}

// With SyncSprites off the patch's remote sprites code doesn't run at all (see runtiming.go): nothing is drawn, the
// local player's sprites are neither captured nor sent, and the NMI hooks run only the game's own code.
func TestRemoteSpritesScenario_SyncSpritesOff(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	h.expectOAM("on", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	e1, e2 := h.gs[0].e, h.gs[1].e
	if gates := e2.SRAM[gatesAddr&0x7FFF]; gates != gateSprites {
		t.Fatalf("g2 gates $%02x; expected $%02x", gates, gateSprites)
	}

	h.gs[0].g.SyncSprites = false
	h.gs[1].g.SyncSprites = false
	h.frames(2)
	h.expectOAMUnused("off", 0, 0x7F)
	if gates := e2.SRAM[gatesAddr&0x7FFF]; gates != 0 {
		t.Fatalf("g2 gates $%02x; expected 0", gates)
	}
	// g2's game doesn't copy Link's OAM entries any more:
	copyAt := linkOAMCopyAddr & 0x7FFF
	before := append([]byte(nil), e2.SRAM[copyAt:copyAt+linkOAMCopySize]...)
	e2.WRAM[0x0800+0x65*4] = 0x51 // the body
	h.frames(2)
	if after := e2.SRAM[copyAt : copyAt+linkOAMCopySize]; !bytes.Equal(before, after) {
		t.Errorf("g2 copied Link's OAM entries with the sprites gate clear")
	}
	// g1's late NMI hook only runs the code it was patched over:
	d := runNMIHook(t, e1, h.gs[0].g)
	if len(d.vram) != 0 {
		t.Errorf("g1 NMI hook uploaded %d words with the sprites gate clear", len(d.vram))
	}

	h.gs[0].g.SyncSprites = true
	h.gs[1].g.SyncSprites = true
	h.frames(8)
	h.expectOAM("on again", 0, 0x7D, [4]byte{0x41, 0x58, 0x20, 0x6E}, 0x02)
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
		{"fire rod flame", func(e *emulator.System) { e.WRAM[0x0C4A+4] = 0x02 }, 0xA4, 0x00},
		{"lamp flame", func(e *emulator.System) { e.WRAM[0x0C4A+4] = 0x2F }, 0xE3, 0x00},
		{"sword beam hit", func(e *emulator.System) { e.WRAM[0x0C4A+3] = 0x04 }, 0xD5, 0x00},
		{"ice rod wall hit", func(e *emulator.System) { e.WRAM[0x0C4A+3] = 0x11 }, 0xCF, 0x00},
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

// Remote players are drawn whenever the game draws the underworld or overworld sprites, including dialogs and menus,
// and the first one always with Link's head and body, in every submodule.
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
		{"underworld intra-room transition", false, 0x07, 0x01, 0, body},
		{"underworld transition", false, 0x07, 0x02, 0, body},
		{"entering through a door", false, 0x07, 0x07, 0, body},
		{"spiral staircase", false, 0x07, 0x0E, 0, body},
		{"straight staircase", false, 0x07, 0x12, 0, body},
		{"dialog", false, 0x0E, 0x02, 0, body},
		{"item menu", false, 0x0E, 0x01, 0, body},
		{"dungeon map opening", false, 0x0E, 0x03, 0, body},
		{"dungeon map shown", false, 0x0E, 0x03, 1, nothing},
		{"dungeon map closing", false, 0x0E, 0x03, 7, body},
		{"spotlight close", false, 0x0F, 0x00, 0, body},
		{"spotlight open", false, 0x10, 0x01, 0, body},
		{"pendant victory", false, 0x13, 0x00, 0, body},
		{"game over", false, 0x12, 0x00, 0, nothing},
		{"overworld", true, 0x09, 0x00, 0, body},
		{"overworld dialog", true, 0x0E, 0x02, 0, body},
		{"overworld map opening", true, 0x0E, 0x07, 0, body},
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
				h.expectBlob("blob", 0x7C, 0x40, 0x50)
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

	// the next frame of the dialog: the tiles are still in VRAM:
	h.frames(1)
	if vram := runNMIHookAt(t, e, g1.g, nmiEarlyPatchAddr); len(vram.writes) != 0 {
		t.Errorf("early hook uploaded %d words already in VRAM", len(vram.writes))
	}
	// the remote player's body changes pose; only its rows are uploaded:
	binary.LittleEndian.PutUint16(h.gs[1].e.WRAM[0x0AD0:], 0x9800)
	binary.LittleEndian.PutUint16(h.gs[1].e.WRAM[0x0AD2:], 0x9A00)
	h.frames(4)
	vram = runNMIHookAt(t, e, g1.g, nmiEarlyPatchAddr)
	if len(vram.writes) != 0x40 {
		t.Errorf("early hook uploaded %d words; expected the body's $40", len(vram.writes))
	}
	for _, c := range []struct {
		vram uint16
		src  uint16
	}{{0x4200, 0x9800}, {0x4300, 0x9A00}} {
		if actual := vram.bytes(c.vram, 0x40); !bytes.Equal(actual, rom(c.src)) {
			t.Errorf("new pose: VRAM $%04x = % x; expected LinkGraphics $%04x", c.vram, actual, c.src)
		}
	}

	// with core updates enabled the early hook leaves the upload to the late hook:
	e.WRAM[0x0710] = 0
	if vram := runNMIHookAt(t, e, g1.g, nmiEarlyPatchAddr); len(vram.writes) != 0 {
		t.Errorf("early hook uploaded %d words with core updates enabled", len(vram.writes))
	}

	// outside of dialogs too, e.g. while a room loads during a transition, the early hook uploads the body; the game
	// uploaded its own tiles over it since:
	g1.beforeEmulate = func() {
		clearOAM()
		e.WRAM[0x0710] = 1
	}
	e.WRAM[0x10], e.WRAM[0x11] = 0x07, 0x02
	h.frames(2)
	h.expectOAM("room load", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("room load", 0, 0x7C, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	vram = runNMIHookAt(t, e, g1.g, nmiEarlyPatchAddr)
	for _, c := range []struct {
		vram uint16
		src  uint16
	}{{0x4200, 0x9800}, {0x4220, 0x8400}, {0x4300, 0x9A00}, {0x4320, 0x8600}} {
		if actual := vram.bytes(c.vram, 0x40); !bytes.Equal(actual, rom(c.src)) {
			t.Errorf("room load: VRAM $%04x = % x; expected LinkGraphics $%04x", c.vram, actual, c.src)
		}
	}
}

// Enemies and NPCs use the same shadows as bombs and thrown objects: shadows are only sent while one of those exists,
// and only the ones just under it.
func TestRemoteSpritesScenario_OnlyOwnShadowsSent(t *testing.T) {
	setOAM := func(e *emulator.System, i int, x, y, chr, attr, ext uint8) {
		copy(e.WRAM[0x0800+i*4:], []byte{x, y, chr, attr})
		e.WRAM[0x0A20+i] = ext
	}
	sent := func(h *skScenario) (chrs []uint8) {
		list := append([]remoteSprite(nil), h.gs[0].g.players[1].Sprites.list...)
		sort.Slice(list, func(i, j int) bool { return list[i].index < list[j].index })
		for _, sp := range list {
			// Link's own entries are at $64..$67:
			if sp.index < 0x64 || sp.index > 0x6F {
				chrs = append(chrs, sp.b[2])
			}
		}
		return
	}
	enemyShadows := func(e *emulator.System) {
		// an enemy's shadow far away and one right next to Link:
		setOAM(e, 0x30, 0xA0, 0x90, 0x6C, 0x28, 0x00)
		setOAM(e, 0x31, 0xA8, 0x90, 0x6C, 0x68, 0x00)
		setOAM(e, 0x32, 0x58, 0x68, 0x6C, 0x28, 0x00)
	}

	t.Run("charging a spin attack", func(t *testing.T) {
		h := newRSScenario(t)
		e := h.gs[1].e
		enemyShadows(e)
		e.WRAM[0x0C4A+3] = 0x3C
		setOAM(e, 0x10, 0x60, 0x50, 0x80, 0x04, 0x00)
		h.frames(8)
		if chrs := sent(h); len(chrs) != 1 || chrs[0] != 0x80 {
			t.Errorf("expected only the sparkle to be sent; got chrs % x", chrs)
		}
	})

	t.Run("thrown pot", func(t *testing.T) {
		h := newRSScenario(t)
		e := h.gs[1].e
		enemyShadows(e)
		e.WRAM[0x0DD0+5], e.WRAM[0x0E20+5] = 0x0B, 0xEC
		// the pot in the air at ($C0, $40) and its shadow 20 pixels below:
		setOAM(e, 0x20, 0xC0, 0x40, 0x44, 0x0C, 0x02)
		setOAM(e, 0x21, 0xC0, 0x54, 0x6C, 0x28, 0x02)
		h.frames(8)
		if chrs := sent(h); len(chrs) != 2 || chrs[0] != 0x44 || chrs[1] != 0x6C {
			t.Errorf("expected the pot and its shadow; got chrs % x", chrs)
		}
	})

	t.Run("bomb", func(t *testing.T) {
		h := newRSScenario(t)
		e := h.gs[1].e
		enemyShadows(e)
		e.WRAM[0x0C4A+2] = 0x07
		setOAM(e, 0x20, 0x20, 0x40, 0x6E, 0x22, 0x02)
		setOAM(e, 0x21, 0x20, 0x4C, 0x6C, 0x28, 0x00)
		setOAM(e, 0x22, 0x28, 0x4C, 0x6C, 0x68, 0x00)
		h.frames(8)
		if chrs := sent(h); len(chrs) != 3 || chrs[0] != 0x6E || chrs[1] != 0x6C || chrs[2] != 0x6C {
			t.Errorf("expected the bomb and its shadow; got chrs % x", chrs)
		}
	})

	t.Run("thrown pot next to an enemy", func(t *testing.T) {
		h := newRSScenario(t)
		e := h.gs[1].e
		e.WRAM[0x0DD0+5], e.WRAM[0x0E20+5] = 0x0B, 0xEC
		// an enemy's shadow at the pot's X, just below it, in its own OAM entry:
		setOAM(e, 0x10, 0xC0, 0x58, 0x6C, 0x28, 0x02)
		setOAM(e, 0x20, 0xC0, 0x40, 0x44, 0x0C, 0x02)
		setOAM(e, 0x21, 0xC0, 0x54, 0x6C, 0x28, 0x02)
		h.frames(8)
		list := h.gs[0].g.players[1].Sprites.list
		var shadows []uint8
		for _, sp := range list {
			if sp.b[2] == 0x6C && (sp.index < 0x64 || sp.index > 0x6F) {
				shadows = append(shadows, sp.index)
			}
		}
		if len(shadows) != 1 || shadows[0] != 0x21 {
			t.Errorf("expected only the pot's shadow (OAM $21); got shadows at % x", shadows)
		}
	})

	t.Run("carried large rock next to an enemy", func(t *testing.T) {
		h := newRSScenario(t)
		e := h.gs[1].e
		// sprite $EC carried by Link (state $0A):
		e.WRAM[0x0DD0+5], e.WRAM[0x0E20+5] = 0x0A, 0xEC
		// the rock centered at X $60 above Link's head (SpriteDraw_ThrownItem_Gigantic):
		setOAM(e, 0x20, 0x58, 0x30, 0x4A, 0x2C, 0x02)
		setOAM(e, 0x21, 0x68, 0x30, 0x4A, 0x6C, 0x02)
		setOAM(e, 0x22, 0x58, 0x40, 0x4A, 0xAC, 0x02)
		setOAM(e, 0x23, 0x68, 0x40, 0x4A, 0xEC, 0x02)
		// its shadow, in region B:
		setOAM(e, 0x30, 0x5A, 0x5C, 0x6C, 0x24, 0x02)
		setOAM(e, 0x31, 0x60, 0x5C, 0x6C, 0x24, 0x02)
		setOAM(e, 0x32, 0x66, 0x5C, 0x6C, 0x24, 0x02)
		// enemies' shadows right next to it:
		setOAM(e, 0x40, 0x60, 0x60, 0x6C, 0x28, 0x02)
		setOAM(e, 0x41, 0x50, 0x50, 0x6C, 0x28, 0x02)
		setOAM(e, 0x42, 0x5A, 0x48, 0x38, 0x28, 0x00)
		h.frames(8)
		var shadows []uint8
		for _, sp := range h.gs[0].g.players[1].Sprites.list {
			if isCaptureShadowChr(sp.b[2]) && (sp.index < 0x64 || sp.index > 0x6F) {
				shadows = append(shadows, sp.index)
			}
		}
		sort.Slice(shadows, func(i, j int) bool { return shadows[i] < shadows[j] })
		if len(shadows) != 3 || shadows[0] != 0x30 || shadows[1] != 0x31 || shadows[2] != 0x32 {
			t.Errorf("expected only the rock's shadow (OAM $30..$32); got shadows at % x", shadows)
		}
	})
}

// A remote player whose Link is invisible (e.g. flickering after a warp) sends no head or body: alttpo still sends
// Link's shadow then, o2 sends nothing. They are not drawn at all, not even as a blob, so the flicker doesn't alternate
// between Link and a blob.
func TestRemoteSpritesScenario_NoSpritesNoBlob(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	h.expectOAM("visible", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

	// only the shadow is left, like alttpo sends:
	e := h.gs[1].e
	for i := 0x64; i <= 0x65; i++ {
		e.WRAM[0x0800+i*4+1] = 0xF0
	}
	h.frames(8)
	if n := len(h.gs[0].g.players[1].Sprites.list); n != 2 {
		t.Fatalf("expected only the shadow; got %d sprites", n)
	}
	h.expectOAMUnused("shadow only", 0, 0x7F)

	// nothing at all:
	for i := 0x66; i <= 0x67; i++ {
		e.WRAM[0x0800+i*4+1] = 0xF0
	}
	h.frames(8)
	if n := len(h.gs[0].g.players[1].Sprites.list); n != 0 {
		t.Fatalf("expected an empty sprite list; got %d sprites", n)
	}
	h.expectOAMUnused("invisible", 0, 0x7F)

	// the same while the body can't be drawn (no free VRAM slots), where a visible player is a blob:
	h.setModuleDraws([2]uint8{0x20, 0x02}, [2]uint8{0x22, 0x02}, [2]uint8{0x24, 0x02}, [2]uint8{0x0C, 0x02},
		[2]uint8{0x09, 0x02}, [2]uint8{0x05, 0x02})
	h.frames(8)
	for index := 0x7F; index > 5; index-- {
		if entry, _ := h.oamEntry(0, index); entry[1] != 0xF0 {
			t.Errorf("invisible with chrs in use: g1 OAM[$%02x] = % x; expected unused", index, entry[:])
		}
	}
}

// A remote player that leaves the room or area is not drawn, even though their reported location only changes once
// their screen transition or warp finishes.
func TestRemoteSpritesScenario_RemoteLeaving(t *testing.T) {
	for _, tc := range []struct {
		name              string
		module, submodule uint8
		subsubmodule      uint8
		overworld         bool
	}{
		{"warp tile", 0x09, 0x23, 0x00, true},
		{"overworld scroll", 0x09, 0x02, 0x00, true},
		{"underworld room scroll", 0x07, 0x02, 0x00, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRSScenario(t)
			if tc.overworld {
				for i := range h.gs {
					h.inArea(i, 0x18)
				}
				h.setScroll(1, 0x1050, 0x0250, 0x1000, 0x0200)
				h.setScroll(0, 0x1070, 0x0270, 0x1010, 0x0208)
			}
			h.frames(8)
			h.expectOAM("before", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

			e := h.gs[1].e
			e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0xB0] = tc.module, tc.submodule, tc.subsubmodule
			h.frames(4)
			p := &h.gs[0].g.players[1]
			if !locationsEqual(p.Location, h.gs[0].g.local.Location) {
				t.Fatalf("the remote's reported location should still be the same: $%06x", p.Location)
			}
			h.expectOAMUnused("leaving", 0, 0x7F)
		})
	}

	// sprites stop arriving (alttpo sends them to the sector it is actually in):
	h := newRSScenario(t)
	h.frames(8)
	g := h.gs[0].g
	if _, draw, _ := g.buildRemoteSpritesTable(time.Now()); !draw {
		t.Fatalf("expected the remote player to be drawn")
	}
	g.players[1].Sprites.received = time.Now().Add(-remoteSpritesStale)
	if _, draw, _ := g.buildRemoteSpritesTable(time.Now()); draw {
		t.Errorf("drawn without recent sprites")
	}
}

// TestRemoteSpritesScenario_OffsetsArriveWithSprites places a remote player's sprites with the scroll offsets that
// arrived with them: a location packet from a later frame (the screen scrolled on) without new sprites yet must not
// shift the old sprites.
func TestRemoteSpritesScenario_OffsetsArriveWithSprites(t *testing.T) {
	h := newRSScenario(t)
	h.frames(8)
	g := h.gs[0].g
	before, draw, _ := g.buildRemoteSpritesTable(time.Now())
	if !draw {
		t.Fatalf("expected the remote player to be drawn")
	}

	p := &g.players[1]
	p.XOffs += 0x40
	p.YOffs += 0x20
	after, _, _ := g.buildRemoteSpritesTable(time.Now())
	if before != after {
		t.Errorf("sprites moved with offsets that came without sprites:\n% x\n% x", before[:], after[:])
	}
}

// The water ripples (and tall grass) the game draws under Link's feet instead of his shadow are in his own OAM
// entries; they are sent without tile data like the shadow and drawn from the local VRAM.
func TestRemoteSpritesScenario_RipplesUnderLink(t *testing.T) {
	h := newRSScenario(t)
	e := h.gs[1].e
	copy(e.WRAM[0x0800+0x66*4:], []byte{0x50, 0x68, 0xD9, 0x24})
	copy(e.WRAM[0x0800+0x67*4:], []byte{0x58, 0x68, 0xD9, 0x64})
	h.frames(8)

	h.expectOAM("ripples", 0, 0x7E, [4]byte{0x40, 0x60, 0xD9, 0x24}, 0x00)
	h.expectOAM("ripples", 0, 0x7F, [4]byte{0x48, 0x60, 0xD9, 0x64}, 0x00)
	h.expectOAM("body", 0, 0x7D, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)

	// tall grass:
	copy(e.WRAM[0x0800+0x66*4:], []byte{0x50, 0x68, 0xC8, 0x22})
	copy(e.WRAM[0x0800+0x67*4:], []byte{0x58, 0x68, 0xC8, 0x62})
	h.frames(8)
	h.expectOAM("grass", 0, 0x7E, [4]byte{0x40, 0x60, 0xC8, 0x22}, 0x00)
	h.expectOAM("grass", 0, 0x7F, [4]byte{0x48, 0x60, 0xC8, 0x62}, 0x00)
}

// The death poof of an enemy (synced by alttpo, so every player draws it already) is not sent even while something
// else is captured; the same chr away from dying enemies is.
func TestRemoteSpritesScenario_EnemyDeathNotSent(t *testing.T) {
	for _, state := range []uint8{0x02, 0x04, 0x06} {
		h := newRSScenario(t)
		e := h.gs[1].e
		// a bomb, so that the OAM buffer is scanned:
		e.WRAM[0x0C4A+2] = 0x07
		// enemy in slot 3 at world ($1060,$0240) is at screen ($60,$40) with g2's scroll ($1000,$0200):
		e.WRAM[0x0DD0+3] = state
		e.WRAM[0x0D10+3], e.WRAM[0x0D30+3] = 0x60, 0x10
		e.WRAM[0x0D00+3], e.WRAM[0x0D20+3] = 0x40, 0x02
		e.WRAM[0x0F70+3] = 0
		copy(e.WRAM[0x0800+0x20*4:], []byte{0x62, 0x44, 0x9B, 0x04})
		copy(e.WRAM[0x0800+0x21*4:], []byte{0xC0, 0x40, 0x9B, 0x04})
		h.frames(8)

		var got []uint8
		for _, sp := range h.gs[0].g.players[1].Sprites.list {
			if sp.b[2] == 0x9B {
				got = append(got, sp.index)
			}
		}
		if len(got) != 1 || got[0] != 0x21 {
			t.Errorf("state $%02x: expected only the $9B away from the enemy (OAM $21) to be sent; got %02x", state, got)
		}
	}
}

// setTestSwordShield gives every player a sword and shield with distinct tiles, in o2's copy and in WRAM $7E9000 (where
// the game keeps them), as if the game and o2 had built them from the ROM.
func (h *skScenario) setTestSwordShield() (tiles [swordShieldTileCount << 5]byte) {
	for i := range tiles {
		tiles[i] = uint8(i*7 + i>>5)
	}
	for _, gs := range h.gs {
		s := &gs.g.swordShield
		s.ok, s.swordOK, s.shieldOK = true, true, true
		s.swordLevel, s.shieldLevel = 1, 1
		s.tiles = tiles
		gs.e.WRAM[0xF359], gs.e.WRAM[0xF35A] = 1, 1
		copy(gs.e.WRAM[0x9000:], tiles[:])
	}
	return
}

// A remote o2 player's sword and shield are drawn in their pose (unique tiles $380..$3A3) with the local player's
// sword and shield tiles from WRAM, uploaded into free VRAM slots like the head and body; only the rows the sprites
// show are uploaded.
func TestRemoteSpritesScenario_SwordAndShield(t *testing.T) {
	h := newRSScenario(t)
	h.setTestSwordShield()
	e := h.gs[1].e
	setOAM := func(i int, x, y, chr, attr, ext uint8) {
		copy(e.WRAM[0x0800+i*4:], []byte{x, y, chr, attr})
		e.WRAM[0x0A20+i] = ext
	}
	// the sword as two 8x8 sprites from chrs $05 and $15, the shield as a 16x16 sprite from chr $07, behind Link:
	setOAM(0x68, 0x48, 0x50, 0x05, 0x26, 0x00)
	setOAM(0x69, 0x48, 0x58, 0x15, 0x26, 0x00)
	setOAM(0x6A, 0x58, 0x58, 0x07, 0x26, 0x02)
	// the poses: sword tiles 3 and 15, shield tiles $1A and $20 (the bottom row is $C0 bytes after the top row):
	for k, v := range []uint16{0x9060, 0x91E0, 0x9340, 0x9400} {
		binary.LittleEndian.PutUint16(e.WRAM[0x0AC0+k*2:], v)
	}
	h.frames(8)

	// back to front: the shadow, the shield, the sword, the body and the head:
	h.expectShadow("sword and shield", 0x40, 0x60)
	h.expectOAM("shield", 0, 0x7D, [4]byte{0x48, 0x50, 0x0C, 0x26}, 0x02)
	h.expectOAM("sword bottom", 0, 0x7C, [4]byte{0x38, 0x50, 0x34, 0x26}, 0x00)
	h.expectOAM("sword top", 0, 0x7B, [4]byte{0x38, 0x48, 0x24, 0x26}, 0x00)
	h.expectOAM("body", 0, 0x7A, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x79, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	h.expectOAMUnused("nothing else", 0, 0x78)

	if rows, expected := h.nmiRows(0), [][3]uint16{
		{0x4200, 0x9000, 0}, {0x4300, 0x9200, 0}, {0x4220, 0x8400, 0}, {0x4320, 0x8600, 0},
		{0x4240, 0x9060, partWRAM}, {0x4340, 0x91E0, partWRAM}, {0x40C0, 0x9340, partWRAM}, {0x41C0, 0x9400, partWRAM},
	}; !equalRows(rows, expected) {
		t.Errorf("NMI rows %04x; expected %04x", rows, expected)
	}
	g1 := h.gs[0]
	vram := runNMIHook(t, g1.e, g1.g)
	for _, c := range []struct {
		vram uint16
		src  uint32
	}{{0x4240, 0x9060}, {0x4340, 0x91E0}, {0x40C0, 0x9340}, {0x41C0, 0x9400}} {
		if actual, expected := vram.bytes(c.vram, 0x40), g1.e.WRAM[c.src:c.src+0x40]; !bytes.Equal(actual, expected) {
			t.Errorf("VRAM $%04x = % x; expected WRAM $7E%04x = % x", c.vram, actual, c.src, expected)
		}
	}
	if vram.dmaBank(0) != 0x7E {
		t.Errorf("DMA source bank not restored to $7E: $%02x", vram.dmaBank(0))
	}

	// only the top row of the sword shows:
	setOAM(0x69, 0x48, 0xF0, 0x15, 0x26, 0x00)
	h.frames(8)
	if rows := h.nmiRows(0); len(rows) != 7 || rows[4] != [3]uint16{0x4240, 0x9060, partWRAM} || rows[5][0] != 0x40C0 {
		t.Errorf("NMI rows %04x; expected only the sword's top row", rows)
	}

	// o2 has no tiles of the local player's shield (e.g. no shield; items sync, so the remote player has none either):
	// the remote player's isn't drawn:
	g1.g.swordShield.shieldOK = false
	h.frames(8)
	h.expectOAM("sword top", 0, 0x7D, [4]byte{0x38, 0x48, 0x24, 0x26}, 0x00)
	h.expectOAM("body", 0, 0x7C, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x7B, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	h.expectOAMUnused("no shield", 0, 0x7A)
}

// alttpo sends the sword and shield tiles themselves (once per chr and packet) instead of unique tile indices; o2 finds
// the pose by looking the tiles up in its copy of the local player's tiles.
func TestRemoteSpritesScenario_AlttpoSwordPose(t *testing.T) {
	h := newRSScenario(t)
	tiles := h.setTestSwordShield()
	g := h.gs[0].g
	tile := func(i int) (d [32]byte) { copy(d[:], tiles[i<<5:]); return }
	none := [4]uint16{noUniqTile, noUniqTile, noUniqTile, noUniqTile}
	sword := func(index, chr uint8, gfx *[32]byte) remoteSprite {
		sp := remoteSprite{index: index, b: [5]uint8{0x48, 0x50, chr, 0x26, 0}, absidx: none}
		if gfx != nil {
			sp.gfx[0], sp.hasGfx = *gfx, true
		}
		return sp
	}
	t3, t4, t15, t9 := tile(3), tile(4), tile(15), tile(9)

	// chrs $05 and $06 show tiles 3 and 4, chr $15 tile 15; a second sprite of chr $05 comes without tiles:
	parts, sprites := g.remoteEquipment([]remoteSprite{
		sword(0x10, 0x05, &t3), sword(0x11, 0x06, &t4), sword(0x12, 0x15, &t15), sword(0x13, 0x05, nil),
	})
	if len(parts) != 1 || parts[0] != (remotePart{src: [2]uint16{0x9060, 0x91E0}, flags: partWRAM | partTopRow | partBottomRow}) {
		t.Errorf("parts %+v; expected the sword from $9060 and $91E0", parts)
	}
	if len(sprites) != 4 || sprites[0].off != 0x00 || sprites[1].off != 0x01 || sprites[2].off != 0x10 || sprites[3].off != 0x00 {
		t.Errorf("sprites %+v", sprites)
	}

	// chrs $05 and $06 that aren't next to each other in the local tiles: not the local player's sword:
	if parts, _ := g.remoteEquipment([]remoteSprite{sword(0x10, 0x05, &t3), sword(0x11, 0x06, &t9)}); len(parts) != 0 {
		t.Errorf("parts %+v; expected none", parts)
	}
	// tiles that aren't the local player's at all:
	var other [32]byte
	for i := range other {
		other[i] = 0xA5
	}
	if parts, _ := g.remoteEquipment([]remoteSprite{sword(0x10, 0x05, &other)}); len(parts) != 0 {
		t.Errorf("parts %+v; expected none", parts)
	}

	// the tiles are kept when the sprites are received:
	msg := []byte{1, 0x85, 0x48, 0x50, 0x05, 0x26, 0x00, 0xFF, 0xFF}
	msg = append(msg, t3[:]...)
	p := &g.players[1]
	if err := g.deserializeRemoteSprites(p, bytes.NewReader(msg), 0); err != nil {
		t.Fatal(err)
	}
	if sp := p.Sprites.list[0]; !sp.hasGfx || sp.gfx[0] != t3 || sp.index != 0x05 || sp.absidx[0] != noUniqTile {
		t.Errorf("received %+v", sp)
	}
}

// setModuleDrawsAt makes g1's module draw 8x8 sprites with chr at the OAM entries index.
func (h *skScenario) setModuleDrawsAt(chr uint8, index ...int) {
	var code []byte
	for _, i := range index {
		o := uint16(0x0800 + i*4)
		code = append(code,
			0xA9, chr, 0x8D, uint8(o+2), uint8((o+2)>>8),
			0xA9, 0x20, 0x8D, uint8(o+1), uint8((o+1)>>8))
	}
	code = append(code, 0x6B)
	for i, b := range code {
		h.gs[0].e.Bus.EaWrite(testROMModuleRouting+uint32(i), b)
	}
}

// Remote sprites go into the unused OAM entries from the end of the buffer down, skipping those the game used.
func TestRemoteSpritesScenario_SkipsUsedEntries(t *testing.T) {
	h := newRSScenario(t)
	if isCaptureChr(0x45) {
		t.Fatal("chr $45 is captured; pick another")
	}
	h.setModuleDrawsAt(0x45, 0x7F, 0x7D, 0x7C, 0x78, 0x77, 0x76, 0x75, 0x74)
	h.frames(8)
	for _, i := range []int{0x7F, 0x7D, 0x7C, 0x78, 0x77, 0x76, 0x75, 0x74} {
		if entry, _ := h.oamEntry(0, i); entry[1] != 0x20 || entry[2] != 0x45 {
			t.Errorf("game's OAM[$%02x] = % x was overwritten", i, entry[:])
		}
	}
	h.expectOAM("shadow right", 0, 0x7E, [4]byte{0x48, 0x60, 0x6C, 0x68}, 0x00)
	h.expectOAM("shadow left", 0, 0x7B, [4]byte{0x40, 0x60, 0x6C, 0x28}, 0x00)
	h.expectOAM("body", 0, 0x7A, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
	h.expectOAM("head", 0, 0x79, [4]byte{0x40, 0x50, 0x22, 0x2E}, 0x02)
	h.expectOAMUnused("nothing else", 0, 0x73)

	// all but the first 3 entries used: the sprites go into those, and what doesn't fit is not drawn:
	used := make([]int, 0, 0x80)
	for i := 0x03; i < 0x80; i++ {
		used = append(used, i)
	}
	h.setModuleDrawsAt(0x45, used...)
	h.frames(2)
	h.expectOAM("shadow right", 0, 0x02, [4]byte{0x48, 0x60, 0x6C, 0x68}, 0x00)
	h.expectOAM("shadow left", 0, 0x01, [4]byte{0x40, 0x60, 0x6C, 0x28}, 0x00)
	h.expectOAM("body", 0, 0x00, [4]byte{0x40, 0x58, 0x20, 0x6E}, 0x02)
}

// The follower slots the parts got keep their tiles: the follower hook leaves the game's uploads into them out, and
// the late hook uploads their rows only when they change.
func TestRemoteSpritesScenario_FollowerSlotsKept(t *testing.T) {
	h := newRSScenario(t)
	e, g := h.gs[0].e, h.gs[0].g
	// Link shows his shield (chr $07, the last slot), as he does nearly all the time:
	h.setModuleDraws([2]uint8{0x07, 0x00})
	h.frames(8)
	if used := h.remoteState(0, rsUsed)[0]; used != 0x40 {
		t.Fatalf("slots in use $%02x; expected $40", used)
	}
	// the body got slot $20 and the head $22:
	if skip := h.remoteState(0, rsSkipSlots)[0]; skip != 0x03 {
		t.Fatalf("kept follower slots $%02x; expected $03", skip)
	}
	uploads := func(when string) map[uint16]bool {
		t.Helper()
		f := runNMIHookAt(t, e, g, nmiFollowerPatchAddr)
		if mask := f.started; mask != 0x04 {
			t.Errorf("%s: follower hook started channels $%02x; expected $04 (the item get only)", when, mask)
		}
		rows := map[uint16]bool{}
		for _, w := range runNMIHook(t, e, g).writes {
			rows[w&^0x1F] = true
		}
		return rows
	}
	if rows := uploads("first"); !rows[0x4200] || !rows[0x4300] || !rows[0x4220] || !rows[0x4320] {
		t.Errorf("first: uploaded rows %v; expected the body's and head's", rows)
	}
	if rows := uploads("unchanged"); len(rows) != 0 {
		t.Errorf("unchanged: uploaded rows %v; expected none", rows)
	}

	// a frame that keeps no follower slot (nothing drawn): the game uploads its own tiles into all of them, so the
	// rows are uploaded again on the next frame that draws:
	skip := h.remoteState(0, rsSkipSlots)
	skip[0] = 0
	if f := runNMIHookAt(t, e, g, nmiFollowerPatchAddr); f.started != 0x07 {
		t.Errorf("nothing kept: follower hook started channels $%02x; expected $07", f.started)
	}
	skip[0] = 0x03
	if rows := uploads("after the game's upload"); !rows[0x4200] || !rows[0x4220] {
		t.Errorf("after the game's upload: uploaded rows %v; expected the body's and head's", rows)
	}

	// a follower shows chr $22: the head moves to slot $24; the game uploads its tiles into $22, the body's rows stay:
	h.setModuleDraws([2]uint8{0x07, 0x00}, [2]uint8{0x22, 0x02})
	h.frames(2)
	if skip := h.remoteState(0, rsSkipSlots)[0]; skip != 0x05 {
		t.Fatalf("kept follower slots $%02x; expected $05", skip)
	}
	f := runNMIHookAt(t, e, g, nmiFollowerPatchAddr)
	if mask := f.started; mask != 0x02 {
		t.Errorf("follower: follower hook started channels $%02x; expected $02", mask)
	}
	rows := map[uint16]bool{}
	for _, w := range runNMIHook(t, e, g).writes {
		rows[w&^0x1F] = true
	}
	if rows[0x4200] || rows[0x4300] || !rows[0x4240] || !rows[0x4340] {
		t.Errorf("follower: uploaded rows %v; expected the head's in slot $24 only", rows)
	}
}

// The follower hooks upload the follower and item get tiles into the slots the parts didn't get, at their own VRAM
// addresses: leaving channels out must not shift the others' tiles into the kept slots.
func TestRemoteSpritesScenario_FollowerUploadsInPlace(t *testing.T) {
	h := newRSScenario(t)
	e, g := h.gs[0].e, h.gs[0].g
	h.frames(8)
	if skip := h.remoteState(0, rsSkipSlots)[0]; skip != 0x03 {
		t.Fatalf("kept follower slots $%02x; expected $03", skip)
	}
	// NMI_DoUpdates set VMADDR and the three channels (64 bytes each) before each hook:
	channels := func(vaddr uint16) func(d *dmaSim) {
		return func(d *dmaSim) {
			d.regs[0x2116], d.regs[0x2117] = uint8(vaddr), uint8(vaddr>>8)
			for ch := uint16(0); ch < 3; ch++ {
				r := 0x4300 + ch<<4
				d.regs[r+0], d.regs[r+1], d.regs[r+4], d.regs[r+5], d.regs[r+6] = 0x01, 0x18, 0x7E, 0x40, 0x00
			}
		}
	}
	for _, c := range []struct {
		addr, vaddr uint32
	}{{nmiFollowerPatchAddr, 0x4200}, {nmiFollowerBottomPatchAddr, 0x4300}} {
		d := runNMIHookAt(t, e, g, c.addr, channels(uint16(c.vaddr)))
		if len(d.writes) != 0x20 {
			t.Errorf("$%06x: %d words uploaded; expected the item get's $20", c.addr, len(d.writes))
		}
		for _, w := range d.writes {
			if w < uint16(c.vaddr)+0x40 || w >= uint16(c.vaddr)+0x60 {
				t.Errorf("$%06x: uploaded VRAM $%04x; expected only slot $24 at $%04x..$%04x", c.addr, w, c.vaddr+0x40, c.vaddr+0x5F)
				break
			}
		}
	}
}
