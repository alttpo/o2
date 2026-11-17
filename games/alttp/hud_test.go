package alttp

import (
	"os"
	"testing"

	"github.com/alttpo/snes/asm"
	"o2/snes"
)

func TestHUDHookSite(t *testing.T) {
	for _, c := range []struct {
		file string
		want bool
	}{
		{"alttp-jp-vanilla.sfc", true},
		{"alttpr - NoGlitches-standard-ganon_ry08z8Q5y5.sfc", false},
	} {
		contents, err := os.ReadFile(c.file)
		if err != nil {
			t.Logf("ROM not available: %v", err)
			continue
		}
		rom, err := snes.NewROM(c.file, contents)
		if err != nil {
			t.Fatal(err)
		}
		if got := hudHookSite(rom) != 0; got != c.want {
			t.Errorf("%s: hook site found = %v; expected %v", c.file, got, c.want)
		}
	}
}

// TestHUDCheck runs hudCheck in the emulator: it asks for a rebuild on the first frame, after any input changes and
// after a frame it didn't run on, and skips it otherwise.
func TestHUDCheck(t *testing.T) {
	setupTestLogger(t)
	e, _, err := CreateTestEmulator("ZELDANODENSETSU", t)
	if err != nil {
		t.Fatal(err)
	}
	for i := range e.SRAM {
		e.SRAM[i] = 0
	}
	// hudCheck at $00:1000 (WRAM), and a stub at $00:1F00 that calls it and stores P at $00:0FF0:
	a := asm.NewEmitter(e.WRAM[0x1000:0x1100], true)
	a.SetBase(0x001000)
	emitHUDCheck(a)
	if err := a.Finalize(); err != nil {
		t.Fatal(err)
	}
	stub := []byte{
		0x18, 0xFB, // CLC; XCE
		0xE2, 0x30, // SEP #$30
		0x22, 0x00, 0x10, 0x00, // JSL $001000
		0x08,       // PHP
		0xE2, 0x30, // SEP #$30
		0x68,                   // PLA
		0x8F, 0xF0, 0x0F, 0x00, // STA.l $000FF0
	}
	copy(e.WRAM[0x1F00:], stub)
	frame := uint8(0x10)
	check := func(when string, wantSkip bool) {
		t.Helper()
		e.WRAM[0x1A] = frame
		frame++
		e.CPU.Reset()
		e.SetPC(0x001F00)
		if !e.RunUntil(0x001F00+uint32(len(stub)), 0x2_000) {
			t.Fatalf("%s: hudCheck did not return; PC=$%06x", when, e.GetPC())
		}
		p := e.WRAM[0x0FF0]
		if skip := p&1 != 0; skip != wantSkip {
			t.Errorf("%s: skip = %v; expected %v", when, skip, wantSkip)
		}
		if p&0x30 != 0 {
			t.Errorf("%s: returned with P = $%02x; expected A, X and Y 16-bit", when, p)
		}
		if e.WRAM[0x0E] != 0xFF || e.WRAM[0x0F] != 0xFF {
			t.Errorf("%s: $0E = $%02x%02x; expected $FFFF", when, e.WRAM[0x0F], e.WRAM[0x0E])
		}
	}
	sram := e.WRAM[0xF300:]
	sram[0x6C], sram[0x6D], sram[0x62], sram[0x43], sram[0x77] = 0x50, 0x48, 0x20, 10, 30

	check("first frame", false)
	check("unchanged", true)
	check("unchanged", true)
	for _, c := range []struct {
		name string
		offs int
	}{{"health", 0x6D}, {"maximum health", 0x6C}, {"magic", 0x6E}, {"keys", 0x6F}, {"rupees", 0x62},
		{"rupees high byte", 0x63}, {"bombs", 0x43}, {"arrows", 0x77}, {"half magic", 0x7B}} {
		sram[c.offs]++
		check(c.name+" changed", false)
		check(c.name+" unchanged after", true)
	}
	// a frame it didn't run on:
	frame++
	check("after a skipped frame", false)
	check("unchanged after a skipped frame", true)
}
