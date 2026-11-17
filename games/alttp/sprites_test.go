package alttp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"o2/client"
	"o2/client/protocol04"
	"o2/snes"
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

type spriteTestRig struct {
	t  *testing.T
	gs gameSync
}

func newSpriteTestRig(t *testing.T) *spriteTestRig {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// pretend the server assigned us an index:
	gs.g.local.IndexF = 2
	gs.g.players[2] = *gs.g.local
	gs.g.local = &gs.g.players[2]

	h := &spriteTestRig{t: t, gs: gs}

	// fill LinkGraphics with a recognizable pattern: tile i, byte j = i*7 + j
	g := gs.g
	for i := 0; i < linkUniqTileCount; i++ {
		pak, err := lorom.BusAddressToPak(uint32(g.linkSprites.tileBank)<<16 | (0x8000 + uint32(i)<<5))
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 32; j++ {
			g.rom.Contents[int(pak)+j] = uint8(i*7 + j)
		}
	}

	e := gs.e
	e.WRAM[0x10] = 0x07
	e.WRAM[0x11] = 0x00

	// Link's OAM entries start at OAM index $64:
	e.WRAM[0x0352], e.WRAM[0x0353] = 0x90, 0x01
	for i := 0; i < 0x80; i++ {
		// disable all sprites:
		e.WRAM[0x0800+i*4+1] = 0xF0
	}
	setOAM := func(i int, x, y, chr, attr, ext uint8) {
		copy(e.WRAM[0x0800+i*4:], []byte{x, y, chr, attr})
		e.WRAM[0x0A20+i] = ext
	}
	// head (16x16, palette 7):
	setOAM(0x64, 0x70, 0x60, 0x00, 0x0E, 0x02)
	// body (16x16, palette 7):
	setOAM(0x65, 0x70, 0x68, 0x02, 0x0E, 0x02)
	// sword (8x8; tiles are in WRAM, not ROM):
	setOAM(0x66, 0x78, 0x58, 0x05, 0x0A, 0x00)
	// shadow (16x16):
	setOAM(0x67, 0x70, 0x6C, 0x6C, 0x08, 0x02)
	// aux (8x8, off screen to the left; x = -8 + 256 with x bit 8 set):
	setOAM(0x68, 0xF8, 0x60, 0x04, 0x0E, 0x01)

	// graphics DMA sources: head $8400/$8600, body $9000/$9200, aux $9400/$9420
	for k, v := range []uint16{0x8400, 0x8600, 0x9000, 0x9200, 0x9400, 0x9420} {
		e.WRAM[0x0ACC+k*2] = uint8(v)
		e.WRAM[0x0ACC+k*2+1] = uint8(v >> 8)
	}

	// Link's palette:
	for j := 0; j < 32; j++ {
		e.WRAM[0xC6E0+j] = uint8(0xA0 + j)
	}

	return h
}

// frames runs n game frames and returns everything o2 sent to alttpo clients during them.
func (h *spriteTestRig) frames(n int) (parsed alttpoParsed) {
	h.t.Helper()
	parsed.tiles = map[uint16][]byte{}
	for f := 0; f < n; f++ {
		h.gs.runFrame(h.t)
		for len(h.gs.c.Wr) > 0 {
			b := <-h.gs.c.Wr
			var proto uint8
			if _, err := client.ParseHeader(b, &proto); err != nil {
				h.t.Fatal(err)
			}
			if proto != protocol04.Protocol {
				continue
			}
			var gm protocol04.GroupMessage
			if err := protocol04.Unmarshal(b[3:], &gm); err != nil {
				h.t.Fatal(err)
			}
			payload := gm.Data
			if gm.Kind != protocol04.BroadcastAll || (len(payload) > 3 && payload[3] >= 0x80) {
				continue
			}
			_, p, err := alttpoParseFull(payload)
			if err != nil {
				h.t.Fatalf("alttpo would fail to parse o2 packet: %v", err)
			}
			if len(payload) > 3 && payload[3] == byte(MsgSprites1) {
				// latest sprites:
				parsed.sprites = p.sprites
			}
			for idx, tile := range p.tiles {
				if _, dup := parsed.tiles[idx]; dup {
					h.t.Errorf("unique tile %#03x sent more than once", idx)
				}
				parsed.tiles[idx] = tile
			}
		}
	}
	return
}

func (h *spriteTestRig) romTile(idx uint16) []byte {
	tile, ok := h.gs.g.linkTile(idx)
	if !ok {
		h.t.Fatalf("tile %#03x not in ROM", idx)
	}
	return tile
}

