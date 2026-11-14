package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"o2/client"
	"o2/client/protocol02"
	"strings"
	"testing"
	"time"
)

// alttpoScriptProtocol is `script_protocol` from alttpo's GameState.as.
const alttpoScriptProtocol = 0x17

// alttpoParse is a model of alttpo's `GameState::deserialize` (GameState.as) used to verify that the packets o2 sends
// parse correctly in alttpo. alttpo does not stop on unknown message types (it logs and keeps reading the following
// bytes as message types), so any unknown type is treated as a failure here. It returns the message types parsed.
func alttpoParse(r []byte) (types []uint8, err error) {
	types, _, err = alttpoParseFull(r)
	return
}

type alttpoSprite struct {
	b       [6]uint8 // index, b0..b4 as received (flag bits included)
	absidx  []uint16
	palette []byte
}

type alttpoParsed struct {
	sprites []alttpoSprite
	tiles   map[uint16][]byte
}

func alttpoParseFull(r []byte) (types []uint8, parsed alttpoParsed, err error) {
	parsed.tiles = map[uint16][]byte{}
	c := 0
	need := func(n int) {
		if c+n > len(r) {
			panic(fmt.Errorf("read past end of packet at %d+%d > %d", c, n, len(r)))
		}
	}
	u8 := func() uint8 { need(1); c++; return r[c-1] }
	u16 := func() uint16 { need(2); c += 2; return binary.LittleEndian.Uint16(r[c-2:]) }
	skip := func(n int) { need(n); c += n }

	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%v", rec)
		}
	}()

	if u8() != alttpoScriptProtocol {
		return nil, parsed, fmt.Errorf("bad game protocol")
	}
	u8() // team
	u8() // frame

	for c < len(r) {
		t := u8()
		types = append(types, t)
		switch t {
		case 0x01: // location
			skip(3 + 3 + 2*9 + 1)
		case 0x02: // sfx
			skip(2)
		case 0x03, 0x04: // sprites
			if t == 0x04 {
				u8()
			}
			n := int(u8())
			for i := 0; i < n; i++ {
				var spr alttpoSprite
				need(6)
				copy(spr.b[:], r[c:c+6])
				fl, b1, b4 := u8(), (func() uint8 { skip(1); return u8() })(), (func() uint8 { skip(2); return u8() })()
				size := (b4 >> 1) & 1
				if b1 == 0xF0 {
					size = 0
				}
				spr.absidx = append(spr.absidx, u16())
				if size != 0 {
					spr.absidx = append(spr.absidx, u16(), u16(), u16())
				}
				if fl&0x80 != 0 {
					skip(32)
					if size != 0 {
						skip(96)
					}
				}
				if b4&0x80 != 0 {
					need(32)
					spr.palette = r[c : c+32]
					skip(32)
				}
				parsed.sprites = append(parsed.sprites, spr)
			}
		case 0x05: // wram
			n := int(u8())
			u16()
			skip(n * 6)
		case 0x06: // sram
			u8() // in_sm
			u16()
			skip(int(u16()))
		case 0x07: // tilemaps
			skip(4 + 3 + 1)
			n := int(u8())
			for i := 0; i < n; i++ {
				offs := u16()
				count := int(u8())
				if offs&0x8000 != 0 {
					skip(3)
				} else {
					skip(3 * count)
				}
			}
		case 0x09: // ancillae
			n := int(u8())
			for i := 0; i < n; i++ {
				if u8()&0x7F < 5 {
					skip(0x20)
				} else {
					skip(0x16)
				}
			}
		case 0x0A: // torches
			skip(int(u8()) * 2)
		case 0x0C: // name
			skip(20)
		case 0x15: // uniqtiles
			n := int(u8())
			for i := 0; i < n; i++ {
				idx := u16()
				need(32)
				parsed.tiles[idx] = r[c : c+32]
				skip(32)
			}
		default:
			// alttpo also understands 0x0B, 0x0D-0x14 and 0x16 but o2 never sends them:
			return types, parsed, fmt.Errorf("message type %#02x not expected from o2 (alttpo would misparse the rest)", t)
		}
	}
	return
}

// alttpoPacket builds a protocol 02 broadcast as relayed by the server from an alttpo client.
type alttpoPacket struct {
	bytes.Buffer
}

