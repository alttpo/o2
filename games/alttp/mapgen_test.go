package alttp

import (
	"fmt"
	"github.com/alttpo/snes/emulator/bus"
	"github.com/alttpo/snes/emulator/cpu65c816"
	"github.com/alttpo/snes/emulator/memory"
	"io"
	"o2/snes"
	"os"
	"testing"
)

func TestGenerateMap(t *testing.T) {
	var err error

	var s *System
	var rom *snes.ROM

	// create the CPU-only SNES emulator:
	s = &System{
		Logger:    os.Stdout,
		LoggerCPU: nil,
	}
	if err = s.CreateEmulator(); err != nil {
		t.Fatal(err)
	}

	var f *os.File
	f, err = os.Open("alttp-jp.smc")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Read(s.ROM[:])
	if err != nil {
		t.Fatal(err)
	}
	err = f.Close()
	if err != nil {
		t.Fatal(err)
	}

	rom, err = snes.NewROM("alttp-jp.smc", s.ROM[:])
	if err != nil {
		t.Fatal(err)
	}

	_ = rom

	s.CPU.Reset()
	if stopPC, expectedPC, cycles := s.RunUntil(0x00_8029, 0x1_000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		t.Fatal(err)
	}
	//#_008029: JSR Sound_LoadIntroSongBank		// skip this

	s.SetPC(0x00_802C)
	//#_00802C: JSR Startup_InitializeMemory
	if stopPC, expectedPC, cycles := s.RunUntil(0x00_802F, 0x10_000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		t.Fatal(err)
	}

	// general world state:
	s.WRAM[0xF3C5] = 0x02 // not raining
	s.WRAM[0xF3C6] = 0x10 // no bed cutscene

	// prepare to call the underworld room load module:
	s.WRAM[0x0010] = 0x06 // Module06_UnderworldLoad
	s.WRAM[0x040C] = 0x00 // dungeon ID ($FF = cave)
	s.WRAM[0x010E] = 0x00 // dungeon entrance ID
	s.WRAM[0x00A0] = 0x00 // supertile (lo)
	s.WRAM[0x00A1] = 0x00 // supertile (hi)

	s.SetPC(0x00_8056)
	//#_008056: JSL Module_MainRouting
	if stopPC, expectedPC, cycles := s.RunUntil(0x00_805A, 0x1000_0000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		t.Fatal(err)
	}
	//#_028174: JSR Underworld_LoadAndDrawRoom
	//#_028177: JSL Underworld_LoadCustomTileAttributes
}

type System struct {
	// emulated system:
	Bus *bus.Bus
	CPU *cpu65c816.CPU

	ROM  [0x1000000]byte
	WRAM [0x20000]byte
	SRAM [0x10000]byte

	Logger    io.Writer
	LoggerCPU io.Writer
}

func (s *System) CreateEmulator() (err error) {
	// create primary A bus for SNES:
	s.Bus, _ = bus.NewWithSizeHint(0x40*2 + 0x10*2 + 1 + 0x70 + 0x80 + 0x70*2)
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
		hwio := &HWIO{s: s}
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

func (s *System) SetPC(pc uint32) {
	s.CPU.RK = byte(pc >> 16)
	s.CPU.PC = uint16(pc & 0xFFFF)
}

func (s *System) GetPC() uint32 {
	return uint32(s.CPU.RK)<<16 | uint32(s.CPU.PC)
}

func (s *System) RunUntil(targetPC uint32, maxCycles uint64) (stopPC uint32, expectedPC uint32, cycles uint64) {
	expectedPC = targetPC
	for cycles = uint64(0); cycles < maxCycles; {
		if s.LoggerCPU != nil {
			s.CPU.DisassembleCurrentPC(s.LoggerCPU)
		}
		if s.GetPC() == targetPC {
			break
		}
		nCycles, _ := s.CPU.Step()
		cycles += uint64(nCycles)
	}

	stopPC = s.GetPC()
	return
}

type HWIO struct {
	s   *System
	mem [0x10000]uint8
}

func (v *HWIO) Read(address uint32) byte {
	offs := address & 0xFFFF
	value := v.mem[offs]
	if v.s.Logger != nil {
		fmt.Fprintf(v.s.Logger, "hwio[$%04x] -> $%02x\n", offs, value)
	}
	return value
}

func (v *HWIO) Write(address uint32, value byte) {
	offs := address & 0xFFFF
	switch offs {
	case 0x4200:
	case 0x4200:
		break
	default:
		if v.s.Logger != nil {
			fmt.Fprintf(v.s.Logger, "hwio[$%04x] <- $%02x\n", offs, value)
		}
		v.mem[offs] = value
	}
}

func (v *HWIO) Shutdown() {
}

func (v *HWIO) Size() uint32 {
	return 0x10000
}

func (v *HWIO) Clear() {
}

func (v *HWIO) Dump(address uint32) []byte {
	return nil
}
