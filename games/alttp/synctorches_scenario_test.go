package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

const (
	ttRoom      = uint16(0x00C7) // a Turtle Rock room with a torch puzzle door
	ttOtherRoom = uint16(0x00C6)
	ttTrapDoor  = uint32(0x0468)
)

// ttTorchPos are the tilemap positions of the room's 4 torches.
var ttTorchPos = [4]uint16{0x0208, 0x0230, 0x0C08, 0x0C30}

type ttScenario struct {
	*skScenario
	// tick counts frames to decrement the torch timers every 4 frames like Underworld_ProcessTorchesAndDoors:
	tick int
}

// newTorchScenario stubs Underworld_LightTorch: set bit 15 of the torch's position and its timer to $C0.
func newTorchScenario(t *testing.T, players int) *ttScenario {
	t.Helper()
	h := &ttScenario{skScenario: newSKScenario(t, players)}
	for _, gs := range h.gs {
		addr := gs.g.romFunctions[fnUnderworldLightTorch]
		pak, _ := lorom.BusAddressToPak(addr)
		copy(gs.e.ROM[pak:], []byte{
			0xAD, 0x33, 0x03, // LDA $0333
			0x29, 0x0F, // AND #$0F
			0x0A,             // ASL
			0x18,             // CLC
			0x6D, 0x78, 0x04, // ADC $0478
			0xAA,             // TAX
			0xBD, 0x41, 0x05, // LDA $0541,X
			0x09, 0x80, // ORA #$80
			0x9D, 0x41, 0x05, // STA $0541,X
			0xAD, 0x33, 0x03, // LDA $0333
			0x29, 0x0F, // AND #$0F
			0xAA,       // TAX
			0xA9, 0xC0, // LDA #$C0
			0x9D, 0xF0, 0x04, // STA $04F0,X
			0x9C, 0x33, 0x03, // STZ $0333
			0x6B, // RTL
		})
	}
	return h
}

// loadTorchRoom does what the room loader does for a room with a pot and the 4 torches, all unlit.
func (h *ttScenario) loadTorchRoom(i int, room uint16) {
	h.inRoom(i, room)
	e := h.gs[i].e
	for k := uint32(0); k < 0x100; k++ {
		e.WRAM[0x0500+k] = 0
	}
	for t := uint32(0); t < 0x10; t++ {
		e.WRAM[uint32(torchTimers)+t] = 0
	}
	// one pot in slot 0, torches in slots 1-4:
	h.setWRAMU16(i, uint32(manipType), 0x1010)
	h.setWRAMU16(i, uint32(manipObjX), 0x0030)
	h.setWRAMU16(i, uint32(manipPos), 0x0410)
	h.setWRAMU16(i, uint32(manipCount), 2)
	for t, pos := range ttTorchPos {
		h.setWRAMU16(i, uint32(manipObjX)+uint32(1+t)<<1, uint16(0x10+t<<1))
		h.setWRAMU16(i, uint32(manipPos)+uint32(1+t)<<1, pos)
	}
	e.WRAM[ttTrapDoor] = 1
}

func (h *ttScenario) torchSlot(i, t int) uint32 {
	return uint32(manipPos) + uint32(h.wramU16(i, uint32(manipCount))) + uint32(t<<1)
}

// lightTorch does what lighting a torch with the lamp does.
func (h *ttScenario) lightTorch(i, t int) {
	a := h.torchSlot(i, t)
	h.setWRAMU16(i, a, h.wramU16(i, a)|0x8000)
	h.gs[i].e.WRAM[uint32(torchTimers)+uint32(t)] = 0xC0
}

func (h *ttScenario) isLit(i, t int) bool {
	return h.wramU16(i, h.torchSlot(i, t))&0x8000 != 0
}

// frame runs a frame and then does what Underworld_ProcessTorchesAndDoors and RoomTag_TorchPuzzleDoor do in rooms
// with torches.
func (h *ttScenario) frame() {
	h.skScenario.frame()
	h.tick++
	for i, gs := range h.gs {
		e := gs.e
		if e.WRAM[skModule] != 0x07 || e.WRAM[skSubModule] != 0 || h.wramU16(i, uint32(manipPos)+2) == 0 {
			continue
		}
		lit := 0
		for t := range ttTorchPos {
			if h.tick&3 == 0 {
				timer := &e.WRAM[uint32(torchTimers)+uint32(t)]
				if *timer > 0 {
					*timer--
					if *timer == 0 {
						a := h.torchSlot(i, t)
						h.setWRAMU16(i, a, h.wramU16(i, a)&0x7FFF)
					}
				}
			}
			if h.isLit(i, t) {
				lit++
			}
		}
		if lit >= 4 {
			e.WRAM[ttTrapDoor] = 0
		} else {
			e.WRAM[ttTrapDoor] = 1
		}
	}
}