func newAlttpoPacket(index uint16, frame uint8) *alttpoPacket {
	p := &alttpoPacket{}
	p.Write(client.MakePacket(0x02).Bytes())
	p.WriteString(fmt.Sprintf("%-20s", "test"))
	p.WriteByte(0x81)
	p.u16(index)
	// script protocol, team, frame:
	p.WriteByte(alttpoScriptProtocol)
	p.WriteByte(0)
	p.WriteByte(frame)
	return p
}

func (p *alttpoPacket) u8(v ...uint8) *alttpoPacket { p.Write(v); return p }
func (p *alttpoPacket) u16(v uint16) *alttpoPacket {
	_ = binary.Write(p, binary.LittleEndian, v)
	return p
}
func (p *alttpoPacket) u32(v uint32) *alttpoPacket {
	_ = binary.Write(p, binary.LittleEndian, v)
	return p
}
func (p *alttpoPacket) zeros(n int) *alttpoPacket { p.Write(make([]byte, n)); return p }

// The writers below follow alttpo's LocalGameState.as serialize_* functions.

func (p *alttpoPacket) location(module, dungeon uint8, location uint32, x, y uint16) *alttpoPacket {
	p.u8(0x01, module, 0, 0)
	p.u8(uint8(location), uint8(location>>8), uint8(location>>16))
	p.u16(x).u16(y)
	p.u16(uint16(dungeon)).u16(0) // dungeon, entrance
	p.u16(0).u16(0)               // last overworld x, y
	p.u16(0).u16(0)               // xoffs, yoffs
	p.u16(0x1234)                 // player color
	p.u8(0)                       // in_sm
	return p
}

func (p *alttpoPacket) name(name string) *alttpoPacket {
	p.u8(0x0C)
	p.WriteString(fmt.Sprintf("%-20s", name))
	return p
}

func (p *alttpoPacket) sram(start uint16, data []byte) *alttpoPacket {
	p.u8(0x06, 0)
	p.u16(start).u16(uint16(len(data)))
	p.Write(data)
	return p
}

func (p *alttpoPacket) smallKeys(ts uint32, values [16]uint8) *alttpoPacket {
	p.u8(0x05, 0x10)
	p.u16(smallKeyFirst)
	for _, v := range values {
		p.u32(ts).u16(uint16(v))
	}
	return p
}

func (p *alttpoPacket) send(t *testing.T, g *Game) {
	t.Helper()
	if err := g.handleNetMessage(p.Bytes()); err != nil {
		t.Fatalf("o2 failed to handle alttpo packet: %v", err)
	}
}

// TestAlttpoCompat_O2PacketsParseInAlttpo captures every packet o2 sends over several hundred frames and verifies that
// all protocol 02 packets (which alttpo receives) parse completely under alttpo's rules, and that o2-only data only
// travels in protocol 03 packets (which alttpo ignores).
func TestAlttpoCompat_O2PacketsParseInAlttpo(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// pretend the server assigned us an index:
	gs.g.local.IndexF = 0
	gs.g.players[0] = *gs.g.local
	gs.g.local = &gs.g.players[0]
	gs.g.local.GameStartTime = time.Now()

	gs.e.WRAM[0x10] = 0x07
	gs.e.WRAM[0x040C] = 0x04
	gs.e.WRAM[0xF36F] = 1

	seen := map[uint8]int{}
	p2Count, p3Count := 0, 0
	for f := 0; f < 256; f++ {
		gs.runFrame(t)
		if f%30 == 0 {
			gs.g.sendPlayerName()
		}
		for len(gs.c.Wr) > 0 {
			b := <-gs.c.Wr
			var proto uint8
			rd, err := client.ParseHeader(b, &proto)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(rd)
			switch proto {
			case 0x02:
				p2Count++
				var h protocol02.Header
				br := bytes.NewReader(body)
				if err := protocol02.Parse(br, &h); err != nil {
					t.Fatal(err)
				}
				if h.Kind != protocol02.Broadcast {
					t.Fatalf("unexpected protocol 02 kind %v", h.Kind)
				}
				if strings.TrimSpace(string(h.Group[:])) != "test" {
					t.Fatalf("unexpected group %q", h.Group)
				}
				payload, _ := io.ReadAll(br)
				types, err := alttpoParse(payload)
				if err != nil {
					t.Fatalf("alttpo would fail to parse o2 packet %x: %v", payload, err)
				}
				for _, ty := range types {
					seen[ty]++
				}
			case 0x03:
				p3Count++
			default:
				t.Fatalf("unexpected server protocol %d", proto)
			}
		}
	}

	if p2Count == 0 || p3Count == 0 {
		t.Fatalf("expected both protocol 02 and 03 packets; got %d and %d", p2Count, p3Count)
	}
	for _, ty := range []uint8{0x01, 0x05, 0x06, 0x0C} {
		if seen[ty] == 0 {
			t.Errorf("expected o2 to send message type %#02x to alttpo", ty)
		}
	}
}

