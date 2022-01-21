package alttp

import (
	"log"
	"o2/snes"
	"o2/snes/asm"
	"o2/snes/lorom"
	"testing"
)

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
	a := asm.NewEmitter(rom.Slice(lorom.BusAddressToPC(0x00_8000), 0x2F), true)
	a.SetBase(0x00_8000)
	a.SEP(0x30)
	a.BRA_imm8(0x2F - 0x04)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// write the $802F code that will be patched over:
	a = asm.NewEmitter(rom.Slice(lorom.BusAddressToPC(0x00_802F), 0x50), true)
	a.SetBase(0x00_802F)
	a.AssumeSEP(0x30)
	a.LDA_imm8_b(0x81)
	a.STA_abs(0x4200)
	a.BRA_imm8(0x56 - 0x34 - 2)
	if err = a.Finalize(); err != nil {
		return
	}
	a.WriteTextTo(log.Writer())

	// write the $8056 code that will be patched over:
	a = asm.NewEmitter(rom.Slice(lorom.BusAddressToPC(0x00_8056), 0x50), true)
	a.SetBase(0x00_8056)
	a.AssumeSEP(0x30)
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
