package alttp

import (
	"bytes"
	"github.com/alttpo/snes/emulator"
	"o2/snes"
	"os"
	"testing"
)

// TestSwordShieldGfx_MatchesGame runs the game's own DecompressSwordGraphics and DecompressShieldGraphics routines in
// the CPU emulator against a real ROM and compares the resulting WRAM tiles with o2's implementation for every sword
// and shield level.
//
// The ROMs are not part of the repository; point O2_TEST_ROM_US / O2_TEST_ROM_JP at an ALTTP US 1.0 / JP 1.0 ROM
// (defaults: ../../../alttp-us.sfc and ../../../alttp-jp.sfc relative to this package).
func TestSwordShieldGfx_MatchesGame(t *testing.T) {
	for _, tc := range []struct {
		name, env, path string
		sword, shield   uint32
	}{
		{"US", "O2_TEST_ROM_US", "../../../alttp-us.sfc", 0x00D2C8, 0x00D308},
		{"JP", "O2_TEST_ROM_JP", "../../../alttp-jp.sfc", 0x00D308, 0x00D348},
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
			if !g.swordShield.ok {
				t.Fatal("sword/shield graphics routines not found in ROM")
			}

			sys := &emulator.System{}
			if err = sys.CreateEmulator(); err != nil {
				t.Fatal(err)
			}
			copy(sys.ROM[:], contents)

			callRoutine := func(addr uint32) {
				t.Helper()
				// stub in low WRAM: switch to native mode, reset stack and direct page, call the routine:
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
				if !sys.RunUntil(0x001F00+uint32(len(stub)), 10_000_000) {
					t.Fatalf("routine at $%06x did not return; PC=$%06x", addr, sys.GetPC())
				}
			}

			for sword := uint8(0); sword <= 4; sword++ {
				for shield := uint8(0); shield <= 3; shield++ {
					sys.WRAM[0xF359], sys.WRAM[0xF35A] = sword, shield
					for i := 0x9000; i < 0x9480; i++ {
						sys.WRAM[i] = 0
					}
					callRoutine(tc.sword)
					callRoutine(tc.shield)

					g.updateSwordShieldGfx(sword, shield)
					for i := 0; i < swordShieldTileCount; i++ {
						expected := sys.WRAM[0x9000+i<<5 : 0x9000+i<<5+32]
						actual, ok := g.swordShieldTile(uint16(swordShieldTileFirst + i))
						if !ok {
							t.Fatalf("sword %d shield %d: tile %d not available", sword, shield, i)
						}
						if !bytes.Equal(expected, actual) {
							t.Errorf("sword %d shield %d: tile $%03x (WRAM $%04x) differs:\n game % x\n   o2 % x",
								sword, shield, swordShieldTileFirst+i, 0x9000+i<<5, expected, actual)
						}
					}

					// sanity check that the tiles are not empty:
					if sword > 0 && bytes.Equal(sys.WRAM[0x9000:0x9300], make([]byte, 0x300)) {
						t.Errorf("sword %d: game produced no tiles", sword)
					}
					if shield > 0 && bytes.Equal(sys.WRAM[0x9300:0x9480], make([]byte, 0x180)) {
						t.Errorf("shield %d: game produced no tiles", shield)
					}
				}
			}

			// changing only the sword re-sends only sword tiles:
			g.updateSwordShieldGfx(1, 1)
			changed := g.updateSwordShieldGfx(2, 1)
			if len(changed) == 0 {
				t.Errorf("expected sword tiles to change from fighter to master sword")
			}
			for _, idx := range changed {
				if idx < swordShieldTileFirst || idx >= swordShieldTileFirst+swordTileCount {
					t.Errorf("unexpected changed tile %#03x", idx)
				}
			}
			if changed := g.updateSwordShieldGfx(2, 1); len(changed) != 0 {
				t.Errorf("expected no changes without an equipment change; got %x", changed)
			}
		})
	}
}
