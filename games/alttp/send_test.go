package alttp

import (
	"bytes"
	"o2/client/protocol05"
	"testing"
)

// sentPacket is a game packet o2 sent, as the server got it.
type sentPacket struct {
	kind  protocol05.Kind
	types []uint8
	data  []byte
}

// sentPackets runs n frames and returns the game packets sent during each.
func (h *spriteTestRig) sentPackets(n int) (frames [][]sentPacket) {
	h.t.Helper()
	for f := 0; f < n; f++ {
		h.gs.runFrame(h.t)
		var pkts []sentPacket
		for len(h.gs.c.Wr) > 0 {
			q, err := protocol05.DecodeRequest(<-h.gs.c.Wr)
			if err != nil {
				h.t.Fatal(err)
			}
			if q.Kind != protocol05.BroadcastAll && q.Kind != protocol05.BroadcastSector {
				continue
			}
			types, _, err := alttpoParseFull(q.Data)
			if err != nil {
				h.t.Fatalf("alttpo would fail to parse o2 packet % x: %v", q.Data, err)
			}
			pkts = append(pkts, sentPacket{kind: q.Kind, types: types, data: q.Data})
		}
		frames = append(frames, pkts)
	}
	return
}

// gameMessages splits a game packet into its messages, each its type byte followed by its body.
func gameMessages(pkt []byte) (msgs [][]byte) {
	for o := 1; o < len(pkt); {
		t := pkt[o]
		o++
		n, w, ok := readMsgLen(pkt[o:])
		if !ok || o+w+n > len(pkt) {
			return
		}
		o += w
		msgs = append(msgs, append([]byte{t}, pkt[o:o+n]...))
		o += n
	}
	return
}

func countType(pkts []sentPacket, kind protocol05.Kind, t MessageType) (n int) {
	for _, p := range pkts {
		if p.kind == kind && bytes.IndexByte(p.types, byte(t)) >= 0 {
			n++
		}
	}
	return
}

// Link's sprites go to the sector, not to all players: players only draw the players in their own location.
// Unchanged sprites are sent again every spritesRefresh frames, so that receivers don't take them as stale.
func TestSend_SpritesToSectorOnChange(t *testing.T) {
	h := newSpriteTestRig(t)
	frames := h.sentPackets(2 * spritesRefresh)

	sent := 0
	for f, pkts := range frames {
		if n := countType(pkts, protocol05.BroadcastAll, MsgSprites); n != 0 {
			t.Errorf("frame %d: sprites sent to all players", f)
		}
		sent += countType(pkts, protocol05.BroadcastSector, MsgSprites)
	}
	// the first frame, then a refresh every spritesRefresh frames:
	if sent < 2 || sent > 3 {
		t.Errorf("expected unchanged sprites sent 2 or 3 times in %d frames; got %d", 2*spritesRefresh, sent)
	}

	// Link moves; sent at once:
	h.gs.e.WRAM[0x0800+0x64*4] ^= 0x10
	frames = h.sentPackets(2)
	if countType(frames[0], protocol05.BroadcastSector, MsgSprites)+countType(frames[1], protocol05.BroadcastSector, MsgSprites) == 0 {
		t.Errorf("expected changed sprites to be sent")
	}
}

// Palettes are only sent when they change, or every paletteRefresh frames.
func TestSend_SpritePalettesOnChange(t *testing.T) {
	h := newSpriteTestRig(t)
	g := h.gs.g
	h.sentPackets(4)

	// Link moves every frame; his palette stays:
	withPalette := 0
	for f := 0; f < 8; f++ {
		h.gs.e.WRAM[0x0800+0x64*4] ^= 0x10
		for _, pkts := range h.sentPackets(1) {
			for _, p := range pkts {
				if p.kind != protocol05.BroadcastSector {
					continue
				}
				_, parsed, _ := alttpoParseFull(p.data)
				withPalette += len(parsed.palettes)
			}
		}
	}
	if withPalette > 1 {
		t.Errorf("expected the unchanged palette sent at most once in 8 frames; got %d", withPalette)
	}

	// it changes:
	h.gs.e.WRAM[0xC6E0] ^= 0xFF
	var got []byte
	for _, pkts := range h.sentPackets(3) {
		for _, p := range pkts {
			if p.kind == protocol05.BroadcastSector {
				_, parsed, _ := alttpoParseFull(p.data)
				if pal, ok := parsed.palettes[7]; ok {
					got = pal
				}
			}
		}
	}
	if !bytes.Equal(got, g.linkSprites.palettes[7][:]) || got[0] != h.gs.e.WRAM[0xC6E0] {
		t.Errorf("expected the changed palette to be sent; got % x", got)
	}
}

// The messages to all players go in one packet a frame, and the small keys and hearts only when they change or are
// due for a refresh.
func TestSend_AllCoalescedOnChange(t *testing.T) {
	h := newSpriteTestRig(t)
	frames := h.sentPackets(2 * smallKeysRefresh)

	keys := 0
	for f, pkts := range frames {
		state := 0
		for _, p := range pkts {
			if p.kind != protocol05.BroadcastAll {
				continue
			}
			if bytes.IndexByte(p.types, byte(MsgLocation)) >= 0 || bytes.IndexByte(p.types, byte(MsgSmallKeys)) >= 0 {
				state++
			}
		}
		if state > 1 {
			t.Errorf("frame %d: expected the location and small keys in one packet; got %d", f, state)
		}
		keys += countType(pkts, protocol05.BroadcastAll, MsgSmallKeys) + countType(pkts, protocol05.BroadcastAll, MsgSmallKeysChanges)
	}
	if keys < 2 || keys > 3 {
		t.Errorf("expected unchanged small keys sent 2 or 3 times in %d frames; got %d", 2*smallKeysRefresh, keys)
	}
}

