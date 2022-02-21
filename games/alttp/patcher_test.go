package alttp

import (
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"log"
	"o2/snes"
	"testing"
)

const testROMMainGameLoop = 0x00_8034
const testROMDoFrame = 0x00_8051
const testROMJSLMainRouting = 0x00_8056
const testROMBreakPoint = 0x00_8100

func MakeTestROM(title string, b []byte) (rom *snes.ROM, err error) {
	rom, err = snes.NewROM("test.sfc", b)
	if err != nil {
		return
	}

	// build ROM header:
	copy(rom.Header.Title[:], title)
	rom.EmulatedVectors.RESET = 0x8000
	rom.Header.RAMSize = 5 // 1024 << 5 = 32768 bytes

	err = rom.WriteHeader()
	if err != nil {
		return
	}

	// write RESET vector:
	var pakAddr uint32
	pakAddr, err = lorom.BusAddressToPak(0x00_8000)
	a := asm.NewEmitter(rom.Slice(pakAddr, 0x2F), true)
	a.SetBase(0x00_8000)
	a.SEP(0x30)
	a.BRA_imm8(0x2F - 0x04)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// write the $802F code that will be patched over:
	pakAddr, err = lorom.BusAddressToPak(0x00_802F)
	a = asm.NewEmitter(rom.Slice(pakAddr, 0x50), true)
	a.SetBase(0x00_802F)
	a.AssumeSEP(0x30)
	a.LDA_imm8_b(0x81)
	a.STA_abs(0x4200)
	a.BRA_imm8(0x56 - 0x34 - 2)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// write the $8034 code as main game loop:
	pakAddr, err = lorom.BusAddressToPak(testROMMainGameLoop)
	a = asm.NewEmitter(rom.Slice(pakAddr, 0x30), true)
	a.SetBase(testROMMainGameLoop)
	a.AssumeSEP(0x30)
	a.BRA("do_frame")
	for a.PC() < testROMDoFrame {
		a.NOP()
	}
	if a.Label("do_frame") != testROMDoFrame {
		panic(fmt.Errorf("do_frame label must be at %#06x", testROMDoFrame))
	}
	// increment frame counter:
	a.INC_dp(0x1A)
	for a.PC() < testROMJSLMainRouting {
		a.NOP()
	}
	a.JSL(testROMBreakPoint)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	return
}

func TestPatcher_Patch(t *testing.T) {
	// 1 MiB size matches ALTTP JP 1.0 ROM size
	var b [0x10_0000]byte

	var rom, err = MakeTestROM("ZELDANODENSETSU", b[:])
	if err != nil {
		t.Errorf("MakeTestROM() error = %v", err)
	}

	p := NewPatcher(rom)
	if err = p.Patch(); err != nil {
		t.Errorf("Patch() error = %v", err)
	}
}
