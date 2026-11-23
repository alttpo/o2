package alttp

import (
	"bytes"
	"github.com/alttpo/snes/emulator"
	"o2/snes"
	"os"
	"testing"
)

// TestLinkFact_MatchesGame builds the Link graphics fact (fact 0x00) and compares each part with what the game's own
// routines build in WRAM, run in the CPU emulator against a real ROM: LoadItemGFXIntoWRAM4BPPBuffer (the item buffer and,
// through LoadItemGFX_Auxiliary, the bird) and DecompressSwordGraphics and DecompressShieldGraphics for every level.
//
// The ROMs are not part of the repository; see TestSwordShieldGfx_MatchesGame.
func TestLinkFact_MatchesGame(t *testing.T) {
	for _, tc := range []struct {
		name, env, path      string
		items, sword, shield uint32
	}{
		{"US", "O2_TEST_ROM_US", "../../../alttp-us.sfc", 0x00D231, 0x00D2C8, 0x00D308},
		{"JP", "O2_TEST_ROM_JP", "../../../alttp-jp.sfc", 0x00D271, 0x00D308, 0x00D348},
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
			fact, err := g.buildLinkFact()
			if err != nil {
				t.Fatal(err)
			}
			if len(fact) != linkFactSize {
				t.Fatalf("fact is %d bytes; expected %d", len(fact), linkFactSize)
			}

			sys := &emulator.System{}
			if err = sys.CreateEmulator(); err != nil {
				t.Fatal(err)
			}
			copy(sys.ROM[:], contents)
			callRoutine := func(addr uint32) {
				t.Helper()
				stub := []byte{
					0x18, 0xFB, // CLC; XCE
					0xC2, 0x30, // REP #$30
					0xA2, 0xFF, 0x01, // LDX #$01FF
					0x9A,             // TXS
					0xA9, 0x00, 0x00, // LDA #$0000
					0x5B,       // TCD
					0xE2, 0x30, // SEP #$30
					0x22, uint8(addr), uint8(addr >> 8), uint8(addr >> 16), // JSL addr
				}
				copy(sys.WRAM[0x1F00:], stub)
				sys.CPU.Reset()
				sys.SetPC(0x001F00)
				if !sys.RunUntil(0x001F00+uint32(len(stub)), 50_000_000) {
					t.Fatalf("routine at $%06x did not return; PC=$%06x", addr, sys.GetPC())
				}
			}
			compare := func(what string, wram, factOff, size int) {
				t.Helper()
				game, ours := sys.WRAM[wram:wram+size], fact[factOff:factOff+size]
				if bytes.Equal(game, make([]byte, size)) {
					t.Errorf("%s: the game built nothing", what)
				}
				for i := 0; i < size; i += 32 {
					if !bytes.Equal(game[i:i+32], ours[i:i+32]) {
						t.Errorf("%s: tile at WRAM $%04x differs:\n game % x\n fact % x", what, wram+i, game[i:i+32], ours[i:i+32])
						return
					}
				}
			}

			callRoutine(tc.items)
			compare("items", lfItemsWRAM, lfItems, lfItemsSize)
			compare("bird", lfBirdWRAM, lfBird, lfBirdSize)

			for level := 1; level <= 4; level++ {
				sys.WRAM[0xF359] = uint8(level)
				callRoutine(tc.sword)
				compare("sword", lfSwordWRAM, lfSwords+(level-1)*lfSwordSize, lfSwordSize)
			}
			for level := 1; level <= 3; level++ {
				sys.WRAM[0xF35A] = uint8(level)
				callRoutine(tc.shield)
				compare("shield", lfShieldWRAM, lfShields+(level-1)*lfShieldSize, lfShieldSize)
			}

			for i := 0; i < lfBodySize>>5; i += 0x37 {
				tile, _ := g.linkTile(uint16(i))
				if !bytes.Equal(fact[i<<5:i<<5+32], tile) {
					t.Errorf("body tile %#03x differs from LinkGraphics", i)
				}
			}
		})
	}
}

func TestLinkFactTile(t *testing.T) {
	for _, tc := range []struct {
		src           uint16
		wram          bool
		sword, shield uint8
		want          uint16
	}{
		{0x8000, false, 0, 0, 0},
		{0x8420, false, 0, 0, 0x21},
		{0xF000, false, 0, 0, noFactTile},
		{0x9020, true, 2, 0, (lfSwords + lfSwordSize + 0x20) >> 5},
		{0x9020, true, 0, 0, noFactTile},
		{0x9340, true, 0, 3, (lfShields + 2*lfShieldSize + 0x40) >> 5},
		{0x94C0, true, 0, 0, (lfItems + 0x40) >> 5},
		{0xB580, true, 0, 0, (lfBird + 0x40) >> 5},
		{0xA480, true, 1, 1, noFactTile},
	} {
		if got := linkFactTile(tc.src, tc.wram, tc.sword, tc.shield); got != tc.want {
			t.Errorf("linkFactTile(%04x, %v, %d, %d) = %#x; want %#x", tc.src, tc.wram, tc.sword, tc.shield, got, tc.want)
		}
	}
}