func (h *ttScenario) frames(n int) {
	for f := 0; f < n; f++ {
		h.frame()
	}
}

// expectLit checks which torches are lit for each player, as a bit mask per player.
func (h *ttScenario) expectLit(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		actual := uint8(0)
		for t := range ttTorchPos {
			if h.isLit(i, t) {
				actual |= 1 << t
			}
		}
		if actual != x {
			h.t.Errorf("%s: g%d: expected torches %04b lit; got %04b", when, i+1, x, actual)
		}
	}
}

func (h *ttScenario) expectDoor(when string, expected ...uint8) {
	h.t.Helper()
	for i, x := range expected {
		if actual := h.gs[i].e.WRAM[ttTrapDoor]; actual != x {
			h.t.Errorf("%s: g%d: expected trap door $%02x; got $%02x", when, i+1, x, actual)
		}
	}
}

func TestTorchesScenario_Light(t *testing.T) {
	h := newTorchScenario(t, 2)
	h.loadTorchRoom(0, ttRoom)
	h.loadTorchRoom(1, ttRoom)
	h.frames(skSettleFrames)
	h.expectLit("initial", 0, 0)

	h.lightTorch(0, 1)
	h.frames(skSettleFrames)
	h.expectLit("g1 lit torch 1", 0b0010, 0b0010)
	if d := int(h.gs[0].e.WRAM[uint32(torchTimers)+1]) - int(h.gs[1].e.WRAM[uint32(torchTimers)+1]); d < -torchTimerSlack || d > torchTimerSlack {
		t.Errorf("expected the timers to be in step; differ by %d", d)
	}
	if o := h.gs[1].g.torches.owner[1]; o == nil || o == h.gs[1].g.local {
		t.Errorf("g2: expected torch 1 to belong to g1")
	}
}

// TestTorchesScenario_PuzzleDoor: two players light two torches each; the trap door opens for both and closes for both
// once the first torch goes out, and nothing is lit again afterwards.
func TestTorchesScenario_PuzzleDoor(t *testing.T) {
	h := newTorchScenario(t, 2)
	h.loadTorchRoom(0, ttRoom)
	h.loadTorchRoom(1, ttRoom)
	h.frames(skSettleFrames)

	h.lightTorch(0, 0)
	h.frames(20)
	h.lightTorch(1, 1)
	h.lightTorch(0, 2)
	h.frames(20)
	h.lightTorch(1, 3)
	h.frames(skSettleFrames)
	h.expectLit("all lit", 0b1111, 0b1111)
	h.expectDoor("all lit", 0, 0)

	// torch 0 was lit first and goes out first, for both players:
	for f := 0; f < 0xC0*4 && h.isLit(0, 0); f++ {
		h.frame()
	}
	h.frames(skSettleFrames)
	h.expectLit("torch 0 out", 0b1110, 0b1110)
	h.expectDoor("torch 0 out", 1, 1)

	h.frames(0xC0 * 4)
	h.expectLit("all out", 0, 0)
	h.expectDoor("all out", 1, 1)
	h.frames(skSettleFrames * 4)
	h.expectLit("settled", 0, 0)
}

// TestTorchesScenario_NoRelight: a torch that goes out here a little earlier than for its owner must not be lit again.
func TestTorchesScenario_NoRelight(t *testing.T) {
	h := newTorchScenario(t, 2)
	h.loadTorchRoom(0, ttRoom)
	h.loadTorchRoom(1, ttRoom)
	h.frames(skSettleFrames)

	h.lightTorch(0, 0)
	h.frames(skSettleFrames)
	h.expectLit("lit", 0b0001, 0b0001)

	// g2's torch is about to go out while g1's still has a few frames left (within the slack, so it is not set to
	// g1's timer):
	h.gs[0].e.WRAM[torchTimers] = 1 + torchTimerSlack
	h.gs[1].e.WRAM[torchTimers] = 1
	wentOut, relit := false, false
	for f := 0; f < 40; f++ {
		h.frame()
		if !h.isLit(1, 0) {
			wentOut = true
		} else if wentOut {
			relit = true
		}
	}
	if !wentOut || relit {
		t.Errorf("g2: expected the torch to go out and not be lit again; went out: %v, lit again: %v", wentOut, relit)
	}
	h.expectLit("out", 0, 0)
}

