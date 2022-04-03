package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator/bus"
	"github.com/alttpo/snes/emulator/cpu65c816"
	"github.com/alttpo/snes/emulator/memory"
	"github.com/alttpo/snes/mapping/lorom"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	"image/png"
	"io"
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
	f, err = os.Open("alttp-jp.sfc")
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

	rom, err = snes.NewROM("alttp-jp.sfc", s.ROM[:])
	if err != nil {
		t.Fatal(err)
	}
	_ = rom

	var a *asm.Emitter

	// initialize game:
	s.CPU.Reset()
	//#_008029: JSR Sound_LoadIntroSongBank		// skip this
	if stopPC, expectedPC, cycles := s.RunUntil(0x00_8029, 0x1_000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		t.Fatal(err)
	}

	// this is useless zeroing of memory:
	//s.SetPC(0x00_802C)
	////#_00802C: JSR Startup_InitializeMemory
	//if stopPC, expectedPC, cycles := s.RunUntil(0x00_802F, 0x10_000); stopPC != expectedPC {
	//	err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
	//	t.Fatal(err)
	//}

	// patch $00:8027 to be our module routing routine with NMI DMA/VRAM update after:
	a = newEmitterAt(s, 0x00_8027, true)
	a.SEP(0x30)
	a.JSL(0x00_80B5) // MainRouting
	a.REP(0x10)
	a.LDA_imm8_b(0x01)
	a.JSR_abs(0x00_85FC) // NMI_PrepareSprites
	//a.STA_abs(0x0710) // disable sprite updates
	a.JSR_abs(0x89E0) // NMI_DoUpdates
	// 8037:
	a.SEP(0x30)
	a.JSL(0x02_FFC7) // load underworld room
	a.REP(0x10)
	a.LDA_imm8_b(0x01)
	//a.JSR_abs(0x00_85FC) // NMI_PrepareSprites
	a.STA_abs(0x0710) // disable sprite updates
	a.JSR_abs(0x89E0) // NMI_DoUpdates
	// 8047
	a.JSR_abs(0x89E0) // NMI_DoUpdates
	// 804A
	a.WriteTextTo(s.Logger)

	// patch in end of bank $02 a tiny routine to load a specific underworld room not by
	// dungeon entrance (which overwrites $A0) but rather a direct supertile (from $A0)
	a = newEmitterAt(s, 0x02_FFC7, true)
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
	//#_0282BE: RTL
	a.WriteTextTo(s.Logger)

	if false {
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
	}

	if false {
		// patch out the pot-clearing loop:
		a = newEmitterAt(s, 0x02_D894, true)
		//#_02D894: LDX.b #$3E
		a.SEP(0x30)
		a.PLB()
		a.RTS()
		a.WriteTextTo(s.Logger)
	}

	if false {
		// patch out LoadCommonSprites:
		a = newEmitterAt(s, 0x00_E6F7, true)
		a.RTS()
		a.WriteTextTo(s.Logger)
	}

	if false {
		// patch out LoadSpriteGraphics:
		a = newEmitterAt(s, 0x00_E5C3, true)
		a.RTS()
		a.WriteTextTo(s.Logger)
	}

	if true {
		// patch out RebuildHUD:
		a = newEmitterAt(s, 0x0D_FA88, true)
		//RebuildHUD_Keys:
		//	#_0DFA88: STA.l $7EF36F
		a.RTL()
		a.WriteTextTo(s.Logger)
	}

	//s.LoggerCPU = os.Stdout

	// general world state:
	s.WRAM[0xF3C5] = 0x02 // not raining
	s.WRAM[0xF3C6] = 0x10 // no bed cutscene

	// prepare to call the underworld room load module:
	s.WRAM[0x10] = 0x06
	s.WRAM[0x11] = 0x00
	s.WRAM[0xB0] = 0x00

	s.WRAM[0x040C] = 0xFF // dungeon ID ($FF = cave)
	s.WRAM[0x010E] = 0x00 // dungeon entrance ID

	// run module 06 to load underworld and NMI_DoUpdates after it:
	s.SetPC(0x00_8027)
	if stopPC, expectedPC, cycles := s.RunUntil(0x00_8037, 0x1000_0000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		t.Fatal(err)
	}

	//patchedTileset := false
	patchedTileset := true
	dumpedAsm := false

	maptiles := make([]image.Image, 0x128)

	wg := sync.WaitGroup{}
	// supertile:
	for supertile := uint16(0); supertile < 0x128; supertile++ {
		//fmt.Fprintf(s.Logger, "supertile $%03x\n", supertile)
		binary.LittleEndian.PutUint16(s.WRAM[0xA0:0xA2], supertile)

		// JSL 02_FFC7 ; JSR NMI_DoUpdates
		s.SetPC(0x00_8037)
		if stopPC, expectedPC, cycles := s.RunUntil(0x00_804A, 0x1000_0000); stopPC != expectedPC {
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
		}

		// dump VRAM+WRAM for each supertile:
		vram := make([]byte, 65536)
		copy(vram, (*(*[65536]byte)(unsafe.Pointer(&s.VRAM[0])))[:])
		wram := make([]byte, 131072)
		copy(wram, s.WRAM[:])
		wg.Add(1)
		go func(st uint16, wram []byte, vram []byte) {
			//ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", st), wram, 0644)
			//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)
			//ioutil.WriteFile(fmt.Sprintf("data/%03X.cgram", st), wram[0xC300:0xC700], 0644)

			cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
			pal := cgramToPalette(cgram)

			// render BG1 image:
			{
				g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)

				// BG2 first:
				renderBG(
					g,
					(*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x4000])))[:],
					vram[0x4000:0x8000],
				)

				// BG1:
				renderBG(
					g,
					(*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x2000])))[:],
					vram[0x4000:0x8000],
				)

				maptiles[st] = g

				if err = exportPNG(fmt.Sprintf("data/%03X.bg1.png", st), g); err != nil {
					panic(err)
				}
			}

			//{
			//	tiles := 0x4000 / 32
			//	g := image.NewPaletted(image.Rect(0, 0, 16*8, (tiles/16)*8), pal)
			//	for t := 0; t < tiles; t++ {
			//		// palette 2
			//		z := uint16(t) | (2 << 10)
			//		draw4bppTile(
			//			g,
			//			z,
			//			vram[0x4000:0x8000],
			//			t%16,
			//			t/16,
			//		)
			//	}
			//
			//	if err = exportPNG(fmt.Sprintf("data/%03X.vram.png", st), g); err != nil {
			//		panic(err)
			//	}
			//}

			wg.Done()
		}(supertile, wram, vram)
	}

	wg.Wait()

	// condense all maps into one image:
	const divider = 8
	const supertilepx = (512 / divider)
	all := image.NewNRGBA(image.Rect(0, 0, 0x10*supertilepx, (0x130*supertilepx)/0x10))
	for st := 0; st < 0x128; st++ {
		row := st / 0x10
		col := st % 0x10
		draw.NearestNeighbor.Scale(
			all,
			image.Rect(col*supertilepx, row*supertilepx, col*supertilepx+supertilepx, row*supertilepx+supertilepx),
			maptiles[st],
			maptiles[st].Bounds(),
			draw.Src,
			nil,
		)
	}
	if err = exportPNG(fmt.Sprintf("data/all.png"), all); err != nil {
		panic(err)
	}
}

