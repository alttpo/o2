package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"o2/client"
	"o2/client/protocol04"
	"testing"
	"time"
)

// alttpoScriptProtocol is `script_protocol` from alttpo's GameState.as.
const alttpoScriptProtocol = 0x01

// alttpoParse is a model of alttpo's `GameState::deserialize` (GameState.as) used to verify that the packets o2 sends
// parse correctly in alttpo. alttpo stops parsing a packet at the first unknown message type, so any unknown type is
// treated as a failure here since the messages following it would be lost. It returns the message types parsed.
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
		case 0x17: // map16 changes
			skip(3) // location u24
			u8()  // count
			u8()  // start
			skip(int(u8()) * 4)
		case 0x19: // star switch
			skip(2 + 4 + 2)
		case 0x1C: // overworld events
			skip(3 + 1 + 1)
		case 0x1A: // room state
			skip(2 + 1 + 4 + 1)
		case 0x1B: // room key doors and switches
			skip(2 + 1 + 1 + 4)
		case 0x1E: // room kill triggers
			skip(2 + 1)
		case 0x1F: // enemies killed
			if u8() == 0 {
				skip(2 + 2) // room, mask
			} else {
				skip(3)              // location u24
				skip(int(u8()) * 2) // blks
			}
		case 0x18: // manipulables
			u16() // room
			n := int(u8())
			for i := 0; i < n; i++ {
				skip(6)
				if u8()&0x02 != 0 {
					skip(8)
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
		case 0x82: // small keys
			skip(4)
			skip(int(u8()) * 7)
		case 0x83: // hearts
			skip(4 + 2 + 1)
		case 0x87: // features
			skip(1)
		case 0x88: // small key changes
			u8() // base
			skip(int(u8()) * 8)
		case 0x89: // location changes
			u8() // base
			m := u8()
			for i := 0; i < 4; i++ {
				if m&(1<<i) != 0 {
					skip(2)
				}
			}
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
			return types, parsed, fmt.Errorf("message type %#02x not expected from o2 (alttpo would drop the rest)", t)
		}
	}
	return
}

// alttpoPacket builds a protocol 04 broadcast as relayed by the server from an alttpo client. The buffer holds the
// broadcast data (game packet header and messages).
type alttpoPacket struct {
	bytes.Buffer

	index uint16
}

