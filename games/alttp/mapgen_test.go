package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator/bus"
	"github.com/alttpo/snes/emulator/cpu65c816"
	"github.com/alttpo/snes/emulator/memory"
	"github.com/alttpo/snes/mapping/lorom"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/ioutil"
	"o2/snes"
	"os"
	"sync"
	"testing"
	"unsafe"
)

func newEmitterAt(s *System, addr uint32, generateText bool) *asm.Emitter {
	lin, _ := lorom.BusAddressToPak(addr)
	a := asm.NewEmitter(s.ROM[lin:], generateText)
	a.SetBase(addr)
	return a
}

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

	// patch in end of bank $02 a tiny routine to load a specific underworld room not by
	// dungeon entrance (which overwrites $A0) but rather a direct supertile (from $A0)
	a := newEmitterAt(s, 0x02_FFC7, true)
	a.SEP(0x30)
	a.LDA_imm8_b(0x00)
	a.PHA()
	a.PLB()
	a.JSR_abs(0xD854) // inside Underworld_LoadEntrance_DoPotsBlocksTorches
	a.JMP_abs_imm16_w(0x8157)
	a.WriteTextTo(s.Logger)

	// skip over music & sfx loading since we did not implement APU registers:
	a = newEmitterAt(s, 0x02_8293, true)
	//#_028293: JSR Underworld_LoadSongBankIfNeeded
	a.JMP_abs_imm16_w(0x82BC)
	//.exit
	//#_0282BC: SEP #$20
	//
	//#_0282BE: RTL
	a.WriteTextTo(s.Logger)

	a = newEmitterAt(s, 0x02_C300, true)
	// NOP out the VRAM upload for tilemaps:
	//.next_quadrant
	//#_02C300: JSL TileMapPrep_NotWaterOnTag
	a.BRA_imm8(0x15)
	//#_02C304: JSL NMI_UploadTilemap_long

	//#_02C308: JSL Underworld_PrepareNextRoomQuadrantUpload
	//#_02C30C: JSL NMI_UploadTilemap_long

	//#_02C310: LDA.w $045C
	//#_02C313: CMP.b #$10
	//#_02C315: BNE .next_quadrant
	a.WriteTextTo(s.Logger)

	// patch out the pot-clearing loop:
	a = newEmitterAt(s, 0x02_D894, true)
	//#_02D894: LDX.b #$3E
	a.SEP(0x30)
	a.PLB()
	a.RTS()
	a.WriteTextTo(s.Logger)

	// patch $00:8056 to JSL to our new routine:
	a = newEmitterAt(s, 0x00_8056, true)
	//#_008056: JSL Module_MainRouting
	a.JSL(0x02_FFC7)
	a.WriteTextTo(s.Logger)

	// patch out LoadCommonSprites:
	a = newEmitterAt(s, 0x00_E6F7, true)
	a.RTS()
	a.WriteTextTo(s.Logger)

	// patch out LoadSpriteGraphics:
	a = newEmitterAt(s, 0x00_E5C3, true)
	a.RTS()
	a.WriteTextTo(s.Logger)

	// patch out RebuildHUD:
	a = newEmitterAt(s, 0x0D_FA88, true)
	//RebuildHUD_Keys:
	//	#_0DFA88: STA.l $7EF36F
	a.RTL()
	a.WriteTextTo(s.Logger)

	//s.LoggerCPU = os.Stdout

	// initialize game:
	s.CPU.Reset()
	if stopPC, expectedPC, cycles := s.RunUntil(0x00_8029, 0x1_000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		t.Fatal(err)
	}
	//#_008029: JSR Sound_LoadIntroSongBank		// skip this
	//s.SetPC(0x00_802C)
	////#_00802C: JSR Startup_InitializeMemory
	//if stopPC, expectedPC, cycles := s.RunUntil(0x00_802F, 0x10_000); stopPC != expectedPC {
	//	err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
	//	t.Fatal(err)
	//}

	// general world state:
	s.WRAM[0xF3C5] = 0x02 // not raining
	s.WRAM[0xF3C6] = 0x10 // no bed cutscene

	// prepare to call the underworld room load module:
	s.WRAM[0x10] = 0x07
	s.WRAM[0x11] = 0x00
	s.WRAM[0xB0] = 0x00

	s.WRAM[0x040C] = 0x00 // dungeon ID ($FF = cave)
	s.WRAM[0x010E] = 0x00 // dungeon entrance ID

	patchedTileset := false
	dumpedAsm := false

	vram := make([]byte, 65536)

	wg := sync.WaitGroup{}
	// supertile:
	for supertile := uint16(0); supertile < 0x128; supertile++ {
		binary.LittleEndian.PutUint16(s.WRAM[0xA0:0xA2], supertile)

		s.SetPC(0x00_8056)
		if stopPC, expectedPC, cycles := s.RunUntil(0x00_805A, 0x1000_0000); stopPC != expectedPC {
			err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
			t.Fatal(err)
		}

		// output is:
		//  s.VRAM: $4000[0x2000] = 4bpp tile graphics
		//  s.WRAM: $2000[0x2000] = BG1 1024x1024 tile map, [64][64]uint16
		//  s.WRAM: $4000[0x2000] = BG2 1024x1024 tile map, [64][64]uint16
		//  s.WRAM: $C300[0x0200] = CGRAM palette

		if dumpedAsm {
			s.LoggerCPU = nil
		}

		// after first run, prevent further tileset updates to VRAM:
		if !patchedTileset {
			a = newEmitterAt(s, 0x00_E1DB, true)
			//InitializeTilesets:
			//	#_00E1DB: PHB
			a.RTL()
			a.WriteTextTo(s.Logger)

			// patch out decompress tiles:
			a = newEmitterAt(s, 0x02_8183, true)
			//#_028183: JSL DecompressAnimatedUnderworldTiles
			a.NOP()
			a.NOP()
			a.NOP()
			a.NOP()
			a.WriteTextTo(s.Logger)

			a = newEmitterAt(s, 0x02_8199, true)
			//#_028199: JSR Underworld_LoadPalettes
			a.NOP()
			a.NOP()
			a.NOP()
			a.WriteTextTo(s.Logger)

			a = newEmitterAt(s, 0x02_824E, true)
			//#_02824E: JSL Follower_Initialize
			a.NOP()
			a.NOP()
			a.NOP()
			a.NOP()
			//#_028252: JSL Sprite_ResetAll
			a.NOP()
			a.NOP()
			a.NOP()
			a.NOP()
			//#_028256: JSL Underworld_ResetSprites
			a.NOP()
			a.NOP()
			a.NOP()
			a.NOP()
			a.WriteTextTo(s.Logger)

			patchedTileset = true
			if !dumpedAsm {
				//s.LoggerCPU = os.Stdout
				dumpedAsm = true
			}

			// dump VRAM only once:
			copy(vram, (*(*[65536]byte)(unsafe.Pointer(&s.VRAM[0])))[:])
			ioutil.WriteFile(fmt.Sprintf("data/r%03X.vram", supertile), vram, 0644)
		}

		// dump WRAM for each supertile:
		wram := make([]byte, 131072)
		copy(wram, s.WRAM[:])
		wg.Add(1)
		go func(st uint16, wram []byte) {
			ioutil.WriteFile(fmt.Sprintf("data/r%03X.wram", st), wram, 0644)
			wg.Done()
		}(supertile, wram)

		// render image:
		cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
		pal := cgramToPalette(cgram)
		g := image.NewPaletted(image.Rect(0, 0, 1024, 1024), pal)
		renderBG(
			g,
			(*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x2000])))[:],
			s.VRAM[0x2000:0x4000],
		)
		//(*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x4000])))[:],
		//s.VRAM[0x2000:0x4000],
		{
			// export to PNG:
			var po *os.File
			po, err = os.OpenFile(fmt.Sprintf("data/r%03X.png", supertile), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				panic(err)
			}
			err = png.Encode(po, g)
			if err != nil {
				panic(err)
			}
			err = po.Close()
			if err != nil {
				panic(err)
			}
		}
		break
	}

	wg.Wait()
}