func exportPNG(name string, g image.Image) (err error) {
	// export to PNG:
	var po *os.File

	po, err = os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() {
		err = po.Close()
		if err != nil {
			return
		}
	}()

	err = png.Encode(po, g)
	if err != nil {
		return
	}
	return
}

func cgramToPalette(cgram []uint16) color.Palette {
	pal := make(color.Palette, 256)
	for i, bgr15 := range cgram {
		// convert BGR15 color format (MSB unused) to RGB24:
		b := (bgr15 & 0x7C00) >> 10
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

func renderBG(g *image.Paletted, bg []uint16, tiles []uint8) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			z := bg[a]
			a++

			draw4bppTile(g, z, tiles, tx, ty)
		}
	}
}

func draw4bppTile(g *image.Paletted, z uint16, tiles []uint8, tx int, ty int) {
	//High     Low          Legend->  c: Starting character (tile) number
	//vhopppcc cccccccc               h: horizontal flip  v: vertical flip
	//                                p: palette number   o: priority bit

	p := byte((z>>10)&7) << 4
	c := int(z & 0x03FF)
	for y := 0; y < 8; y++ {
		fy := y
		if z&0x8000 != 0 {
			fy = 7 - y
		}
		p0 := tiles[(c<<5)+(y<<1)]
		p1 := tiles[(c<<5)+(y<<1)+1]
		p2 := tiles[(c<<5)+(y<<1)+16]
		p3 := tiles[(c<<5)+(y<<1)+17]
		for x := 0; x < 8; x++ {
			fx := x
			if z&0x4000 == 0 {
				fx = 7 - x
			}

			i := byte((p0>>x)&1) |
				byte(((p1>>x)&1)<<1) |
				byte(((p2>>x)&1)<<2) |
				byte(((p3>>x)&1)<<3)

			// transparency:
			if i == 0 {
				continue
			}

			g.SetColorIndex(tx<<3+fx, ty<<3+fy, p+i)
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

type DMAChannel struct{}

func (c *DMAChannel) Transfer(regs *DMARegs, ch int, h *HWIO) {
	aSrc := uint32(regs.srcB())<<16 | uint32(regs.srcH())<<8 | uint32(regs.srcL())
	siz := uint16(regs.sizH())<<8 | uint16(regs.sizL())

	bDest := regs.dest()
	bDestAddr := uint32(bDest) | 0x2100

	incr := regs.ctrl()&0x10 == 0
	fixed := regs.ctrl()&0x08 != 0
	mode := regs.ctrl() & 7

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
	//	fmt.Fprintf(h.s.Logger, "DMA[%d] start: $%06x -> $%04x [$%05x]\n", ch, aSrc, bDestAddr, siz)
	//}

	if regs.ctrl()&0x80 != 0 {
		// PPU -> CPU
		panic("PPU -> CPU DMA transfer not supported!")
	} else {
		// CPU -> PPU
	copyloop:
		for {
			switch mode {
			case 0:
				h.Write(bDestAddr, h.s.Bus.EaRead(aSrc))
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
				h.Write(bDestAddr, h.s.Bus.EaRead(aSrc))
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
				h.Write(bDestAddr+1, h.s.Bus.EaRead(aSrc))
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
	}

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "DMA[%d]  stop: $%06x -> $%04x [$%05x]\n", ch, aSrc, bDestAddr, siz)
	//}
}

type HWIO struct {
	s   *System
	mem [0x10000]uint8

	dmaregs [16]DMARegs
	dma     [8]DMAChannel

	ppu struct {
		incrMode      bool   // false = increment after $2118, true = increment after $2119
		incrAmt       uint32 // 1, 32, or 128
		addrRemapping byte
		addr          uint32
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
		ch := (offs & 0x00F0) >> 4
		if ch <= 7 {
			reg := offs & 0x000F
			h.dmaregs[ch][reg] = value
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
	if offs == 0x2102 || offs == 0x2103 {
		// OAMADD
		return
	}
	if offs == 0x2104 {
		// OAMDATA
		return
	}
	if offs == 0x2121 {
		// CGADD
		return
	}
	if offs == 0x2122 {
		// CGDATA
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
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMAIN = $%02x\n", value)
		//}
		return
	}
	if offs == 0x2116 {
		// VMADDL
		h.ppu.addr = uint32(value) | h.ppu.addr&0xFF00
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMADDL = $%04x\n", h.ppu.addr)
		//}
		return
	}
	if offs == 0x2117 {
		// VMADDH
		h.ppu.addr = uint32(value)<<8 | h.ppu.addr&0x00FF
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMADDH = $%04x\n", h.ppu.addr)
		//}
		return
	}
	if offs == 0x2118 {
		// VMDATAL
		h.s.VRAM[h.ppu.addr<<1] = value
		if h.ppu.incrMode == false {
			h.ppu.addr += h.ppu.incrAmt
		}
		return
	}
	if offs == 0x2119 {
		// VMDATAH
		h.s.VRAM[(h.ppu.addr<<1)+1] = value
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