func newAlttpoPacket(index uint16, frame uint8) *alttpoPacket {
	p := &alttpoPacket{index: index}
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

// smallKeyDeltas follows alttpo's SmallKeysLocal::serialize (SmallKeys.as).
func (p *alttpoPacket) smallKeyDeltas(session uint32, net [16]int16, value [16]uint8, ts [16]uint32) *alttpoPacket {
	p.u8(0x82).u32(session).u8(16)
	for k := 0; k < 16; k++ {
		p.u16(uint16(net[k])).u8(value[k]).u32(ts[k])
	}
	return p
}

// heartDeltas follows alttpo's HeartsLocal::serialize (Hearts.as).
func (p *alttpoPacket) heartDeltas(session uint32, net int16, value uint8) *alttpoPacket {
	return p.u8(0x83).u32(session).u16(uint16(net)).u8(value)
}

// packet wraps the broadcast data in a protocol 04 packet as the server relays it.
func (p *alttpoPacket) packet(t *testing.T) []byte {
	t.Helper()
	b, err := protocol04.MakePacket(&protocol04.GroupMessage{
		Group:          "test",
		PlayerTime:     1,
		ServerTime:     time.Now().UnixNano(),
		PlayerIndex:    uint32(p.index),
		PlayerInSector: 0x010012,
		Kind:           protocol04.BroadcastAll,
		Data:           p.Bytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (p *alttpoPacket) send(t *testing.T, g *Game) {
	t.Helper()
	if err := g.handleNetMessage(p.packet(t)); err != nil {
		t.Fatalf("o2 failed to handle alttpo packet: %v", err)
	}
}

// TestAlttpoCompat_O2PacketsParseInAlttpo captures every packet o2 sends over several hundred frames and verifies that
// all packets containing alttpo message types parse completely under alttpo's rules, and that o2-only message types
// only travel in their own packets (which alttpo stops parsing at their first message).
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
	alttpoCount, o2Count, sectorCount := 0, 0, 0
	for f := 0; f < 256; f++ {
		if f == 100 {
			// o2-only reset messages must not reach alttpo:
			gs.g.initiateReset(time.Now())
		}
		gs.g.tickReset(time.Now())
		gs.runFrame(t)
		if f%30 == 0 {
			gs.g.sendPlayerName()
		}
		for len(gs.c.Wr) > 0 {
			b := <-gs.c.Wr
			var proto uint8
			if _, err := client.ParseHeader(b, &proto); err != nil {
				t.Fatal(err)
			}
			if proto != protocol04.Protocol {
				t.Fatalf("unexpected server protocol %d", proto)
			}
			var gm protocol04.GroupMessage
			if err := protocol04.Unmarshal(b[3:], &gm); err != nil {
				t.Fatal(err)
			}
			if gm.Group != "test" {
				t.Fatalf("unexpected group %q", gm.Group)
			}
			if gm.Kind != protocol04.BroadcastAll && gm.Kind != protocol04.BroadcastSector {
				continue
			}
			if gm.Kind == protocol04.BroadcastSector {
				if gm.TargetSector != gm.PlayerInSector {
					t.Fatalf("expected sector packet for own sector %#x; got %#x", gm.PlayerInSector, gm.TargetSector)
				}
				sectorCount++
			}
			payload := gm.Data
			if len(payload) > 3 && payload[3] >= 0x80 {
				// o2-only packet; alttpo stops parsing at its first message:
				o2Count++
				continue
			}
			alttpoCount++
			types, err := alttpoParse(payload)
			if err != nil {
				t.Fatalf("alttpo would fail to parse o2 packet %x: %v", payload, err)
			}
			for _, ty := range types {
				seen[ty]++
			}
		}
	}

	if alttpoCount == 0 || o2Count == 0 || sectorCount == 0 {
		t.Fatalf("expected alttpo, o2-only and sector packets; got %d, %d and %d", alttpoCount, o2Count, sectorCount)
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
		var net [16]int16
		var value [16]uint8
		var ts [16]uint32
		value[2] = 3
		p.smallKeyDeltas(0x01020304, net, value, ts)
		p.send(t, g)
	}

	rp := &g.players[remote]
	if rp.Name() != "alttpo" {
		t.Errorf("expected remote name %q; got %q", "alttpo", rp.Name())
	}
	if rp.Module != 0x07 || rp.Location != 0x010012 || rp.X != 0x1234 || rp.Y != 0x5678 || rp.Dungeon != 0x04 {
		t.Errorf("unexpected remote location: module=%02x location=%06x x=%04x y=%04x dungeon=%02x", rp.Module, rp.Location, rp.X, rp.Y, rp.Dungeon)
	}
	if rk := rp.SmallKeys; !rk.valid || rk.session != 0x01020304 || rk.value[2] != 3 {
		t.Errorf("expected remote small keys session 01020304 with Eastern Palace = 3; got %+v", rk)
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
		// manipulables: a pot that was lifted and a push block:
		p.u8(0x18).u16(0x0012).u8(2)
		p.u16(0x1111).u16(0x0030).u16(0x0410).u8(0x03).u16(0x0D0E).u16(0x0D1E).u16(0x4D0E).u16(0x4D1E)
		p.u16(0x0000).u16(0x0008).u16(0x0820).u8(0x00)
		// map16 changes (alttpo sends these to its sector in the overworld; the parser does not care):
		// location u24 ($000018: Kakariko Village, light world):
		p.u8(0x17, 0x18).u16(0x0000).u8(2, 0, 2).u16(0x0410).u16(0x0DC7).u16(0x0412).u16(0x0DC7)
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
		p.Bytes()[0] = 0x16
		if err := g.handleNetMessage(p.packet(t)); err == nil {
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

// TestAlttpoCompat_BroadcastSector verifies o2 reports the same server sector as alttpo's `broadcast_sector()`.
func TestAlttpoCompat_BroadcastSector(t *testing.T) {
	for _, tc := range []struct {
		location uint32
		want     uint64
	}{
		{0x000012, 0x000012}, // light world overworld
		{0x020052, 0x020052}, // dark world overworld
		{0x010012, 0x010012}, // underworld
		{0x030012, 0x010012}, // underworld entered from the dark world
	} {
		if got := broadcastSector(tc.location); got != tc.want {
			t.Errorf("broadcastSector(%06x) = %06x; want %06x", tc.location, got, tc.want)
		}
	}
}

// TestAlttpoCompat_SmallKeyDeltasFromAlttpo verifies o2 applies small key deltas sent by alttpo, which sends message
// 0x82 in its main packet alongside other messages.
func TestAlttpoCompat_SmallKeyDeltasFromAlttpo(t *testing.T) {
	h := newSKScenario(t, 1)
	g := h.gs[0].g
	h.inDungeon(0, 1)
	h.frames(8)

	var (
		net   [16]int16
		value [16]uint8
		ts    [16]uint32
	)
	for k := range value {
		value[k] = 0xFF
	}
	value[2] = 1

	frame := uint8(0)
	send := func() {
		frame++
		p := newAlttpoPacket(3, frame)
		p.location(0x07, 0x04, 0x010012, 0, 0)
		p.smallKeyDeltas(0x12345678, net, value, ts)
		p.name("alttpo")
		p.send(t, g)
	}

	// first message only establishes a baseline:
	send()
	h.frames(8)
	if name := g.players[3].Name(); name != "alttpo" {
		t.Fatalf("expected parsing to continue past small keys; name = %q", name)
	}
	h.expectKeys("baseline", 1)

	// alttpo player picks up a key:
	net[2], value[2], ts[2] = 1, 2, 1000
	send()
	h.frames(8)
	h.expectKeys("alttpo pickup", 2)

	// alttpo player spends two keys:
	net[2], value[2], ts[2] = -1, 0, 2000
	send()
	h.frames(8)
	h.expectKeys("alttpo spend", 0)

	// repeated messages without changes don't apply anything:
	send()
	h.frames(8)
	h.expectKeys("no change", 0)
}

// TestAlttpoCompat_HeartDeltasFromAlttpo verifies o2 applies heart deltas sent by alttpo, which sends message 0x83 in
// its main packet after the small keys.
func TestAlttpoCompat_HeartDeltasFromAlttpo(t *testing.T) {
	h := newSKScenario(t, 1)
	g := h.gs[0].g
	h.onOverworldWithHearts(0, hpAreaA, 3<<3, 0)
	h.frames(8)

	var (
		net   [16]int16
		value [16]uint8
		ts    [16]uint32
	)
	for k := range value {
		value[k] = 0xFF
	}

	frame := uint8(0)
	send := func(heartsNet int16, heartsValue uint8) {
		frame++
		p := newAlttpoPacket(3, frame)
		p.location(0x09, 0xFF, uint32(hpAreaB), 0, 0)
		p.smallKeyDeltas(0x12345678, net, value, ts)
		p.heartDeltas(0x9ABCDEF0, heartsNet, heartsValue)
		p.name("alttpo")
		p.send(t, g)
	}

	// first message only establishes a baseline:
	send(0, 12)
	h.frames(8)
	if name := g.players[3].Name(); name != "alttpo" {
		t.Fatalf("expected parsing to continue past hearts; name = %q", name)
	}
	h.expectHearts("baseline", 3<<3, 0)

	// alttpo player collects a heart piece, then a heart container:
	send(1, 13)
	h.frames(8)
	h.expectHearts("alttpo heart piece", 3<<3, 1)
	send(5, 17)
	h.frames(8)
	h.expectHearts("alttpo heart container", 4<<3, 1)

	// repeated messages without changes don't apply anything:
	send(5, 17)
	h.frames(8)
	h.expectHearts("no change", 4<<3, 1)
}

// TestAlttpoCompat_UnknownMessageTypeKeepsPacket verifies that a message type o2 does not know (e.g. one added by a newer
// client at the end of its packet) does not fail the packet: the messages before it are kept.
func TestAlttpoCompat_UnknownMessageTypeKeepsPacket(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g

	for frame := uint8(1); frame <= 3; frame++ {
		p := newAlttpoPacket(3, frame)
		p.name("newer")
		p.u8(0xFE, 1, 2, 3)
		p.send(t, g)
	}
	if name := g.players[3].Name(); name != "newer" {
		t.Errorf("expected the name before the unknown message type to be kept; got %q", name)
	}
	if n := len(g.unknownMsgLogged); n != 1 {
		t.Errorf("expected the unknown message type to be logged once; got %d entries", n)
	}
}