func TestLinkSprites_SentToAlttpo(t *testing.T) {
	h := newSpriteTestRig(t)
	parsed := h.frames(4)

	// head, body and shadow; sword skipped; aux is off screen:
	if len(parsed.sprites) != 3 {
		t.Fatalf("expected 3 sprites; got %d: %+v", len(parsed.sprites), parsed.sprites)
	}

	head, body, shadow := parsed.sprites[0], parsed.sprites[1], parsed.sprites[2]
	if !bytes.Equal(head.b[:], []byte{0x64, 0x70, 0x60, 0x00, 0x0E, 0x82}) {
		t.Errorf("unexpected head OAM bytes % x", head.b)
	}
	if !bytes.Equal(head.palette, h.gs.e.WRAM[0xC6E0:0xC700]) {
		t.Errorf("expected Link's palette with the first sprite; got % x", head.palette)
	}
	if expected := []uint16{0x20, 0x21, 0x30, 0x31}; !equalU16(head.absidx, expected) {
		t.Errorf("head: expected uniqtiles %x; got %x", expected, head.absidx)
	}
	if body.b[5]&0x80 != 0 {
		t.Errorf("palette should only be sent once per packet")
	}
	if expected := []uint16{0x80, 0x81, 0x90, 0x91}; !equalU16(body.absidx, expected) {
		t.Errorf("body: expected uniqtiles %x; got %x", expected, body.absidx)
	}
	if shadow.b[3] != 0x6C || !equalU16(shadow.absidx, []uint16{noUniqTile, noUniqTile, noUniqTile, noUniqTile}) {
		t.Errorf("shadow: expected chr $6C without uniqtiles; got % x %x", shadow.b, shadow.absidx)
	}

	// every referenced tile was sent exactly once with the data from ROM:
	for _, idx := range append(head.absidx, body.absidx...) {
		tile, ok := parsed.tiles[idx]
		if !ok {
			t.Errorf("tile %#03x was not sent", idx)
			continue
		}
		if !bytes.Equal(tile, h.romTile(idx)) {
			t.Errorf("tile %#03x: data does not match ROM", idx)
		}
	}
	if len(parsed.tiles) != 8 {
		t.Errorf("expected 8 tiles sent; got %d", len(parsed.tiles))
	}

	// a new pose only sends the new tiles:
	e := h.gs.e
	e.WRAM[0x0AD0], e.WRAM[0x0AD1] = 0x00, 0xA0 // body top row now $A000
	parsed = h.frames(2)
	if expected := []uint16{0x100, 0x101}; len(parsed.tiles) != 2 || parsed.tiles[0x100] == nil || parsed.tiles[0x101] == nil {
		t.Errorf("expected only new tiles %x to be sent; got %d tiles", expected, len(parsed.tiles))
	}
}

// TestLinkSprites_ReadAfterOAMCleared reads WRAM only while the OAM buffer is cleared, as at the start of a frame
// after ClearOAMBuffer; Link's sprites are still sent from the copy the patched ROM made after the module drew them.
func TestLinkSprites_ReadAfterOAMCleared(t *testing.T) {
	h := newSpriteTestRig(t)
	e := h.gs.e
	var drawn [0x200]byte
	copy(drawn[:], e.WRAM[0x0800:0x0A00])
	clearOAM := func() {
		for i := 0; i < 0x80; i++ {
			e.WRAM[0x0800+i*4+1] = 0xF0
		}
	}
	// the module draws Link's sprites during the emulated frame, and they are cleared again before o2 reads:
	h.gs.beforeEmulate = func() { copy(e.WRAM[0x0800:0x0A00], drawn[:]) }
	clearOAM()

	var parsed alttpoParsed
	for f := 0; f < 4; f++ {
		p := h.frames(1)
		if p.sprites != nil {
			parsed.sprites = p.sprites
		}
		clearOAM()
	}
	if len(parsed.sprites) != 3 || parsed.sprites[0].b[0] != 0x64 {
		t.Fatalf("expected Link's 3 sprites from OAM index $64; got %+v", parsed.sprites)
	}
}

func TestLinkSprites_OAMMoves(t *testing.T) {
	h := newSpriteTestRig(t)
	h.frames(2)

	// Link's OAM entries move to OAM index $38:
	e := h.gs.e
	copy(e.WRAM[0x0800+0x38*4:], e.WRAM[0x0800+0x64*4:0x0800+0x64*4+12*4])
	copy(e.WRAM[0x0A20+0x38:], e.WRAM[0x0A20+0x64:0x0A20+0x64+12])
	for i := 0x64; i < 0x64+12; i++ {
		e.WRAM[0x0800+i*4+1] = 0xF0
	}
	e.WRAM[0x0352], e.WRAM[0x0353] = 0xE0, 0x00

	parsed := h.frames(3)
	if len(parsed.sprites) != 3 || parsed.sprites[0].b[0] != 0x38 {
		t.Fatalf("expected Link's 3 sprites from OAM index $38; got %+v", parsed.sprites)
	}
}

