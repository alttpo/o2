package alttp

import (
	"fmt"
	"o2/snes"
	"o2/snes/emulator"
	"o2/snes/lorom"
	"testing"
)

type testingLogger struct {
	t *testing.T
}

func (l *testingLogger) WriteString(s string) (n int, err error) {
	l.t.Log(s)
	return len(s), nil
}

func CreateTestEmulator(t *testing.T, romTitle string) (system *emulator.System, rom *snes.ROM, err error) {
	// create the CPU-only SNES emulator:
	system = &emulator.System{
		Logger: &testingLogger{t},
		ShouldLogCPU: func(s *emulator.System) bool {
			return true
		},
	}
	if err = system.CreateEmulator(); err != nil {
		return
	}

	// create a ROM for testing our patch process and the generated ASM code:
	rom, err = MakeTestROM(romTitle, system.ROM[:])
	if err != nil {
		return
	}

	p := NewPatcher(rom)
	if err = p.Patch(); err != nil {
		return
	}

	// run the initialization code to set up SRAM:
	system.CPU.Reset()
	if !system.RunUntil(0x00_8033, 0x1_000) {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", 0x00_8033, system.CPU.PC)
		return
	}

	return
}

func CreateTestGame(t *testing.T, rom *snes.ROM, system *emulator.System) *Game {
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

	// set logger for system emulator to this specific test:
	system.Logger = &testingLogger{t}

	// reset memory:
	for i := range system.WRAM {
		system.WRAM[i] = 0x00
		g.wram[i] = 0x00
	}
	// cannot reset SRAM here because of the setup code executed in CreateTestEmulator

	return g
}
