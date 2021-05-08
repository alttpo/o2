package emulator

import (
	"bytes"
	"io"
	"o2/snes"
	"o2/snes/asm"
	"o2/snes/emulator/bus"
	"o2/snes/emulator/cpu65c816"
	"o2/snes/emulator/memory"
	"o2/util"
)

type System struct {
	// emulated system:
	Bus *bus.Bus
	CPU *cpu65c816.CPU

	ROM  [0x1000000]byte
	WRAM [0x20000]byte
	SRAM [0x10000]byte

	Logger       io.StringWriter
	ShouldLogCPU func(s *System) bool
}

func (s *System) CreateEmulator() (err error) {
	// create primary A bus for SNES:
	s.Bus, _ = bus.New()
	// Create CPU:
	s.CPU, _ = cpu65c816.New(s.Bus)

	// map in ROM to Bus; parts of this mapping will be overwritten:
	for b := uint32(0); b < 0x40; b++ {
		halfBank := b << 15
		bank := b << 16
		err = s.Bus.Attach(
			memory.NewRAM(s.ROM[halfBank:halfBank+0x8000], bank|0x8000),
			"rom",
			bank|0x8000,
			bank|0xFFFF,
		)
		if err != nil {
			return
		}

		// mirror:
		err = s.Bus.Attach(
			memory.NewRAM(s.ROM[halfBank:halfBank+0x8000], (bank+0x80_0000)|0x8000),
			"rom",
			(bank+0x80_0000)|0x8000,
			(bank+0x80_0000)|0xFFFF,
		)
		if err != nil {
			return
		}
	}

	// SRAM (banks 70-7D,F0-FF) (7E,7F) will be overwritten with WRAM:
	for b := uint32(0); b < uint32(len(s.SRAM)>>15); b++ {
		bank := b << 16
		halfBank := b << 15
		err = s.Bus.Attach(
			memory.NewRAM(s.SRAM[halfBank:halfBank+0x8000], bank+0x70_0000),
			"sram",
			bank+0x70_0000,
			bank+0x70_7FFF,
		)
		if err != nil {
			return
		}

		// mirror:
		err = s.Bus.Attach(
			memory.NewRAM(s.SRAM[halfBank:halfBank+0x8000], bank+0xF0_0000),
			"sram",
			bank+0xF0_0000,
			bank+0xF0_7FFF,
		)
		if err != nil {
			return
		}
	}

	// WRAM:
	{
		err = s.Bus.Attach(
			memory.NewRAM(s.WRAM[0:0x20000], 0x7E0000),
			"wram",
			0x7E_0000,
			0x7F_FFFF,
		)
		if err != nil {
			return
		}

		// map in first $2000 of each bank as a mirror of WRAM:
		for b := uint32(0); b < 0x70; b++ {
			bank := b << 16
			err = s.Bus.Attach(
				memory.NewRAM(s.WRAM[0:0x2000], bank),
				"wram",
				bank,
				bank|0x1FFF,
			)
			if err != nil {
				return
			}
		}
		for b := uint32(0x80); b < 0x100; b++ {
			bank := b << 16
			err = s.Bus.Attach(
				memory.NewRAM(s.WRAM[0:0x2000], bank),
				"wram",
				bank,
				bank|0x1FFF,
			)
			if err != nil {
				return
			}
		}
	}

	// Memory-mapped IO registers:
	{
		hwio := &memory.FakeHW{}
		for b := uint32(0); b < 0x70; b++ {
			bank := b << 16
			err = s.Bus.Attach(
				hwio,
				"hwio",
				bank|0x2000,
				bank|0x7FFF,
			)
			if err != nil {
				return
			}

			bank = (b + 0x80) << 16
			err = s.Bus.Attach(
				hwio,
				"hwio",
				bank|0x2000,
				bank|0x7FFF,
			)
			if err != nil {
				return
			}
		}
	}

	return
}

func MakeTestROM(title string) (rom *snes.ROM, err error) {
	var b [0x1_0000]byte

	a := asm.Emitter{
		Code: &bytes.Buffer{},
		Text: nil,
	}
	// this is the RESET vector:
	a.SetBase(0x00_8000)
	a.Comment("switch to 8-bit mode and JSL to $70:7FFA")
	a.SEP(0x30)
	a.JSL(0x70_7FFA)
	a.Comment("this is our stopping point at $00:8006:")
	a.BRA(-5)

	// copy asm into ROM:
	aw := util.ArrayWriter{Buffer: b[0x0000:0x7FFF]}
	_, err = a.Code.WriteTo(&aw)
	if err != nil {
		return
	}

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

	return
}

func (s *System) SetupPatch() (err error) {
	a := asm.Emitter{}
	// entry point at 0x70_7FFA
	a.Code = &bytes.Buffer{}
	a.SetBase(0x70_7FFA)
	a.JSR_abs(0x7C00)
	a.RTL()

	aw := util.ArrayWriter{Buffer: s.SRAM[0x7FFA:0x7FFF]}
	_, err = a.Code.WriteTo(&aw)
	if err != nil {
		return
	}

	// assemble the RTS instructions at the two A/B update routine locations:
	a.Code = &bytes.Buffer{}
	a.SetBase(0x70_7C00)
	a.RTS()

	aw = util.ArrayWriter{Buffer: s.SRAM[0x7C00:0x7FFF]}
	_, err = a.Code.WriteTo(&aw)
	if err != nil {
		return
	}

	a.Code = &bytes.Buffer{}
	a.SetBase(0x70_7E00)
	a.RTS()

	aw = util.ArrayWriter{Buffer: s.SRAM[0x7E00:0x7FFF]}
	_, err = a.Code.WriteTo(&aw)
	if err != nil {
		return
	}

	return
}

func (s *System) SetPC(pc uint32) {
	s.CPU.RK = byte(pc >> 16)
	s.CPU.PC = uint16(pc & 0xFFFF)
}

func (s *System) GetPC() uint32 {
	return uint32(s.CPU.RK)<<16 | uint32(s.CPU.PC)
}

func (s *System) RunUntil(targetPC uint32, maxCycles uint64) bool {
	shouldLog := s.ShouldLogCPU
	for cycles := uint64(0); cycles < maxCycles; {
		if shouldLog != nil && shouldLog(s) {
			_, _ = s.Logger.WriteString(s.CPU.DisassembleCurrentPC())
		}
		if s.GetPC() == targetPC {
			break
		}
		nCycles, _ := s.CPU.Step()
		cycles += uint64(nCycles)
	}

	return s.GetPC() == targetPC
}