// TestAlttpoCompat_AlttpoPacketsParseInO2 feeds o2 packets built the way alttpo builds them, including every message
// type alttpo can send, and verifies o2 parses through all of them and applies the state it shares.
func TestAlttpoCompat_AlttpoPacketsParseInO2(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g
	const remote = 3

	// main packet: location, name, sfx, pvp, ancillae, crystal switch, small keys
	{
		p := newAlttpoPacket(remote, 10)
		p.location(0x07, 0x04, 0x010012, 0x1234, 0x5678)
		p.name("alttpo")
		p.u8(0x02, 0x11, 0x22) // sfx
		// pvp: active hitbox, 2 attacks:
		p.u8(0x0B, 1).u16(1).u16(2).u8(8, 8, 3, 4).u8(1, 2).u8(2)
		p.u16(0).u8(1, 2, 3, 4, 5, 6, 7)
		p.u16(1).u8(1, 2, 3, 4, 5, 6, 7)
		// ancillae: one front-slot (0x20 facts) and one back-slot (0x16 facts):
		p.u8(0x09, 2, 0x81).zeros(0x20).u8(0x06).zeros(0x16)
		// crystal switch:
		p.u8(0x05, 0x01).u16(0xC172).u32(5).u16(1)
		var keys [16]uint8
		keys[2] = 3
		p.smallKeys(0x01020304, keys)
		p.send(t, g)
	}

	rp := &g.players[remote]
	if rp.Name() != "alttpo" {
		t.Errorf("expected remote name %q; got %q", "alttpo", rp.Name())
	}
	if rp.Module != 0x07 || rp.Location != 0x010012 || rp.X != 0x1234 || rp.Y != 0x5678 || rp.Dungeon != 0x04 {
		t.Errorf("unexpected remote location: module=%02x location=%06x x=%04x y=%04x dungeon=%02x", rp.Module, rp.Location, rp.X, rp.Y, rp.Dungeon)
	}
	if w := rp.WRAM[smallKeyFirst+2]; w == nil || w.Value != 3 || w.Timestamp != 0x01020304 {
		t.Errorf("expected remote Eastern Palace small keys = 3 @ 01020304; got %+v", w)
	}

	// sprites packet with chr data, palettes and uniqtile indices, followed by uniqtiles and a NAK:
	{
		p := newAlttpoPacket(remote, 10)
		p.u8(0x16).u16(remote).u8(2).u16(1).u16(2)
		p.u8(0x03, 3)
		// small sprite with chr + palette:
		p.u8(0x80, 0x10, 0x20, 0x30, 0x40, 0x80).u16(0xFFFF).zeros(32).zeros(32)
		// large sprite with chr data (4 chrs) and uniqtile indices:
		p.u8(0x81, 0x10, 0x20, 0x30, 0x40, 0x02).u16(1).u16(2).u16(3).u16(4).zeros(32 * 4)
		// disabled large-flagged sprite (b1 = $F0 means size is not decoded):
		p.u8(0x02, 0x00, 0xF0, 0x00, 0x00, 0x02).u16(5)
		p.u8(0x04, 3, 1).u8(0x05, 0, 0, 0, 0, 0).u16(6)
		p.u8(0x15, 1).u16(7).zeros(32)
		p.name("alttpo2")
		p.send(t, g)
	}
	if rp.Name() != "alttpo2" {
		t.Errorf("expected parsing to continue past sprite messages; name = %q", rp.Name())
	}

	// enemy sync packet:
	{
		p := newAlttpoPacket(remote, 10)
		// enemy data for 2 sprites (dungeon):
		p.u8(0x12).u16(0x0003).u8(1).zeros(2 * 55)
		// enemy segment data: moldorm in slot 0, swamola in slot 7 (no data), default in slot 1:
		p.u8(0x13).u16(0x0083).u8(0x09).zeros(4 * 0x80).u8(0x30).zeros(7 * 0x10).u8(0xCF)
		// overlords 0 and 2:
		p.u8(0x14, 0x05).zeros(2 * 11)
		// torches:
		p.u8(0x0A, 2, 1, 5, 2, 6)
		// tilemaps: one "same" run and one varying run:
		p.u8(0x07).u32(1).u8(0x12, 0x00, 0x01).u8(0, 2)
		p.u16(0x8010).u8(4).u8(1, 2, 3)
		p.u16(0x4020).u8(2).u8(1, 2, 3, 4, 5, 6)
		p.name("alttpo3")
		p.send(t, g)
	}
	if rp.Name() != "alttpo3" {
		t.Errorf("expected parsing to continue past enemy messages; name = %q", rp.Name())
	}

	// SRAM packet:
	{
		p := newAlttpoPacket(remote, 11)
		p.sram(0x340, []byte{1, 2, 3, 4})
		p.sram(0x280, []byte{0x20})
		p.send(t, g)
	}
	if got := rp.SRAM.data[0x340:0x344]; !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Errorf("expected remote SRAM [$340..$343] = 01 02 03 04; got % x", got)
	}
	if rp.SRAM.data[0x280] != 0x20 {
		t.Errorf("expected remote SRAM [$280] = 20; got %02x", rp.SRAM.data[0x280])
	}

	// Super Metroid (SMZ3) packet:
	{
		p := newAlttpoPacket(remote, 12)
		p.u8(0x0F).zeros(15)
		p.u8(0x0D).zeros(0x54 + 2)
		p.u8(0x0E).zeros(0x40)
		p.u8(0x11, 34)
		p.u8(0x11, 1, 2, 0)
		p.u8(0x11, 1, 3, 1).zeros(64 + 2)
		p.name("alttpo4")
		p.send(t, g)
	}
	if rp.Name() != "alttpo4" {
		t.Errorf("expected parsing to continue past Super Metroid messages; name = %q", rp.Name())
	}

	// a different script protocol version is ignored without taking down the game:
	{
		p := newAlttpoPacket(remote, 13)
		b := p.Bytes()
		b[len(client.MakePacket(0x02).Bytes())+20+1+2] = 0x16
		if err := g.handleNetMessage(b); err == nil {
			t.Fatalf("expected a version mismatch error")
		}
	}
}

