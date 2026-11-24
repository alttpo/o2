package alttp

import (
	"bytes"
	"github.com/alttpo/snes/emulator"
	"o2/snes"
	"os"
	"testing"
)

// TestFollowerFact_MatchesGame builds the follower graphics fact (fact 0x01) and compares each follower's part with what
// the game's LoadFollowerGraphics builds in WRAM for that follower, run in the CPU emulator against a real ROM.
//
// The ROMs are not part of the repository; see TestSwordShieldGfx_MatchesGame.
func TestFollowerFact_MatchesGame(t *testing.T) {
	for _, tc := range []struct {
		name, env, path string
		routine         uint32
	}{
		{"US", "O2_TEST_ROM_US", "../../../alttp-us.sfc", romFollowerGfxUS},
		{"JP", "O2_TEST_ROM_JP", "../../../alttp-jp.sfc", romFollowerGfxJP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := os.Getenv(tc.env)
			if path == "" {
				path = tc.path
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("no %s ROM available (set %s): %v", tc.name, tc.env, err)
			}
			rom, err := snes.NewROM(path, contents)
			if err != nil {
				t.Fatal(err)
			}
			g := NewGame(rom)
			fact, err := g.buildFollowerFact()
			if err != nil {
				t.Fatal(err)
			}
			if len(fact) != followerFactSize {
				t.Fatalf("fact is %d bytes; expected %d", len(fact), followerFactSize)
			}

			sys := &emulator.System{}
			if err = sys.CreateEmulator(); err != nil {
				t.Fatal(err)
			}
			copy(sys.ROM[:], contents)
			stub := []byte{
				0x18, 0xFB, // CLC; XCE
				0xC2, 0x30, // REP #$30
				0xA2, 0xFF, 0x01, // LDX #$01FF
				0x9A,             // TXS
				0xA9, 0x00, 0x00, // LDA #$0000
				0x5B,       // TCD
				0xE2, 0x30, // SEP #$30
				0x22, uint8(tc.routine), uint8(tc.routine >> 8), uint8(tc.routine >> 16), // JSL LoadFollowerGraphics
			}
			for f := 0; f < followerCount; f++ {
				sys.WRAM[0xF3CC] = uint8(f)
				clear(sys.WRAM[followerGfxWRAM : followerGfxWRAM+followerGfxSize])
				copy(sys.WRAM[0x1F00:], stub)
				sys.CPU.Reset()
				sys.SetPC(0x001F00)
				if !sys.RunUntil(0x001F00+uint32(len(stub)), 50_000_000) {
					t.Fatalf("follower %#02x: LoadFollowerGraphics did not return; PC=$%06x", f, sys.GetPC())
				}
				game := sys.WRAM[followerGfxWRAM : followerGfxWRAM+followerGfxSize]
				ours := fact[f*followerGfxSize : (f+1)*followerGfxSize]
				if bytes.Equal(game, make([]byte, followerGfxSize)) {
					t.Errorf("follower %#02x: the game built nothing", f)
				}
				for i := 0; i < followerGfxSize; i += 32 {
					if !bytes.Equal(game[i:i+32], ours[i:i+32]) {
						t.Errorf("follower %#02x: tile at WRAM $%04x differs:\n game % x\n fact % x", f, followerGfxWRAM+i, game[i:i+32], ours[i:i+32])
						break
					}
				}
			}
		})
	}
}

// The randomizer ROMs are JP-based: LoadFollowerGraphics is found there too, with its table in the FastROM bank.
func TestFollowerFact_Randomizer(t *testing.T) {
	path := "alttpr - NoGlitches-standard-ganon_ry08z8Q5y5.sfc"
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no randomizer ROM available: %v", err)
	}
	rom, err := snes.NewROM(path, contents)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGame(rom)
	r, err := g.findFollowerGfxRoutine()
	if err != nil {
		t.Fatal(err)
	}
	if r.gfxOffsets != 0x80D447 {
		t.Errorf("gfx offsets at $%06x; expected $80D447", r.gfxOffsets)
	}
	if _, err = g.buildFollowerFact(); err != nil {
		t.Fatal(err)
	}
}

func TestFollowerFactTile(t *testing.T) {
	for _, tc := range []struct {
		follower, dma uint8
		want          uint16
	}{
		{0x00, 0x20, noFactTile},
		{0x01, 0x00, 32},
		{0x01, 0x20, 34},
		{0x06, 0xE0, 6*32 + 14},
		{0x0D, 0x60, 13*32 + 6},
		{0x0E, 0x20, noFactTile},
		{0x01, 0x10, 33},
		{0x01, 0x08, noFactTile},
		{0x01, 0xF0, noFactTile},
	} {
		if got := followerFactTile(tc.follower, tc.dma); got != tc.want {
			t.Errorf("followerFactTile(%#02x, %#02x) = %#x; want %#x", tc.follower, tc.dma, got, tc.want)
		}
	}
}
