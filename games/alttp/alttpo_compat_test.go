package alttp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"o2/client/protocol05"
	"testing"
	"time"
)

// alttpoParse is a model of alttpo's `GameState::deserialize` (GameState.as) used to verify that the packets o2 sends
// parse correctly in alttpo: game protocol v2 framing (frame, then `type, length, body` messages), and for the message
// types both share, alttpo's own reading of the body, which must use exactly the message's length. It returns the
// message types parsed.
func alttpoParse(r []byte) (types []uint8, err error) {
	types, _, err = alttpoParseFull(r)
	return
}

type alttpoSprite struct {
	b [5]uint8 // x, y, chr, attributes, ext
}

type alttpoParsed struct {
	sprites []alttpoSprite
	// chr bindings: chr -> fact << 16 | tile:
	chrs map[uint8]uint32
	// palettes sent, by palette:
	palettes map[int][]byte
}

func alttpoParseFull(pkt []byte) (types []uint8, parsed alttpoParsed, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%v", rec)
		}
	}()
	if len(pkt) < 1 {
		return nil, parsed, fmt.Errorf("empty packet")
	}
	for o := 1; o < len(pkt); {
		t := pkt[o]
		o++
		n, w, ok := readMsgLen(pkt[o:])
		if !ok || o+w+n > len(pkt) {
			return types, parsed, fmt.Errorf("message %#02x truncated", t)
		}
		o += w
		body := pkt[o : o+n]
		o += n
		types = append(types, t)
		used, known := alttpoParseBody(t, body, &parsed)
		if known && used != len(body) {
			return types, parsed, fmt.Errorf("message %#02x: alttpo reads %d of its %d bytes", t, used, len(body))
		}
	}
	return
}

// alttpoParseBody reads a message body the way alttpo does, returning how many bytes it read; known is false for
// message types without a model here (they are only checked for framing).
func alttpoParseBody(t uint8, r []byte, parsed *alttpoParsed) (c int, known bool) {
	need := func(n int) {
		if c+n > len(r) {
			panic(fmt.Errorf("message %#02x: read past end at %d+%d > %d", t, c, n, len(r)))
		}
	}
	u8 := func() uint8 { need(1); c++; return r[c-1] }
	u16 := func() uint16 { need(2); c += 2; return binary.LittleEndian.Uint16(r[c-2:]) }
	skip := func(n int) { need(n); c += n }

	known = true
	switch MessageType(t) {
	case MsgLocation:
		skip(3 + 3 + 2*9 + 1)
	case MsgSfx:
		skip(2)
	case MsgSprites:
		n := int(u8())
		for i := 0; i < n; i++ {
			var spr alttpoSprite
			need(5)
			copy(spr.b[:], r[c:c+5])
			skip(5)
			parsed.sprites = append(parsed.sprites, spr)
		}
	case MsgSpriteChrs:
		if parsed.chrs == nil {
			parsed.chrs = map[uint8]uint32{}
		}
		n := int(u8())
		for i := 0; i < n; i++ {
			chr, cnt, fact := u8(), u8(), u8()
			tile := u16()
			for k := uint8(0); k < cnt; k++ {
				parsed.chrs[chr+k] = uint32(fact)<<16 | uint32(tile+uint16(k))
			}
		}
	case MsgSpritePalettes:
		if parsed.palettes == nil {
			parsed.palettes = map[int][]byte{}
		}
		mask := u8()
		for k := 0; k < 8; k++ {
			if mask&(1<<k) != 0 {
				need(32)
				parsed.palettes[k] = r[c : c+32]
				skip(32)
			}
		}
	case MsgWRAM:
		n := int(u8())
		u16()
		skip(n * 6)
	case MsgSRAM:
		u8() // in_sm
		u16()
		skip(int(u16()))
	case MsgMap16:
		skip(3) // location u24
		u8()    // count
		u8()    // start
		skip(int(u8()) * 4)
	case MsgStarSwitch:
		skip(2 + 4 + 2)
	case MsgOwEvents:
		skip(3 + 1 + 1)
	case MsgRoomState:
		skip(2 + 1 + 4 + 1)
	case MsgRoomDoors:
		skip(2 + 1 + 1 + 4)
	case MsgRoomKills:
		skip(2 + 1)
	case MsgEnemiesKilled:
		if u8() == 0 {
			skip(2 + 2) // room, mask
		} else {
			skip(3)             // location u24
			skip(int(u8()) * 2) // blks
		}
	case MsgManip:
		u16() // room
		n := int(u8())
		for i := 0; i < n; i++ {
			skip(6)
			if u8()&0x02 != 0 {
				skip(8)
			}
		}
	case MsgAncillae:
		n := int(u8())
		for i := 0; i < n; i++ {
			if u8()&0x7F < 5 {
				skip(0x20)
			} else {
				skip(0x16)
			}
		}
	case MsgTorches:
		skip(int(u8()) * 2)
	case MsgSmallKeys:
		skip(4)
		skip(int(u8()) * 7)
	case MsgHearts:
		skip(4 + 2 + 1)
	case MsgSmallKeysChanges:
		u8() // base
		skip(int(u8()) * 8)
	case MsgLocationChanges:
		u8() // base
		m := u8()
		for i := 0; i < 4; i++ {
			if m&(1<<i) != 0 {
				skip(2)
			}
		}
	default:
		known = false
	}
	return
}

