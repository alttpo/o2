package alttp

import "testing"

// TestMusicScenario_DisableAndRestart runs g1's game with NMI simulated: each frame the game requests the track in req (if not 0), and
// sent records what is in $012C when the frame is done, which NMI would send to the APU.
func TestMusicScenario_DisableAndRestart(t *testing.T) {
	h := newSfxScenario(t)
	e := h.gs[0].e
	const musicCmd, musicTune = 0x012C, 0x0130

	var req uint8
	// the game requests during the frame; NMI is simulated before the next one:
	var sent []uint8
	h.gs[0].beforeEmulate = func() {
		// NMI sends the request that is left after its first instructions, and the game now plays it:
		runTestNMI(t, e)
		if e.WRAM[musicCmd] != 0 {
			sent = append(sent, e.WRAM[musicCmd])
			e.WRAM[musicTune] = e.WRAM[musicCmd]
		}
		e.WRAM[musicCmd] = req
		req = 0
	}
	run := func(n int) { sent = nil; h.frames(n) }

	// enabled: a request goes through
	req = 0x0D
	run(3)
	if len(sent) != 1 || sent[0] != 0x0D {
		t.Fatalf("enabled: sent % x; expected 0d", sent)
	}

	// disabled: the track playing is faded out, new requests are dropped
	e.SRAM[musicOffAddr&0x7FFF] = 1
	run(3)
	if len(sent) != 1 || sent[0] != 0xF1 {
		t.Fatalf("disabled: sent % x; expected a fade out (f1)", sent)
	}
	req = 0x07
	run(3)
	if len(sent) != 0 {
		t.Fatalf("disabled: sent % x; expected nothing", sent)
	}
	// a jingle that plays once is dropped and not remembered:
	req = 0x0F
	run(3)
	if len(sent) != 0 {
		t.Fatalf("disabled: sent % x for a jingle; expected nothing", sent)
	}
	if got := e.SRAM[musicWantedAddr&0x7FFF]; got != 0x07 {
		t.Fatalf("remembered track $%02x; expected $07", got)
	}

	// enabled again: the last requested track restarts once
	e.SRAM[musicOffAddr&0x7FFF] = 0
	run(4)
	if len(sent) != 1 || sent[0] != 0x07 {
		t.Fatalf("restart: sent % x; expected 07 once", sent)
	}
}
