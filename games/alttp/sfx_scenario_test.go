package alttp

import (
	"testing"
)

const (
	sfxWRAM2 = 0x012E
	sfxWRAM3 = 0x012F
)

// sfxScenario sets up g1 and g2 in the same room, with g2 80 pixels right of g1. Each player's NMI is simulated by
// clearing $012E/$012F before the frame; queue sets the sounds the module queues on the next frames; played records
// the sounds left in $012E/$012F after each frame (what NMI would send to the APU).
type sfxScenario struct {
	*skScenario
	queue  [][][2]uint8
	played [][][2]uint8
}

func newSfxScenario(t *testing.T) *sfxScenario {
	t.Helper()
	h := &sfxScenario{skScenario: newSKScenario(t, 2)}
	h.queue = make([][][2]uint8, 2)
	h.played = make([][][2]uint8, 2)
	for i := range h.gs {
		h.inRoom(i, rsRoom)
	}
	h.setScroll(0, 0x1050, 0x0250, 0x1000, 0x0200)
	h.setScroll(1, 0x10A0, 0x0250, 0x1000, 0x0200)
	for i := range h.gs {
		i := i
		e := h.gs[i].e
		h.gs[i].beforeEmulate = func() {
			// NMI sent what was left there to the APU:
			h.played[i] = append(h.played[i], [2]uint8{e.WRAM[sfxWRAM2], e.WRAM[sfxWRAM3]})
			e.WRAM[sfxWRAM2], e.WRAM[sfxWRAM3] = 0, 0
			// the module queues sounds:
			if len(h.queue[i]) > 0 {
				e.WRAM[sfxWRAM2], e.WRAM[sfxWRAM3] = h.queue[i][0][0], h.queue[i][0][1]
				h.queue[i] = h.queue[i][1:]
			}
		}
	}
	h.frames(skSettleFrames)
	h.played = make([][][2]uint8, 2)
	return h
}

// playedSounds returns the sounds player i played, in order.
func (h *sfxScenario) playedSounds(i int) (s [][2]uint8) {
	for _, p := range h.played[i] {
		if p != [2]uint8{} {
			s = append(s, p)
		}
	}
	return
}

func TestSfxScenario_RemoteSoundPlayed(t *testing.T) {
	h := newSfxScenario(t)

	// g2 swings its sword ($012E = $01, centered) and later hits a wall ($012F = $05 panned left):
	h.queue[1] = [][2]uint8{{0x01, 0x00}, {0, 0}, {0, 0}, {0x00, 0x85}}
	h.frames(16)

	// g1 hears both, panned right since g2 is 80 pixels to the right (the wall hit 40 pixels left of g2 is 40 pixels
	// right of g1):
	played := h.playedSounds(0)
	expected := [][2]uint8{{0x41, 0x00}, {0x00, 0x45}}
	if len(played) != len(expected) || played[0] != expected[0] || played[1] != expected[1] {
		t.Errorf("g1 played % x; expected % x", played, expected)
	}
	// g2 only played its own sounds, once each:
	if played := h.playedSounds(1); len(played) != 2 || played[0] != [2]uint8{0x01, 0} || played[1] != [2]uint8{0, 0x85} {
		t.Errorf("g2 played % x; expected only its own sounds", played)
	}
}

func TestSfxScenario_LocalSoundsFirst(t *testing.T) {
	h := newSfxScenario(t)

	// g1 queues a sound in $012E every frame for a while; g2's sound waits for the slot to be free, while its $012F
	// sound is played right away:
	for f := 0; f < 12; f++ {
		h.queue[0] = append(h.queue[0], [2]uint8{0x0B, 0x00})
	}
	h.queue[1] = [][2]uint8{{0x01, 0x2C}}
	h.frames(20)

	played := h.played[0]
	firstRemote2, firstRemote3 := -1, -1
	for f, p := range played {
		if p[0] != 0 && p[0] != 0x0B && firstRemote2 < 0 {
			firstRemote2 = f
		}
		if p[1] != 0 && firstRemote3 < 0 {
			firstRemote3 = f
		}
		// played[f] is what the frame before it left for NMI:
		if f >= 1 && f <= 12 && p[0] != 0x0B {
			t.Errorf("frame %d: g1's own sound was replaced by $%02x", f, p[0])
		}
	}
	if firstRemote3 < 1 || firstRemote3 > 12 {
		t.Errorf("g2's SFX3 was played on frame %d; expected while g1's SFX2 was busy", firstRemote3)
	}
	if firstRemote2 <= 12 {
		t.Errorf("g2's SFX2 was played on frame %d; expected after g1's own sounds", firstRemote2)
	}
	if firstRemote2 >= 0 && played[firstRemote2][0] != 0x41 {
		t.Errorf("g2's SFX2 played as $%02x; expected $41", played[firstRemote2][0])
	}
}

func TestSfxScenario_FilteredAndOtherRoom(t *testing.T) {
	h := newSfxScenario(t)

	// the low life warning beep and menu sounds are not sent:
	h.queue[1] = [][2]uint8{{0x2B, 0x00}, {0x00, 0x11}}
	h.frames(12)
	if played := h.playedSounds(0); len(played) != 0 {
		t.Errorf("g1 played % x; expected nothing", played)
	}

	// sounds of players in another room are not played:
	h.inRoom(1, rsOtherRoom)
	h.frames(skSettleFrames)
	h.played[0] = nil
	h.queue[1] = [][2]uint8{{0x01, 0x00}}
	h.frames(12)
	if played := h.playedSounds(0); len(played) != 0 {
		t.Errorf("g1 played % x from another room; expected nothing", played)
	}
}

func TestIsSfxFiltered(t *testing.T) {
	for _, c := range []struct {
		slot int
		sfx  uint8
		want bool
	}{
		{0, 0x2B, true},
		{0, 0x36, true}, // thunder, with any pan
		{0, 0x76, true},
		{0, 0xB6, true},
		{0, 0x35, false},
		{0, 0x01, false},
		{1, 0x36, false},
		{1, 0x10, true},
		{1, 0x50, true},
		{1, 0x2B, false},
	} {
		if got := isSfxFiltered(c.slot, c.sfx); got != c.want {
			t.Errorf("isSfxFiltered(%d, $%02x) = %v; expected %v", c.slot, c.sfx, got, c.want)
		}
	}
}
