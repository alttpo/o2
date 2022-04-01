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

	var f *os.File
	f, err = os.Open("alttp-jp.smc")
	if err != nil {
		t.Skip(err)
	}

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

	VRAM [0x10000]byte

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

type DMARegs [7]byte

func (c *DMARegs) ctrl() byte { return c[0] }
func (c *DMARegs) dest() byte { return c[1] }
func (c *DMARegs) srcL() byte { return c[2] }
func (c *DMARegs) srcH() byte { return c[3] }
func (c *DMARegs) srcB() byte { return c[4] }
func (c *DMARegs) sizL() byte { return c[5] }
func (c *DMARegs) sizH() byte { return c[6] }

type DMAChannel struct {
}

func (c *DMAChannel) Transfer(regs *DMARegs, ch int, h *HWIO) {
	aSrc := uint32(regs.srcB())<<16 | uint32(regs.srcH())<<8 | uint32(regs.srcL())
	siz := uint32(regs.sizH())<<8 | uint32(regs.sizL())
	if siz == 0 {
		siz = 0x10000
	}

	bDest := regs.dest()

	incr := regs.ctrl()&0x10 == 0
	fixed := regs.ctrl()&0x08 != 0
	mode := regs.ctrl() & 7

	if h.s.Logger != nil {
		fmt.Fprintf(h.s.Logger, "DMA[%d] start\n", ch)
	}

	if regs.ctrl()&0x80 == 0 {
		// CPU -> PPU
	copyloop:
		for siz > 0 {
			switch mode {
			case 0:
				h.Write(uint32(bDest)|0x2100, h.s.Bus.EaRead(aSrc))
				if !fixed {
					if incr {
						aSrc = ((aSrc&0xFFFF)+1)&0xFFFF + aSrc&0xFF0000
					} else {
						aSrc = ((aSrc&0xFFFF)-1)&0xFFFF + aSrc&0xFF0000
					}
				}
				siz--
				if siz == 0 {
					break copyloop
				}
				break
			case 1:
				// p
				h.Write(uint32(bDest)|0x2100, h.s.Bus.EaRead(aSrc))
				if !fixed {
					if incr {
						aSrc = ((aSrc&0xFFFF)+1)&0xFFFF + aSrc&0xFF0000
					} else {
						aSrc = ((aSrc&0xFFFF)-1)&0xFFFF + aSrc&0xFF0000
					}
				}
				siz--
				if siz == 0 {
					break copyloop
				}
				// p+1
				h.Write(uint32(bDest+1)|0x2100, h.s.Bus.EaRead(aSrc))
				if !fixed {
					if incr {
						aSrc = ((aSrc&0xFFFF)+1)&0xFFFF + aSrc&0xFF0000
					} else {
						aSrc = ((aSrc&0xFFFF)-1)&0xFFFF + aSrc&0xFF0000
					}
				}
				siz--
				if siz == 0 {
					break copyloop
				}
				break
			case 2:
				panic("mode 2!!!")
			case 3:
				panic("mode 3!!!")
			case 4:
				panic("mode 4!!!")
			case 5:
				panic("mode 5!!!")
			case 6:
				panic("mode 6!!!")
			case 7:
				panic("mode 7!!!")
			}
		}
	} else {
		// PPU -> CPU
	}

	if h.s.Logger != nil {
		fmt.Fprintf(h.s.Logger, "DMA[%d] stop\n", ch)
	}
}

type HWIO struct {
	s   *System
	mem [0x10000]uint8

	dmaregs [8]DMARegs
	dma     [8]DMAChannel
}

func (h *HWIO) Read(address uint32) byte {
	offs := address & 0xFFFF
	value := h.mem[offs]
	if h.s.Logger != nil {
		fmt.Fprintf(h.s.Logger, "hwio[$%04x] -> $%02x\n", offs, value)
	}
	return value
}

func (h *HWIO) Write(address uint32, value byte) {
	offs := address & 0xFFFF

	if offs&0xFF00 == 0x4300 {
		// DMA registers:
		ch := offs & 0x00F0 >> 8
		if ch <= 7 {
			reg := offs & 0x000F
			if reg <= 6 {
				h.dmaregs[ch][reg] = value
			}
		}

		if h.s.Logger != nil {
			fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA register\n", offs, value)
		}
		return
	} else if offs == 0x420b {
		// MDMAEN:
		hdmaen := value
		if h.s.Logger != nil {
			fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA start\n", offs, hdmaen)
		}
		// execute DMA transfers from channels 0..7 in order:
		for c := range h.dma {
			if hdmaen&(1<<c) == 0 {
				continue
			}

			// channel enabled:
			h.dma[c].Transfer(&h.dmaregs[c], c, h)
		}
		if h.s.Logger != nil {
			fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA end\n", offs, hdmaen)
		}
		return
	} else if offs == 0x420c {
		// HDMAEN:
		// no HDMA support
		if h.s.Logger != nil {
			fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x HDMA ignored\n", offs, value)
		}
		return
	}

	switch offs {
	default:
		if h.s.Logger != nil {
			fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x\n", offs, value)
		}
		h.mem[offs] = value
	}
}

func (h *HWIO) Shutdown() {
}

func (h *HWIO) Size() uint32 {
	return 0x10000
}

func (h *HWIO) Clear() {
}

func (h *HWIO) Dump(address uint32) []byte {
	return nil
}