func cgramToPalette(cgram []uint16) color.Palette {
	pal := make(color.Palette, 256)
	for i, bgr15 := range cgram {
		// convert BGR15 color format (MSB unused) to RGB24:
		b := (bgr15 & 0xF800) >> 10
		g := (bgr15 & 0x03E0) >> 5
		r := bgr15 & 0x001F
		pal[i] = color.NRGBA{
			R: uint8(r<<3 | r>>2),
			G: uint8(g<<3 | g>>2),
			B: uint8(b<<3 | b>>2),
			A: 0xff,
		}
	}
	return pal
}

func renderBG(g *image.Paletted, bg []uint16, tiles []uint16) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			//High     Low          Legend->  c: Starting character (tile) number
			//vhopppcc cccccccc               h: horizontal flip  v: vertical flip
			//                                p: palette number   o: priority bit
			z := bg[a]

			// TODO: h and v
			p := byte((z & 7) >> 10)
			c := int(z & 0x03FF)
			for y := 0; y < 8; y++ {
				p01 := tiles[(c<<4)+(y<<1)]
				p23 := tiles[(c<<4)+(y<<1)+16]
				for x := 0; x < 8; x++ {
					i := byte(p01&(1<<x)) | byte(p01&(1<<(x+8))>>7) |
						byte(p23&(1<<x)<<2) | byte(p23&(1<<(x+8))>>6)

					// transparency:
					if i == 0 {
						continue
					}

					g.SetColorIndex(tx<<3+x, ty<<3+y, p+i)
				}
			}

			a++
		}
	}
}