// The underworld SRAM is sent whole the first time, then only the rooms that changed.
func TestSend_UnderworldSRAMChanges(t *testing.T) {
	h := newSpriteTestRig(t)
	g := h.gs.g
	sram := func(frames [][]sentPacket) (ranges [][2]int) {
		for _, pkts := range frames {
			for _, p := range pkts {
				if p.kind != protocol05.BroadcastAll {
					continue
				}
				for _, m := range gameMessages(p.data) {
					if MessageType(m[0]) != MsgSRAM {
						continue
					}
					body := m[1:]
					start := int(body[1]) | int(body[2])<<8
					count := int(body[3]) | int(body[4])<<8
					if start < 0x250 {
						ranges = append(ranges, [2]int{start, start + count})
					}
				}
			}
		}
		return
	}

	first := sram(h.sentPackets(64))
	if len(first) == 0 || first[0] != [2]int{0, 0x250} {
		t.Fatalf("expected the underworld SRAM whole first; got %x", first)
	}
	for _, r := range first[1:] {
		t.Errorf("expected nothing more while unchanged; got %x", r)
	}

	// a room's flags change:
	h.gs.e.WRAM[0xF000+0x0A5] |= 0x80
	h.gs.e.WRAM[0xF000+0x0A8] |= 0x01
	got := sram(h.sentPackets(40))
	if len(got) != 1 || got[0] != [2]int{0xA4, 0xAA} {
		t.Errorf("expected only rooms $52..$54 sent; got %x", got)
	}
	if g.local.SRAM.data[0x0A5]&0x80 == 0 {
		t.Errorf("expected the change read")
	}
}

// The counters split each packet's bytes by the messages in it.
func TestSend_BandwidthCounters(t *testing.T) {
	h := newSpriteTestRig(t)
	g := h.gs.g
	h.sentPackets(8)

	s := &g.bandwidth
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.packets == 0 || s.wire == 0 {
		t.Fatalf("expected packets counted")
	}
	var wire float64
	for _, c := range s.kinds {
		wire += c.wire
	}
	if d := wire - float64(s.wire); d > 0.5 || d < -0.5 {
		t.Errorf("expected the rows to add up to the total %d; got %f", s.wire, wire)
	}
	for _, name := range []string{msgTypeName(MsgLocation), msgTypeName(MsgSprites), msgTypeName(MsgSmallKeys)} {
		if c := s.kinds[name]; c == nil || c.msgs == 0 || c.payload == 0 {
			t.Errorf("expected %q counted; got %+v", name, c)
		}
	}
}

// sendPackets runs on every read of the SNES, which can be more than once a game frame; it sends once per frame.
func TestSend_OncePerGameFrame(t *testing.T) {
	h := newSpriteTestRig(t)
	h.sentPackets(4)
	h.gs.g.sendPackets()
	if n := len(h.gs.c.Wr); n != 0 {
		t.Errorf("expected nothing sent again in the same game frame; got %d packets", n)
	}
}

// Between clients that read them, the location and small keys go as changes to the last full message.
func TestSend_CompactBetweenO2Players(t *testing.T) {
	setupTestLogger(t)
	var f []gameSyncTestFrame
	dungeon := func(t testing.TB, gs [2]gameSync) {
		for _, g := range gs {
			g.e.WRAM[0x10] = 0x07
			g.e.WRAM[0x1B] = 1
		}
	}
	for i := 0; i < 80; i++ {
		f = append(f, gameSyncTestFrame{preFrame: dungeon})
	}
	f[70].preFrame = func(t testing.TB, gs [2]gameSync) {
		dungeon(t, gs)
		gs[0].e.WRAM[0x22] += 0x10
		gs[0].g.smallKeys.net[3] = 1
	}
	f[79].postFrame = func(t testing.TB, gs [2]gameSync) {
		s := &gs[0].g.bandwidth
		s.mu.Lock()
		for _, mt := range []MessageType{MsgLocationChanges, MsgSmallKeysChanges} {
			if s.kinds[msgTypeName(mt)] == nil {
				t.Errorf("expected %s sent", msgTypeName(mt))
			}
		}
		s.mu.Unlock()

		remote := gs[1].g.RemotePlayers()
		if len(remote) != 1 {
			t.Fatalf("expected one remote player; got %d", len(remote))
		}
		if p := remote[0]; p.X != gs[0].g.local.X {
			t.Errorf("expected the X change received: %#04x != %#04x", p.X, gs[0].g.local.X)
		}
		if p := remote[0]; p.SmallKeys.applied[3] != 1 {
			t.Errorf("expected the small key change received; got %d", p.SmallKeys.applied[3])
		}
	}
	tc := newGameSyncTestCase("VT test", f)
	tc.runGameSyncTest(t)
}
