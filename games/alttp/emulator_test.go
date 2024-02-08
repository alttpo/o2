package alttp

import (
	"fmt"
	"github.com/alttpo/snes/emulator"
	"github.com/alttpo/snes/mapping/lorom"
	"io"
	"log"
	"o2/snes"
	"o2/util"
	"testing"
)

func setupTestLogger(t *testing.T) (logger *testLogger) {
	logger = &testLogger{}
	logger.b.Grow(20000)
	originalLogger := log.Writer()
	t.Cleanup(func() {
		t.Log()
		logger.WriteTo(originalLogger)
		log.SetOutput(originalLogger)
	})
	log.SetOutput(logger)

	return
}

func createTestEmulator(romTitle string, logger io.Writer) (system *emulator.System, rom *snes.ROM, err error) {
	// create the CPU-only SNES emulator:
	system = &emulator.System{
		Logger: logger,
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
	if !system.RunUntil(0x00_8034, 0x1_000) {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", 0x00_8034, system.CPU.PC)
		return
	}

	// setup the required ROM lookup table for the patcher's module check:
	{
		mask := uint16(1)
		for i := 0; i < 16; i++ {
			system.Bus.EaWrite(uint32(0x00_98C0+((15-i)<<1)+0), byte(mask&0xFF))
			system.Bus.EaWrite(uint32(0x00_98C0+((15-i)<<1)+1), byte(mask>>8&0xFF))
			mask <<= 1
		}
	}

	return
}

func CreateTestEmulator(romTitle string, t testing.TB) (system *emulator.System, rom *snes.ROM, err error) {
	return createTestEmulator(
		romTitle,
		&util.CommitLogger{Committer: func(p []byte) {
			log.Writer().Write(p)
		}},
	)
}

func CreateTestGame(rom *snes.ROM, system *emulator.System) *Game {
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
		pakAddr, _ := lorom.BusAddressToPak(addr)
		system.ROM[pakAddr] = 0x6B
	}

	g.Reset()

	g.ProvideQueue(&testQueue{E: system})

	return g
}
