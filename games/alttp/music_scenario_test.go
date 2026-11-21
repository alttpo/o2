package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

// TestMusicScenario_DisableAndRestart runs g1's game with NMI simulated: each frame the game requests the track in req
// (if not 0), the hook at the start of NMI runs, then the rest of the game's NMI sends what is left in $012C unless it
// is the last track sent ($0133), and records it in sent.
func TestMusicScenario_DisableAndRestart(t *testing.T) {
	h := newSfxScenario(t)
	e := h.gs[0].e
	const musicCmd, musicTune, musicLast = 0x012C, 0x0130, 0x0133

	var req uint8
	var sent []uint8
	h.gs[0].beforeEmulate = func() {
		e.WRAM[musicCmd] = req
		req = 0
		runTestNMI(t, e)
		if c := e.WRAM[musicCmd]; c != 0 && c != e.WRAM[musicLast] {
			sent = append(sent, c)
			e.WRAM[musicLast] = c
			if c < 0xF2 {
				e.WRAM[musicTune] = c
			}
		}
		e.WRAM[musicCmd] = 0
	}
	run := func(n int) { sent = nil; h.frames(n) }

	// enabled: a request goes through
	req = 0x0D
	run(3)
	if len(sent) != 1 || sent[0] != 0x0D {
		t.Fatalf("enabled: sent % x; expected 0d", sent)
	}

	// disabled: the track playing is faded out once
	e.SRAM[musicOffAddr&0x7FFF] = 1
	run(3)
	if len(sent) != 1 || sent[0] != 0xF1 {
		t.Fatalf("disabled: sent % x; expected a fade out (f1)", sent)
	}

	// the game's overworld transition: fade, then the area's track; nothing is sent and the game believes the track plays
	req = 0xF1
	run(2)
	req = 0x07
	run(3)
	if len(sent) != 0 {
		t.Fatalf("disabled: sent % x; expected nothing", sent)
	}
	if got := e.WRAM[musicTune]; got != 0x07 {
		t.Fatalf("disabled: the game's current track is $%02x; expected $07", got)
	}
	if got := e.SRAM[musicWantedAddr&0x7FFF]; got != 0x07 {
		t.Fatalf("remembered track $%02x; expected $07", got)
	}

	// a jingle that plays once is sent, which the game can wait for, and is not remembered:
	req = 0x0F
	run(3)
	if len(sent) != 1 || sent[0] != 0x0F {
		t.Fatalf("disabled: sent % x for a jingle; expected 0f", sent)
	}
	if got := e.SRAM[musicWantedAddr&0x7FFF]; got != 0x07 {
		t.Fatalf("remembered track $%02x after a jingle; expected $07", got)
	}

	// enabled again: the last track that repeats restarts once, even though $0133 held the jingle's number
	e.SRAM[musicOffAddr&0x7FFF] = 0
	run(4)
	if len(sent) != 1 || sent[0] != 0x07 {
		t.Fatalf("restart: sent % x; expected 07 once", sent)
	}
}

func TestMusicRequestAddr(t *testing.T) {
	_, rom, err := createTestEmulator("ZELDANODENSETSU", testWriter{t})
	if err != nil {
		t.Fatal(err)
	}
	pak, _ := lorom.BusAddressToPak(musicRequestOperandAddr)
	for _, c := range []struct {
		operand byte
		want    uint32
	}{{0x2C, 0x7E012C}, {0x2B, 0x7E012B}, {0x2D, 0}} {
		rom.Contents[pak] = c.operand
		if got := musicRequestAddr(rom); got != c.want {
			t.Errorf("operand $%02x: request at $%06x; expected $%06x", c.operand, got, c.want)
		}
	}
}
