package alttp

import (
	"bytes"
	"o2/interfaces"
	"o2/snes/asm"
	"o2/snes/emulator"
	"o2/util"
	"strings"
	"testing"
)

// sramTest represents a single byte of SRAM used for verifying sync logic
type sramTest struct {
	// offset from $7EF000 in WRAM, e.g. $340 for bow, $341 for boomerang, etc.
	offset uint16
	// value to set for the local player
	localValue uint8
	// value to set for the remote player syncing in
	remoteValue uint8
	// expected value to see for the local player after ASM code runs
	expectedValue uint8
}

type sramTestCase struct {
	name string
	// individual bytes of SRAM to be set and tested
	sram        []sramTest
	wantUpdated bool
	// expected front-end notification to be sent or "" if none expected
	wantNotification string
	// any verification logic to run after verifying update:
	verify func(t *testing.T, g *Game, system *emulator.System, tt *sramTestCase)
}

type testingLogger struct {
	t *testing.T
}

func (l *testingLogger) WriteString(s string) (n int, err error) {
	l.t.Log(s)
	return len(s), nil
}

func runAsmEmulationTests(t *testing.T, romTitle string, tests []sramTestCase) {
	// create a ROM for testing our patch process and the generated ASM code:
	rom, err := emulator.MakeTestROM(romTitle)
	if err != nil {
		t.Fatal(err)
	}

	// create the CPU-only SNES emulator:
	system := emulator.System{
		Logger: &testingLogger{t},
		ShouldLogCPU: func(s *emulator.System) bool {
			return true
		},
	}
	if err = system.CreateEmulator(); err != nil {
		t.Fatal(err)
	}

	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			var err error

			// instantiate the Game instance for testing:
			g := NewGame(rom)
			g.IsCreated = true
			g.GameName = "ALTTP"
			g.PlayerColor = 0x12ef
			g.SyncItems = true
			g.SyncDungeonItems = true
			g.SyncProgress = true
			g.SyncHearts = true
			g.SyncSmallKeys = true
			g.SyncUnderworld = true
			g.SyncOverworld = true
			g.SyncChests = true
			g.SyncTunicColor = false

			// make all JSL destinations contain a single RTL instruction:
			g.fillRomFunctions()
			for _, addr := range g.romFunctions {
				// 0x6B RTL
				rom.Contents[rom.BusAddressToPC(addr)] = 0x6B
			}

			g.Reset()

			// subscribe to front-end Notifications from the game:
			lastNotification := ""
			g.Notifications.Subscribe(interfaces.ObserverImpl(func(object interface{}) {
				lastNotification = object.(string)
				t.Logf("notify: '%s'", lastNotification)
			}))

			// set logger for system emulator to this specific test:
			system.Logger = &testingLogger{t}

			// reset memory:
			for i := range system.WRAM {
				system.WRAM[i] = 0x00
			}
			for i := range system.SRAM {
				system.SRAM[i] = 0x00
			}

			// copy ROM contents into system emulator:
			copy(system.ROM[:], g.rom.Contents)

			// setup patch code in emulator SRAM:
			if err = system.SetupPatch(); err != nil {
				t.Fatal(err)
			}

			// default module/submodule:
			system.WRAM[0x10] = 0x09 // overworld module
			system.WRAM[0x11] = 0x00 // player in control

			// set up SRAM per each player:
			g.players[1].IndexF = 1
			g.players[1].Ttl = 255
			g.players[1].NameF = "remote"
			for _, sram := range tt.sram {
				system.WRAM[0xF000+sram.offset] = sram.localValue
				g.local.SRAM[sram.offset] = sram.localValue
				g.players[1].SRAM[sram.offset] = sram.remoteValue
			}

			a := &asm.Emitter{
				Code: &bytes.Buffer{},
				Text: &strings.Builder{},
			}
			// default to 8-bit:
			a.AssumeSEP(0x30)
			updated := g.generateSRAMRoutine(a, 0x707C00)
			if updated != tt.wantUpdated {
				t.Logf("%s", a.Text.String())
				t.Errorf("generateUpdateAsm() = %v, want %v", updated, tt.wantUpdated)
				return
			}

			// only run the ASM if it is generated:
			if updated {
				a.Comment("restore 8-bit mode and return to RESET code:")
				a.SEP(0x30)
				a.RTS()

				aw := util.ArrayWriter{Buffer: system.SRAM[0x7C00:]}
				if _, err = a.Code.WriteTo(&aw); err != nil {
					t.Fatal(err)
				}

				// run the CPU until it either runs away or hits the expected stopping point in the ROM code:
				// see the emulator.MakeTestROM() function for details
				system.CPU.Reset()
				if !system.RunUntil(0x00_8006, 0x1_000) {
					t.Errorf("CPU ran too long and did not reach PC=0x008006; actual=%#06x", system.CPU.PC)
					return
				}
			}

			// copy SRAM shadow in WRAM into local player copy:
			copy(g.local.SRAM[:], system.WRAM[0xF000:])

			// verify SRAM values:
			for _, sram := range tt.sram {
				if actual, expected := system.WRAM[0xF000+sram.offset], sram.expectedValue; actual != expected {
					t.Errorf("local.SRAM[%#04X] = %02X, expected %02X", sram.offset, actual, expected)
				}
			}

			// call generateUpdateAsm() again for next frame to receive notifications:
			a.Code = &bytes.Buffer{}
			_ = g.generateUpdateAsm(a)

			if lastNotification != tt.wantNotification {
				t.Errorf("notification = '%s', expected '%s'", lastNotification, tt.wantNotification)
			}

			// call custom verify function for test:
			if tt.verify != nil {
				tt.verify(t, g, &system, tt)
			}
		})
	}
}