func TestTorchesScenario_OtherRoom(t *testing.T) {
	h := newTorchScenario(t, 2)
	h.loadTorchRoom(0, ttRoom)
	h.loadTorchRoom(1, ttOtherRoom)
	h.frames(skSettleFrames)

	h.lightTorch(0, 0)
	h.frames(skSettleFrames)
	h.expectLit("other room", 0b0001, 0)
}

// TestTorchesScenario_EnterRoomCatchesUp: a player entering the room sees the torches lit there; when their owner
// leaves, they stay lit and are now sent by the remaining player.
func TestTorchesScenario_EnterRoomCatchesUp(t *testing.T) {
	h := newTorchScenario(t, 3)
	h.loadTorchRoom(0, ttRoom)
	h.loadTorchRoom(1, ttOtherRoom)
	h.loadTorchRoom(2, ttOtherRoom)
	h.frames(skSettleFrames)

	h.lightTorch(0, 2)
	h.frames(skSettleFrames)

	h.gs[1].e.WRAM[skSubModule] = 0x02
	h.frames(2)
	h.loadTorchRoom(1, ttRoom)
	h.frames(skSettleFrames)
	h.expectLit("g2 entered", 0b0100, 0b0100, 0)

	// g1 leaves; g2 keeps the torch burning and shares it with g3 entering:
	h.gs[0].e.WRAM[skSubModule] = 0x02
	h.frames(2)
	h.loadTorchRoom(0, ttOtherRoom)
	h.frames(skSettleFrames)
	if o := h.gs[1].g.torches.owner[2]; o != h.gs[1].g.local {
		t.Errorf("g2: expected to own torch 2 after g1 left")
	}
	h.gs[2].e.WRAM[skSubModule] = 0x02
	h.frames(2)
	h.loadTorchRoom(2, ttRoom)
	h.frames(skSettleFrames)
	h.expectLit("g3 entered", 0, 0b0100, 0b0100)
}

func TestAlttpoCompat_TorchesFromAlttpo(t *testing.T) {
	h := newTorchScenario(t, 1)
	g := h.gs[0].g
	h.loadTorchRoom(0, ttRoom)
	h.frames(skSettleFrames)

	send := func(frame uint8, torches ...uint8) {
		p := newAlttpoPacket(3, frame)
		p.location(0x07, skEastern, 0x010000|uint32(ttRoom), 0, 0)
		// LocalGameState::serialize_torches:
		p.m(MsgTorches).u8(uint8(len(torches) / 2)).u8(torches...)
		p.name("alttpo")
		p.send(t, g)
	}

	send(1, 1, 0xB0, 3, 0x80)
	h.frames(skSettleFrames)
	send(2, 1, 0xB0-3, 3, 0x80-3)
	h.frames(skSettleFrames)
	h.expectLit("alttpo lit 1 and 3", 0b1010)
	if timer := h.gs[0].e.WRAM[uint32(torchTimers)+3]; timer > 0x80 || timer < 0x80-3-torchTimerSlack-3 {
		t.Errorf("expected torch 3's timer to follow alttpo's; got $%02x", timer)
	}

	// alttpo's torch 1 went out; ours burns out by itself shortly after:
	h.gs[0].e.WRAM[uint32(torchTimers)+1] = 2
	send(3, 3, 0x78)
	h.frames(skSettleFrames)
	h.expectLit("alttpo torch 1 out", 0b1000)

	// the o2 player lights torch 0 and sends it:
	h.lightTorch(0, 0)
	h.frames(skSettleFrames)
	var b bytesWriter
	if err := g.SerializeTorches(&b); err != nil {
		t.Fatal(err)
	}
	if len(b) != 4 || b[0] != 0x0A || b[1] != 1 || b[2] != 0 {
		t.Errorf("expected o2 to send torch 0 only; got % x", []byte(b))
	}
}

type bytesWriter []byte

func (b *bytesWriter) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}
