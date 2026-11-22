package alttp

import (
	"bytes"
	"o2/client"
	"o2/client/protocol04"
	"testing"
)

// sentPacket is a game packet o2 sent, as the server got it.
type sentPacket struct {
	kind  protocol04.Kind
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
			if gm.Kind != protocol04.BroadcastAll && gm.Kind != protocol04.BroadcastSector {
				continue
			}
			sp := sentPacket{kind: gm.Kind, data: gm.Data}
			if len(gm.Data) > 3 && gm.Data[3] < 0x80 {
				// what alttpo parses; it stops at the first type it doesn't know:
				types, _, err := alttpoParseFull(gm.Data)
				if err != nil {
					h.t.Fatalf("alttpo would fail to parse o2 packet % x: %v", gm.Data, err)
				}
				sp.types = types
			} else if len(gm.Data) > 3 {
				sp.types = []uint8{gm.Data[3]}
			}
			pkts = append(pkts, sp)
		}
		frames = append(frames, pkts)
	}
	return
}

func countType(pkts []sentPacket, kind protocol04.Kind, t MessageType) (n int) {
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
		if n := countType(pkts, protocol04.BroadcastAll, MsgSprites1); n != 0 {
			t.Errorf("frame %d: sprites sent to all players", f)
		}
		sent += countType(pkts, protocol04.BroadcastSector, MsgSprites1)
	}
	// the first frame, then a refresh every spritesRefresh frames:
	if sent < 2 || sent > 3 {
		t.Errorf("expected unchanged sprites sent 2 or 3 times in %d frames; got %d", 2*spritesRefresh, sent)
	}

	// Link moves; sent at once:
	h.gs.e.WRAM[0x0800+0x64*4] ^= 0x10
	frames = h.sentPackets(2)
	if countType(frames[0], protocol04.BroadcastSector, MsgSprites1)+countType(frames[1], protocol04.BroadcastSector, MsgSprites1) == 0 {
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
				if p.kind != protocol04.BroadcastSector {
					continue
				}
				_, parsed, _ := alttpoParseFull(p.data)
				for _, sp := range parsed.sprites {
					if sp.palette != nil {
						withPalette++
					}
				}
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
			if p.kind == protocol04.BroadcastSector {
				_, parsed, _ := alttpoParseFull(p.data)
				for _, sp := range parsed.sprites {
					if sp.palette != nil {
						got = sp.palette
					}
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
			if p.kind != protocol04.BroadcastAll {
				continue
			}
			if bytes.IndexByte(p.types, byte(MsgLocation)) >= 0 || bytes.IndexByte(p.types, byte(MsgSmallKeys)) >= 0 {
				state++
			}
		}
		if state > 1 {
			t.Errorf("frame %d: expected the location and small keys in one packet; got %d", f, state)
		}
		keys += countType(pkts, protocol04.BroadcastAll, MsgSmallKeys)
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
				if p.kind != protocol04.BroadcastAll || len(p.data) <= 3 || p.data[3] != byte(MsgSRAM) {
					continue
				}
				for c := 3; c+6 <= len(p.data) && p.data[c] == byte(MsgSRAM); {
					start := int(p.data[c+2]) | int(p.data[c+3])<<8
					count := int(p.data[c+4]) | int(p.data[c+5])<<8
					if start < 0x250 {
						ranges = append(ranges, [2]int{start, start + count})
					}
					c += 6 + count
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
	for _, name := range []string{msgTypeName(MsgLocation), msgTypeName(MsgSprites1), msgTypeName(MsgSmallKeys)} {
		if c := s.kinds[name]; c == nil || c.msgs == 0 || c.payload == 0 {
			t.Errorf("expected %q counted; got %+v", name, c)
		}
	}
}