type System struct {
	// emulated system:
	Bus *bus.Bus
	CPU *cpu65c816.CPU

	ROM  [0x1000000]byte
	WRAM [0x20000]byte
	SRAM [0x10000]byte

	VRAM [0x8000]uint16

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

type DMAChannel struct{}

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

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "DMA[%d] start\n", ch)
	//}

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
		panic("PPU -> CPU DMA transfer not supported!")
	}

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "DMA[%d] stop\n", ch)
	//}
}

type HWIO struct {
	s   *System
	mem [0x10000]uint8

	dmaregs [8]DMARegs
	dma     [8]DMAChannel

	ppu struct {
		incrMode      bool   // false = increment after $2118, true = increment after $2119
		incrAmt       uint32 // 1, 32, or 128
		addrRemapping byte
		addr          uint32
		data          uint16
	}
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

	if offs == 0x4200 {
		// NMITIMEN
		return
	}

	if offs == 0x420b {
		// MDMAEN:
		hdmaen := value
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA start\n", offs, hdmaen)
		//}
		// execute DMA transfers from channels 0..7 in order:
		for c := range h.dma {
			if hdmaen&(1<<c) == 0 {
				continue
			}

			// channel enabled:
			h.dma[c].Transfer(&h.dmaregs[c], c, h)
		}
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA end\n", offs, hdmaen)
		//}
		return
	}
	if offs == 0x420c {
		// HDMAEN:
		// no HDMA support
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x HDMA ignored\n", offs, value)
		//}
		return
	}
	if offs&0xFF00 == 0x4300 {
		// DMA registers:
		ch := offs & 0x00F0 >> 8
		if ch <= 7 {
			reg := offs & 0x000F
			if reg <= 6 {
				h.dmaregs[ch][reg] = value
			}
		}

		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA register\n", offs, value)
		//}
		return
	}

	if offs == 0x2100 {
		// INIDISP
		return
	}
	if offs == 0x212e || offs == 0x212f {
		// TMW, TSW
		return
	}

	// PPU:
	if offs == 0x2115 {
		// VMAIN = o---mmii
		h.ppu.incrMode = value&0x80 != 0
		switch value & 3 {
		case 0:
			h.ppu.incrAmt = 1
			break
		case 1:
			h.ppu.incrAmt = 32
			break
		default:
			h.ppu.incrAmt = 128
			break
		}
		h.ppu.addrRemapping = (value & 0x0C) >> 2
		if h.ppu.addrRemapping != 0 {
			panic(fmt.Errorf("unsupported VRAM address remapping mode %d", h.ppu.addrRemapping))
		}
		return
	}
	if offs == 0x2116 {
		// VMADDL
		h.ppu.addr = uint32(value) | h.ppu.addr&0xFF00
		return
	}
	if offs == 0x2117 {
		// VMADDH
		h.ppu.addr = uint32(value)<<8 | h.ppu.addr&0x00FF
		return
	}
	if offs == 0x2118 {
		// VMDATAL
		h.ppu.data = uint16(value) | h.ppu.data&0xFF00
		h.s.VRAM[h.ppu.addr] = h.ppu.data
		if h.ppu.incrMode == false {
			h.ppu.addr += h.ppu.incrAmt
		}
		return
	}
	if offs == 0x2119 {
		// VMDATAH
		h.ppu.data = uint16(value)<<8 | h.ppu.data&0x00FF
		h.s.VRAM[h.ppu.addr] = h.ppu.data
		if h.ppu.incrMode == true {
			h.ppu.addr += h.ppu.incrAmt
		}
		return
	}

	// APU:
	if offs >= 0x2140 && offs <= 0x2143 {
		// APUIO0 .. APUIO3
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