func TestLinkSprites_NakResendsTiles(t *testing.T) {
	h := newSpriteTestRig(t)
	h.frames(3)

	// an alttpo player is missing two of our tiles:
	p := newAlttpoPacket(5, 1)
	p.u8(0x16).u16(2).u8(2).u16(0x21).u16(0x90)
	p.send(t, h.gs.g)
	// a NAK for another player is ignored:
	p = newAlttpoPacket(5, 1)
	p.u8(0x16).u16(7).u8(1).u16(0x30)
	p.send(t, h.gs.g)

	parsed := h.frames(1)
	if len(parsed.tiles) != 2 || !bytes.Equal(parsed.tiles[0x21], h.romTile(0x21)) || !bytes.Equal(parsed.tiles[0x90], h.romTile(0x90)) {
		t.Errorf("expected tiles $021 and $090 to be re-sent; got %d tiles", len(parsed.tiles))
	}
}

func equalU16(a, b []uint16) bool {
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

// splitQueue splits reads into commands of up to 8 reads like the FX Pak Pro driver, optionally failing one.
type splitQueue struct {
	testQueue
	failCommand int
}

type failingReadCommand struct{}

func (failingReadCommand) Execute(snes.Queue, snes.KeepAlive) error { return errors.New("read failed") }

func (q *splitQueue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) (cmds snes.CommandSequence) {
	for i := 0; len(reqs) > 0; i++ {
		n := len(reqs)
		if n > 8 {
			n = 8
		}
		var cmd snes.Command = testReadCommand(reqs[:n])
		if i+1 == q.failCommand {
			cmd = failingReadCommand{}
		}
		cmds = append(cmds, snes.CommandWithCompletion{Command: cmd, Completion: batchComplete})
		reqs = reqs[n:]
	}
	return
}

func (q *splitQueue) Enqueue(cmd snes.CommandWithCompletion) error {
	err := cmd.Command.Execute(&q.testQueue, nil)
	if cmd.Completion != nil {
		cmd.Completion(cmd.Command, err)
	}
	return nil
}

func TestReadSubmit_CompletesOncePerSequence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failCommand int
		responses   int
	}{
		{"all succeed", 0, 18},
		{"first fails", 1, 0},
		{"second fails", 2, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs, err := createTestGameSync("VT test", "o2player", io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			g := gs.g
			g.queue = &splitQueue{testQueue: testQueue{E: gs.e}, failCommand: tc.failCommand}

			reads := make([]snes.Read, 0, 18)
			for i := 0; i < 18; i++ {
				reads = g.readEnqueue(reads, 0xF50000+uint32(i)*0x10, 0x10, nil)
			}
			g.readSubmit(reads)

			if n := len(g.readComplete); n != 1 {
				t.Fatalf("expected exactly 1 completed read sequence; got %d", n)
			}
			if rsps := <-g.readComplete; len(rsps) != tc.responses {
				t.Errorf("expected %d responses; got %d", tc.responses, len(rsps))
			}
		})
	}
}

func TestLinkSprites_SwordAndShield(t *testing.T) {
	h := newSpriteTestRig(t)
	g, e := h.gs.g, h.gs.e

	// the synthetic test ROM has no game code to find, so provide the sword and shield tiles directly:
	ss := &g.swordShield
	ss.swordOK, ss.shieldOK = true, true
	for i := range ss.tiles {
		ss.tiles[i] = uint8(0x55 ^ i)
	}

	// sword (8x8, palette 5) from $9020 and shield (16x16, palette 5) from $9340/$9380:
	e.WRAM[0x0AC0], e.WRAM[0x0AC1] = 0x20, 0x90
	e.WRAM[0x0AC4], e.WRAM[0x0AC5] = 0x40, 0x93
	e.WRAM[0x0AC6], e.WRAM[0x0AC7] = 0x80, 0x93
	copy(e.WRAM[0x0800+0x69*4:], []byte{0x68, 0x64, 0x07, 0x0A})
	e.WRAM[0x0A20+0x69] = 0x02
	for j := 0; j < 32; j++ {
		e.WRAM[0xC6A0+j] = uint8(0x50 + j)
	}

	parsed := h.frames(4)
	var sword, shield *alttpoSprite
	for i := range parsed.sprites {
		switch parsed.sprites[i].b[3] {
		case 0x05:
			sword = &parsed.sprites[i]
		case 0x07:
			shield = &parsed.sprites[i]
		}
	}
	if sword == nil || shield == nil {
		t.Fatalf("expected sword and shield sprites; got %+v", parsed.sprites)
	}
	if !equalU16(sword.absidx, []uint16{0x381}) {
		t.Errorf("sword: expected uniqtile $381; got %x", sword.absidx)
	}
	if !equalU16(shield.absidx, []uint16{0x39A, 0x39B, 0x39C, 0x39D}) {
		t.Errorf("shield: expected uniqtiles $39A,$39B,$39C,$39D; got %x", shield.absidx)
	}
	// palette 5 goes with the first sprite using it (the sword comes first in OAM):
	if !bytes.Equal(sword.palette, e.WRAM[0xC6A0:0xC6C0]) || shield.palette != nil {
		t.Errorf("expected palette 5 with the sword only")
	}
	for _, idx := range append(sword.absidx, shield.absidx...) {
		expected, _ := g.swordShieldTile(idx)
		if !bytes.Equal(parsed.tiles[idx], expected) {
			t.Errorf("tile %#03x: expected sword/shield tile data", idx)
		}
	}
}

