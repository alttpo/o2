package alttp

import (
	"o2/interfaces"
	"o2/snes/asm"
	"o2/snes/emulator"
	"o2/snes/lorom"
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
	rom, err := MakeTestROM(romTitle)
	if err != nil {
		t.Fatal(err)
	}

	p := NewPatcher(rom)
	if err = p.Patch(); err != nil {
		t.Errorf("Patch() error = %v", err)
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

	// copy ROM contents into system emulator:
	copy(system.ROM[:], rom.Contents)

	// run the initialization code to set up SRAM:
	system.CPU.Reset()
	if !system.RunUntil(0x00_8033, 0x1_000) {
		t.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", 0x00_8033, system.CPU.PC)
		return
	}

	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
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
				system.ROM[lorom.BusAddressToPC(addr)] = 0x6B
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
			// cannot reset SRAM here because of the setup code above this loop.

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

			a := asm.NewEmitter(make([]byte, 0x200), true)
			// default to 8-bit:
			a.AssumeSEP(0x30)
			updated := g.generateSRAMRoutine(a, 0x707C00)
			if updated != tt.wantUpdated {
				t.Errorf("generateUpdateAsm() = %v, want %v", updated, tt.wantUpdated)
				return
			}

			// only run the ASM if it is generated:
			if updated {
				// generateSRAMRoutine() takes care of this:
				//a.Comment("restore 8-bit mode and return to RESET code:")
				//a.SEP(0x30)
				//a.RTS()
				//a.WriteTextTo(log.Writer())

				copy(system.SRAM[0x7C00:0x7D00], a.Bytes())

				// run the CPU until it either runs away or hits the expected stopping point in the ROM code:
				system.CPU.Reset()
				system.SetPC(0x00_8056)
				if !system.RunUntil(testROMBreakPoint, 0x1_000) {
					t.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", testROMBreakPoint, system.CPU.PC)
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
			a = asm.NewEmitter(make([]byte, 0x200), false)
			a.SetBase(0x707E00)
			a.AssumeSEP(0x30)
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
