package alttp

import (
	"fmt"
	"log"
	"o2/snes"
	"o2/snes/asm"
	"testing"
)

const testROMBreakPoint = 0x00_8100

func MakeTestROM(title string) (rom *snes.ROM, err error) {
	// 1 MiB size matches ALTTP JP 1.0 ROM size
	var b [0x10_0000]byte

	rom, err = snes.NewROM("test.sfc", b[:])
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

	// fill RESET vector with NOPs:
	a := asm.NewEmitter(true)
	a.SetBase(0x00_8000)
	a.SEP(0x30)
	a.BRA(0x2F - 0x04)
	_, err = a.WriteTo(rom.BusWriter(0x00_8000))
	log.Print(a.Text.String())
	if err != nil {
		err = fmt.Errorf("Emitter.WriteTo(0x00_8000) error = %w", err)
		return
	}

	// write the $802F code that will be patched over:
	a = asm.NewEmitter(true)
	a.SetBase(0x00_802F)
	a.AssumeSEP(0x30)
	a.LDA_imm8_b(0x81)
	a.STA_abs(0x4200)
	a.BRA(0x56 - 0x34 - 2)
	_, err = a.WriteTo(rom.BusWriter(0x00_802F))
	log.Print(a.Text.String())
	if err != nil {
		err = fmt.Errorf("Emitter.WriteTo(0x00_802F) error = %w", err)
		return
	}

	// write the $8056 code that will be patched over:
	a = asm.NewEmitter(true)
	a.SetBase(0x00_8056)
	a.AssumeSEP(0x30)
	a.JSL(testROMBreakPoint)
	_, err = a.WriteTo(rom.BusWriter(0x00_8056))
	log.Print(a.Text.String())
	if err != nil {
		err = fmt.Errorf("Emitter.WriteTo(0x00_8056) error = %w", err)
		return
	}

	return
}

func TestPatcher_Patch(t *testing.T) {
	var rom, err = MakeTestROM("ZELDANODENSETSU")
	if err != nil {
		t.Errorf("MakeTestROM() error = %v", err)
	}

	p := NewPatcher(rom)
	if err = p.Patch(); err != nil {
		t.Errorf("Patch() error = %v", err)
	}
}