// alttpo draws remote sprites only with palettes the remote player sent, so every palette used is sent: Link's,
// the shadow's and the dash dust's.
func TestLinkSprites_PalettesSentForEffects(t *testing.T) {
	h := newSpriteTestRig(t)
	e := h.gs.e
	for i := 0; i < 0x100; i++ {
		e.WRAM[0xC600+i] = uint8(i)
	}
	e.WRAM[0x0C4A+3] = 0x1E
	copy(e.WRAM[0x0800+0x10*4:], []byte{0x68, 0x70, 0xCF, 0x04})
	e.WRAM[0x0A20+0x10] = 0
	// pretend a remote player is in the same location:
	h.gs.beforeEmulate = func() { e.SRAM[(remoteTableAddr+rtCaptureTTL)&0x7FFF] = 2 }
	parsed := h.frames(4)

	pal := map[uint8][]byte{}
	for _, sp := range parsed.sprites {
		if sp.palette != nil {
			pal[(sp.b[4]>>1)&7] = sp.palette
		}
	}
	for _, p := range []uint8{2, 4, 7} {
		if expected := e.WRAM[0xC600+int(p)*32 : 0xC620+int(p)*32]; !bytes.Equal(pal[p], expected) {
			t.Errorf("palette %d: sent % x; expected % x", p, pal[p], expected)
		}
	}
}

// TestLinkSprites_NotSentInGameOver sends no sprites while the GAME OVER animation and menu show (module $12 from
// submodule $06), since those sprites are not Link's and alttpo draws them garbled.
func TestLinkSprites_NotSentInGameOver(t *testing.T) {
	h := newSpriteTestRig(t)
	e := h.gs.e

	// dying; Link still spins:
	e.WRAM[0x10], e.WRAM[0x11] = 0x12, 0x05
	if parsed := h.frames(3); len(parsed.sprites) != 3 {
		t.Fatalf("expected Link's 3 sprites while dying; got %+v", parsed.sprites)
	}

	// GAME OVER:
	e.WRAM[0x11] = 0x06
	parsed := h.frames(3)
	if len(parsed.sprites) != 0 {
		t.Fatalf("expected an empty sprite list in GAME OVER; got %+v", parsed.sprites)
	}
}

// TestLinkSprites_ScrollFromCopy sends the scroll offsets Link's OAM entries were drawn with, from the copy the
// trampoline made, not the ones read from WRAM, which may already be from a later frame while the screen scrolls.
func TestLinkSprites_ScrollFromCopy(t *testing.T) {
	h := newSpriteTestRig(t)
	e := h.gs.e
	setScroll := func(x, y uint16) {
		binary.LittleEndian.PutUint16(e.WRAM[0xE2:], x)
		binary.LittleEndian.PutUint16(e.WRAM[0xE8:], y)
	}
	// screen shake:
	binary.LittleEndian.PutUint16(e.WRAM[0x011A:], 0x0002)
	binary.LittleEndian.PutUint16(e.WRAM[0x011C:], 0x0001)
	// the module draws Link with the screen at ($0700,$0E00):
	h.gs.beforeEmulate = func() { setScroll(0x0700, 0x0E00) }
	for f := 0; f < 3; f++ {
		h.frames(1)
		// and the screen scrolls on before o2 reads WRAM:
		setScroll(0x0740, 0x0E20)
	}

	local := h.gs.g.local
	if local.XOffs != 0x06FE || local.YOffs != 0x0DFF {
		t.Errorf("expected offsets ($06fe,$0dff) the sprites were drawn with; got ($%04x,$%04x)", local.XOffs, local.YOffs)
	}
}