// TestAlttpoCompat_ItemsSyncFromAlttpo verifies an item an alttpo player picks up is synced to an o2 player.
func TestAlttpoCompat_ItemsSyncFromAlttpo(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g
	g.local.IndexF = 0
	g.players[0] = *g.local
	g.local = &g.players[0]

	gs.e.WRAM[0x10] = 0x07
	for f := 0; f < 8; f++ {
		gs.runFrame(t)
	}

	// alttpo player has the hookshot ($342):
	for f := 0; f < 8; f++ {
		p := newAlttpoPacket(1, uint8(f))
		p.location(0x07, 0x04, 0x010012, 0, 0)
		p.name("alttpo")
		p.send(t, g)
		p = newAlttpoPacket(1, uint8(f))
		p.sram(0x340, []byte{0, 0, 1})
		p.send(t, g)
		gs.runFrame(t)
	}

	if gs.e.WRAM[0xF342] != 1 {
		t.Errorf("expected hookshot to sync from alttpo player; got %02x", gs.e.WRAM[0xF342])
	}
}

// TestAlttpoCompat_RemoteWithoutDoorState verifies that syncing does not crash when a remote player never sent the
// WRAM values o2 syncs (alttpo does not send the current dungeon room state at $0400).
func TestAlttpoCompat_RemoteWithoutDoorState(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g
	g.local.IndexF = 0
	g.players[0] = *g.local
	g.local = &g.players[0]

	// in the same dungeon room as the alttpo player so the room state sync runs:
	gs.e.WRAM[0x10] = 0x07
	gs.e.WRAM[0x11] = 0x00
	gs.e.WRAM[0x1B] = 0x01 // underworld
	gs.e.WRAM[0xA0] = 0x12 // room $12
	gs.e.WRAM[0x0400] = 0x01

	for f := 0; f < 8; f++ {
		p := newAlttpoPacket(1, uint8(f))
		p.location(0x07, 0x04, 0x010012, 0, 0)
		p.name("alttpo")
		p.send(t, g)
		gs.runFrame(t)
	}
}