// alttpoPacket builds a broadcast as relayed by the server from an alttpo client: the game packet (frame, then framed
// messages). Start each message with m.
type alttpoPacket struct {
	bytes.Buffer

	index uint16
	// starts are where the messages start:
	starts []int
	// playerName is the name its player-info carries:
	playerName string
}

func newAlttpoPacket(index uint16, frame uint8) *alttpoPacket {
	p := &alttpoPacket{index: index, playerName: "alttpo"}
	p.WriteByte(frame)
	return p
}

// m starts a message of type t.
func (p *alttpoPacket) m(t MessageType) *alttpoPacket {
	p.starts = append(p.starts, p.Len())
	p.WriteByte(byte(t))
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

// data returns the game packet with each message's length inserted.
func (p *alttpoPacket) data() []byte {
	b := p.Bytes()
	out := []byte{b[0]}
	for i, start := range p.starts {
		end := len(b)
		if i+1 < len(p.starts) {
			end = p.starts[i+1]
		}
		out = append(out, frameMessage(b[start:end])...)
	}
	return out
}

// packet wraps the game packet in a protocol 05 relay-all as the server relays it.
func (p *alttpoPacket) packet(t *testing.T) []byte {
	t.Helper()
	return protocol05.MakeRelay(p.index, time.Now(), false, 0, p.data())
}

// name sets the name of the player, which o2 learns from its player-info.
func (p *alttpoPacket) name(name string) *alttpoPacket {
	p.playerName = name
	return p
}

// alttpoPlayerInfo tells g a remote player's name and info, as the server does when it joins.
func alttpoPlayerInfo(t *testing.T, g *Game, index uint16, name string) {
	t.Helper()
	pkt := protocol05.MakePlayerInfo([]protocol05.PlayerInfoEntry{{PlayerIndex: index, Name: name, Info: []byte{SerializationVersion, 0}}})
	if err := g.handleNetMessage(pkt); err != nil {
		t.Fatal(err)
	}
}

func (p *alttpoPacket) send(t *testing.T, g *Game) {
	t.Helper()
	if pi := &g.net.info[p.index]; !pi.known || pi.name != p.playerName {
		alttpoPlayerInfo(t, g, p.index, p.playerName)
	}
	m, err := protocol05.Decode(p.packet(t))
	if err != nil {
		t.Fatal(err)
	}
	rp := &g.players[p.index]
	rp.IndexF = int(p.index)
	if err := g.Deserialize(bytes.NewReader(m.Data), rp); err != nil {
		t.Fatalf("o2 failed to handle alttpo packet: %v", err)
	}
	g.SetTTL(rp, 255)
}

// The writers below follow alttpo's LocalGameState.as serialize_* functions.

func (p *alttpoPacket) location(module, dungeon uint8, location uint32, x, y uint16) *alttpoPacket {
	p.m(MsgLocation).u8(module, 0, 0)
	p.u8(uint8(location), uint8(location>>8), uint8(location>>16))
	p.u16(x).u16(y)
	p.u16(uint16(dungeon)).u16(0) // dungeon, entrance
	p.u16(0).u16(0)               // last overworld x, y
	p.u16(0).u16(0)               // xoffs, yoffs
	p.u16(0x1234)                 // player color
	p.u8(0)                       // in_sm
	return p
}

func (p *alttpoPacket) sram(start uint16, data []byte) *alttpoPacket {
	p.m(MsgSRAM).u8(0)
	p.u16(start).u16(uint16(len(data)))
	p.Write(data)
	return p
}

// smallKeyDeltas follows alttpo's SmallKeysLocal::serialize (SmallKeys.as).
func (p *alttpoPacket) smallKeyDeltas(session uint32, net [16]int16, value [16]uint8, ts [16]uint32) *alttpoPacket {
	p.m(MsgSmallKeys).u32(session).u8(16)
	for k := 0; k < 16; k++ {
		p.u16(uint16(net[k])).u8(value[k]).u32(ts[k])
	}
	return p
}

// heartDeltas follows alttpo's HeartsLocal::serialize (Hearts.as).
func (p *alttpoPacket) heartDeltas(session uint32, net int16, value uint8) *alttpoPacket {
	return p.m(MsgHearts).u32(session).u16(uint16(net)).u8(value)
}

// TestAlttpoCompat_O2PacketsParseInAlttpo captures every packet o2 sends over several hundred frames and verifies that
// they all parse under alttpo's rules: framing, and the body of every message type both share.
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
	allCount, sectorCount := 0, 0
	for f := 0; f < 256; f++ {
		if f == 100 {
			gs.g.initiateReset(time.Now())
		}
		gs.g.tickReset(time.Now())
		gs.runFrame(t)
		if f%30 == 0 {
			gs.g.sendRunTimingBeat()
		}
		for len(gs.c.Wr) > 0 {
			q, err := protocol05.DecodeRequest(<-gs.c.Wr)
			if err != nil {
				t.Fatal(err)
			}
			if q.Kind != protocol05.BroadcastAll && q.Kind != protocol05.BroadcastSector {
				continue
			}
			if q.Kind == protocol05.BroadcastSector {
				if want := broadcastSector(gs.g.local.Location); q.Sector != want {
					t.Fatalf("expected sector packet for own sector %#x; got %#x", want, q.Sector)
				}
				sectorCount++
			} else {
				allCount++
			}
			types, err := alttpoParse(q.Data)
			if err != nil {
				t.Fatalf("alttpo would fail to parse o2 packet %x: %v", q.Data, err)
			}
			for _, ty := range types {
				seen[ty]++
			}
		}
	}

	if allCount == 0 || sectorCount == 0 {
		t.Fatalf("expected packets to all and to the sector; got %d and %d", allCount, sectorCount)
	}
	for _, ty := range []MessageType{MsgLocation, MsgWRAM, MsgSRAM, MsgRunTiming, MsgReset} {
		if seen[uint8(ty)] == 0 {
			t.Errorf("expected o2 to send message type %#02x", ty)
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

	// main packet: location, sfx, pvp, ancillae, crystal switch, small keys
	{
		p := newAlttpoPacket(remote, 10)
		p.location(0x07, 0x04, 0x010012, 0x1234, 0x5678)
		p.m(MsgSfx).u8(0x11, 0x22)
		// pvp: active hitbox, 2 attacks:
		p.m(MsgPvP).u8(1).u16(1).u16(2).u8(8, 8, 3, 4).u8(1, 2).u8(2)
		p.u16(0).u8(1, 2, 3, 4, 5, 6, 7)
		p.u16(1).u8(1, 2, 3, 4, 5, 6, 7)
		// ancillae: one front-slot (0x20 facts) and one back-slot (0x16 facts):
		p.m(MsgAncillae).u8(2, 0x81).zeros(0x20).u8(0x06).zeros(0x16)
		// crystal switch:
		p.m(MsgWRAM).u8(0x01).u16(0xC172).u32(5).u16(1)
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

	// sprites packet: Link's head and body bound to the Link graphics fact, a palette and three sprites:
	{
		p := newAlttpoPacket(remote, 10)
		p.m(MsgSpriteChrs).u8(2)
		p.u8(0x00, 2, 0).u16(0x0020)
		p.u8(0x10, 2, 0).u16(0x0030)
		p.m(MsgSpritePalettes).u8(0x80).zeros(32)
		p.m(MsgSprites).u8(3)
		p.u8(0x10, 0x20, 0x00, 0x0E, 0x02) // head, 16x16
		p.u8(0x10, 0x28, 0x6C, 0x08, 0x02) // shadow, 16x16
		p.u8(0x00, 0xF0, 0x00, 0x00, 0x02) // disabled
		p.location(0x07, 0x04, 0x010012, 0x1235, 0x5678)
		p.send(t, g)
	}
	if rp.X != 0x1235 {
		t.Errorf("expected parsing to continue past sprite messages; x = %04x", rp.X)
	}
	if l := rp.Sprites.list; len(l) != 3 || l[0].absidx != [4]uint16{0x20, 0x21, 0x30, 0x31} || l[1].absidx[0] != noUniqTile {
		t.Errorf("unexpected remote sprites %+v", l)
	}

	// enemy sync packet:
	{
		p := newAlttpoPacket(remote, 10)
		// enemy data for 2 sprites (dungeon):
		p.m(MsgEnemyData).u16(0x0003).u8(1).zeros(2 * 55)
		// enemy segment data: moldorm in slot 0, swamola in slot 7 (no data), default in slot 1:
		p.m(MsgEnemySegmentData).u16(0x0083).u8(0x09).zeros(4 * 0x80).u8(0x30).zeros(7 * 0x10).u8(0xCF)
		// overlords 0 and 2:
		p.m(MsgOverlordData).u8(0x05).zeros(2 * 11)
		// torches:
		p.m(MsgTorches).u8(2, 1, 5, 2, 6)
		// manipulables: a pot that was lifted and a push block:
		p.m(MsgManip).u16(0x0012).u8(2)
		p.u16(0x1111).u16(0x0030).u16(0x0410).u8(0x03).u16(0x0D0E).u16(0x0D1E).u16(0x4D0E).u16(0x4D1E)
		p.u16(0x0000).u16(0x0008).u16(0x0820).u8(0x00)
		// map16 changes (alttpo sends these to its sector in the overworld; the parser does not care):
		// location u24 ($000018: Kakariko Village, light world):
		p.m(MsgMap16).u8(0x18).u16(0x0000).u8(2, 0, 2).u16(0x0410).u16(0x0DC7).u16(0x0412).u16(0x0DC7)
		p.location(0x07, 0x04, 0x010012, 0x1236, 0x5678)
		p.send(t, g)
	}
	if rp.X != 0x1236 {
		t.Errorf("expected parsing to continue past enemy messages; x = %04x", rp.X)
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

	// Super Metroid (SMZ3) packet; o2 skips these by their length:
	{
		p := newAlttpoPacket(remote, 12)
		p.m(MsgSMLocation).zeros(15)
		p.m(MsgSMEvents).zeros(0x54 + 2)
		p.m(MsgSMSRAM).zeros(0x40)
		p.m(MsgSMEnemies).u8(34)
		p.m(MsgSMEnemies).u8(1, 2, 0)
		p.m(MsgSMEnemies).u8(1, 3, 1).zeros(64 + 2)
		p.location(0x07, 0x04, 0x010012, 0x1237, 0x5678)
		p.send(t, g)
	}
	if rp.X != 0x1237 {
		t.Errorf("expected parsing to continue past Super Metroid messages; x = %04x", rp.X)
	}

	// a player with a different game protocol version is ignored:
	{
		pkt := protocol05.MakePlayerInfo([]protocol05.PlayerInfoEntry{{PlayerIndex: 5, Name: "old", Info: []byte{0x01, 0}}})
		if err := g.handleNetMessage(pkt); err != nil {
			t.Fatal(err)
		}
		p := newAlttpoPacket(5, 13)
		p.location(0x07, 0x04, 0x010012, 0x1238, 0x5678)
		if err := g.handleNetMessage(p.packet(t)); err != nil {
			t.Fatal(err)
		}
		if g.players[5].X == 0x1238 {
			t.Errorf("expected the incompatible player's messages to be dropped")
		}

		// listed as incompatible, after the active players:
		vms := g.appendIncompatiblePlayers(nil, g.ActivePlayers())
		if len(vms) != 1 || vms[0].Index != 5 || vms[0].Name != "old" || !vms[0].Incompatible {
			t.Errorf("expected player 5 listed as incompatible; got %+v", vms)
		}
		// and no longer once it left:
		if err := g.handleNetMessage(protocol05.MakePlayerLeft(5, 0)); err != nil {
			t.Fatal(err)
		}
		if vms := g.appendIncompatiblePlayers(nil, g.ActivePlayers()); len(vms) != 0 {
			t.Errorf("expected no incompatible players after player-left; got %+v", vms)
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
		want     uint32
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
// client) is skipped by its length, and the messages after it are read.
func TestAlttpoCompat_UnknownMessageTypeKeepsPacket(t *testing.T) {
	gs, err := createTestGameSync("VT test", "o2player", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	g := gs.g

	for frame := uint8(1); frame <= 3; frame++ {
		p := newAlttpoPacket(3, frame)
		p.m(0xFE).u8(1, 2, 3)
		p.location(0x07, 0x04, 0x010012, 0x1239, 0)
		p.send(t, g)
	}
	if x := g.players[3].X; x != 0x1239 {
		t.Errorf("expected the message after the unknown message type to be read; x = %04x", x)
	}
	if n := len(g.unknownMsgLogged); n != 1 {
		t.Errorf("expected the unknown message type to be logged once; got %d entries", n)
	}
}
