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
	// this is useless zeroing of memory; don't need to run it
	//#_00802C: JSR Startup_InitializeMemory
	if err = s.Exec(0x00_8029); err != nil {
		t.Fatal(err)
	}

	var b01LoadAndDrawRoomPC uint32
	var b01LoadAndDrawRoomSetSupertilePC uint32
	{
		// must execute in bank $01
		a = asm.NewEmitter(s.HWIO.Dyn[0x01_5100&0xFFFF-0x5000:], true)
		a.SetBase(0x01_5100)

		{
			b01LoadAndDrawRoomPC = a.Label("loadAndDrawRoom")
			a.REP(0x30)
			b01LoadAndDrawRoomSetSupertilePC = a.Label("loadAndDrawRoomSetSupertile") + 1
			a.LDA_imm16_w(0x0000)
			a.STA_dp(0xA0)
			a.SEP(0x30)

			// loads header and draws room
			a.Comment("Underworld_LoadRoom#_01873A")
			a.JSL(0x01_873A)

			a.Comment("Underworld_LoadCustomTileAttributes#_0FFD65")
			a.JSL(0x0F_FD65)
			a.Comment("Underworld_LoadAttributeTable#_01B8BF")
			a.JSL(0x01_B8BF)

			// then JSR Underworld_LoadHeader#_01B564 to reload the doors into $19A0[16]
			//a.BRA("jslUnderworld_LoadHeader")
			a.WDM(0xAA)
		}

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(s.Logger)
	}

	// this routine renders a supertile assuming gfx tileset and palettes already loaded:
	b02LoadUnderworldSupertilePC := uint32(0x02_5200)
	{
		// emit into our custom $02:5100 routine:
		a = asm.NewEmitter(s.HWIO.Dyn[b02LoadUnderworldSupertilePC&0xFFFF-0x5000:], true)
		a.SetBase(b02LoadUnderworldSupertilePC)
		a.Comment("setup bank restore back to $00")
		a.SEP(0x30)
		a.LDA_imm8_b(0x00)
		a.PHA()
		a.PLB()
		a.Comment("in Underworld_LoadEntrance_DoPotsBlocksTorches at PHB and bank switch to $7e")
		a.JSR_abs(0xD854)
		a.Comment("Module06_UnderworldLoad after JSR Underworld_LoadEntrance")
		a.JMP_abs_imm16_w(0x8157)
		a.Comment("implied RTL")
		a.WriteTextTo(s.Logger)
	}

	var loadEntrancePC uint32
	var setEntranceIDPC uint32
	var loadSupertilePC uint32
	var donePC uint32
	{
		// emit into our custom $00:5000 routine:
		a = asm.NewEmitter(s.HWIO.Dyn[:], true)
		a.SetBase(0x00_5000)
		a.SEP(0x30)

		a.Comment("InitializeTriforceIntro#_0CF03B: sets up initial state")
		a.JSL(0x0C_F03B)
		a.Comment("LoadDefaultTileAttributes#_0FFD2A")
		a.JSL(0x0F_FD2A)

		// general world state:
		a.Comment("disable rain")
		a.LDA_imm8_b(0x02)
		a.STA_abs(0xF3C5)

		a.Comment("no bed cutscene")
		a.LDA_imm8_b(0x10)
		a.STA_abs(0xF3C6)

		loadEntrancePC = a.Label("loadEntrance")
		a.SEP(0x30)
		// prepare to call the underworld room load module:
		a.Comment("module $06, submodule $00:")
		a.LDA_imm8_b(0x06)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.STZ_dp(0xB0)

		a.Comment("dungeon entrance DungeonID")
		setEntranceIDPC = a.Label("setEntranceID") + 1
		a.LDA_imm8_b(0x08)
		a.STA_abs(0x010E)

		// loads a dungeon given an entrance ID:
		a.Comment("JSL MainRouting")
		a.JSL(0x00_80B5)
		a.BRA("updateVRAM")

		loadSupertilePC = a.Label("loadSupertile")
		a.SEP(0x30)
		a.INC_abs(0x0710)
		a.Comment("Intro_InitializeDefaultGFX after JSL DecompressAnimatedUnderworldTiles")
		a.JSL(0x0C_C237)
		a.STZ_dp(0x11)
		a.Comment("LoadUnderworldSupertile")
		a.JSL(b02LoadUnderworldSupertilePC)

		a.Label("updateVRAM")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(0x85FC)
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0) // NMI_DoUpdates

		// WDM triggers an abort for values >= 10
		donePC = a.Label("done")
		a.WDM(0xAA)

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(s.Logger)
	}

	{
		// skip over music & sfx loading since we did not implement APU registers:
		a = newEmitterAt(s, 0x02_8293, true)
		//#_028293: JSR Underworld_LoadSongBankIfNeeded
		a.JMP_abs_imm16_w(0x82BC)
		//.exit
		//#_0282BC: SEP #$20
		//#_0282BE: RTL
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
	_ = loadSupertilePC

	// run the initialization code:
	if err = s.ExecAt(0x00_5000, donePC); err != nil {
		t.Fatal(err)
	}

	maptiles := make([]image.Image, 0x128)

	// make a set to determine which supertiles have been visited:
	stVisited := make(map[Supertile]struct{})

	// iterate over entrances:
	wg := sync.WaitGroup{}
	const entranceCount = 0x84
	for eID := uint8(0); eID < entranceCount; eID++ {
		fmt.Fprintf(s.Logger, "entrance $%02x\n", eID)

		// poke the entrance ID into our asm code:
		s.HWIO.Dyn[setEntranceIDPC-0x5000] = eID
		// load the entrance and draw the room:
		if err = s.ExecAt(loadEntrancePC, donePC); err != nil {
			t.Fatal(err)
		}

		entranceSupertile := Supertile(s.ReadWRAM16(0xA0))

		// render the entrance supertile in the background:
		if err = renderSupertile(s, &wg, maptiles, uint16(entranceSupertile)); err != nil {
			t.Fatal(err)
		}

		// build a stack (LIFO) of supertiles to visit:
		lifo := make([]Supertile, 0, 0x100)
		lifo = append(lifo, entranceSupertile)

		// build a list of supertiles to render following from this entrance:
		toRender := make([]Supertile, 0, 0x100)

		// process the LIFO:
		for len(lifo) != 0 {
			// pop off the stack:
			lifoEnd := len(lifo) - 1
			this := lifo[lifoEnd]
			lifo = lifo[0:lifoEnd]

			// skip this supertile if we already visited it:
			if _, isVisited := stVisited[this]; isVisited {
				fmt.Fprintf(s.Logger, "  already visited supertile %s\n", this)
				continue
			}

			// mark as visited:
			stVisited[this] = struct{}{}
			toRender = append(toRender, this)

			fmt.Fprintf(s.Logger, "  supertile   = %s\n", this)

			// discover all supertile doorway/pit/warp exits from this supertile:
			doorwaysTo := make([]Supertile, 0, 16)

			// load and draw current supertile:
			write16(s.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, uint16(this))
			if err = s.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
				panic(err)
			}

			ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", uint16(this)), s.WRAM[:], 0644)

			// determine if falling through pits and pots is feasible:
			// alternatively: decide if the WARPTO byte was leaked from a neighboring room header table entry
			//WARPTO          = $7EC000
			//s.ReadWRAM8(0xC000)
			fmt.Fprintf(s.Logger, "    WARPTO   = %s\n", Supertile(read8(s.WRAM[:], 0xC000)))
			fmt.Fprintf(s.Logger, "    STAIR0TO = %s\n", Supertile(read8(s.WRAM[:], 0xC001)))
			fmt.Fprintf(s.Logger, "    STAIR1TO = %s\n", Supertile(read8(s.WRAM[:], 0xC002)))
			fmt.Fprintf(s.Logger, "    STAIR2TO = %s\n", Supertile(read8(s.WRAM[:], 0xC003)))
			fmt.Fprintf(s.Logger, "    STAIR3TO = %s\n", Supertile(read8(s.WRAM[:], 0xC004)))

			exitSeen := make(map[Supertile]struct{}, 24)
			markExit := func(st Supertile, name string) {
				if _, ok := exitSeen[st]; ok {
					return
				}

				if st == 0 {
					panic("exit to 0!!")
				}
				exitSeen[st] = struct{}{}
				doorwaysTo = append(doorwaysTo, st)
				fmt.Fprintf(s.Logger, "    %s to %s\n", name, st)
			}

			// make a mutable copy of the tile type map:
			tiletypes := [0x2000]uint8{}
			copy(tiletypes[:], s.WRAM[0x12000:0x14000])

			ioutil.WriteFile(fmt.Sprintf("data/%03X.tmap", uint16(this)), tiletypes[:], 0644)

			// FAIL: while this works for HC $001 it fails for TT entrance $0DB
			// propagate collision from BG1 to BG2 and vice versa; this should fill in unreachable areas:
			//for t := 0; t < 0x1000; t++ {
			//	if tiletypes[t] == 0 && tiletypes[t+0x1000] != 0 {
			//		tiletypes[t] = tiletypes[t+0x1000]
			//	} else if tiletypes[t+0x1000] == 0 && tiletypes[t] != 0 {
			//		tiletypes[t+0x1000] = tiletypes[t]
			//	}
			//}

			// process doors first:
			doors := make([]Door, 0, 16)
			for m := 0; m < 16; m++ {
				tpos := read16(s.WRAM[:], uint32(0x19A0+(m<<1)))
				// stop marker:
				if tpos == 0 {
					break
				}

				door := Door{
					Pos:  tpos >> 1, // we're only interested in tile type map positions, not the gfx tile map
					Type: DoorType(read16(s.WRAM[:], uint32(0x1980+(m<<1)))),
					Dir:  DoorDir(read16(s.WRAM[:], uint32(0x19C0+(m<<1)))),
				}

				fmt.Fprintf(s.Logger, "    door: %#v\n", &door)

				if door.Type.IsExit() {
					// fill in exit doors with collision tiles so they don't interfere with inter-tile door detection:
					for i := uint16(0); i < 4; i++ {
						tiletypes[door.Pos+0x00+i] = 0x01
						tiletypes[door.Pos+0x40+i] = 0x01
					}
					continue
				}

				doors = append(doors, door)
			}

			//STAIR0TO = $7EC001
			//STAIR1TO = $7EC002
			//STAIR2TO = $7EC003
			//STAIR3TO = $7EC004
			stairExitTo := [4]Supertile{
				Supertile(read8(s.WRAM[:], uint32(0xC001))),
				Supertile(read8(s.WRAM[:], uint32(0xC002))),
				Supertile(read8(s.WRAM[:], uint32(0xC003))),
				Supertile(read8(s.WRAM[:], uint32(0xC004))),
			}

			// grab list of interroom staircases:
			stairs := make([]StaircaseInterRoom, 0, 4)
			for m := 0; m < 4; m++ {
				pos := read16(s.WRAM[:], uint32(0x06B0+(m<<1)))
				// stop marker:
				if pos == 0 {
					break
				}

				staircase := StaircaseInterRoom{
					Pos: pos,
				}

				fmt.Fprintf(s.Logger, "    interroom stairs: %#v\n", &staircase)

				stairs = append(stairs, staircase)
			}

			//WARPTO   = $7EC000
			warpExitTo := Supertile(read8(s.WRAM[:], 0xC000))

			// scan BG1 and BG2 tiletype map for warp tiles, door tiles, stair tiles, etc:
			for _, offs := range []uint32{0x0000, 0x1000} {
				for t := uint32(0); t < 0x1000; t++ {
					x := tiletypes[offs+t]

					if x == 0x4B {
						// warp tile
						markExit(warpExitTo, "warp")
					} else if x == 0x89 {
						// east/west transport door:
						stairSupertile := stairExitTo[3]
						fmt.Fprintf(s.Logger, "    transport door to %s\n", stairSupertile)
						markExit(stairSupertile, "transport door")
					} else

					// supertile exiting stairwells:
					if x >= 0x1D && x <= 0x1F {
						fmt.Fprintf(s.Logger, "    stair $%02x at $%04x\n", x, t)
					} else if x == 0x22 {
						fmt.Fprintf(s.Logger, "    stair $%02x at $%04x\n", x, t)
					} else if x == 0x3D {
						fmt.Fprintf(s.Logger, "    stair $%02x at $%04x\n", x, t)
					} else if x >= 0x3E && x <= 0x3F {
						fmt.Fprintf(s.Logger, "    stair $%02x at $%04x\n", x, t)
					} else if x >= 0x5E && x <= 0x5F {
						doorTile := tiletypes[offs+0x40+t]
						if doorTile&0xF8 == 0x30 {
							stairs := doorTile & 0x03
							fmt.Fprintf(s.Logger, "    stair%d $%02x at $%04x\n", stairs, x, t)
							markExit(stairExitTo[stairs], fmt.Sprintf("stair%d", stairs))

							// block up the stairwell:
							for i := uint32(0); i < 2; i++ {
								tiletypes[offs+t+0x00+i] = 0x01
								tiletypes[offs+t+0x40+i] = 0x01
							}
							if tiletypes[offs+0x80+t] >= 0xF0 {
								// block up the doorway so that door analysis does not think this door goes to an adjacent supertile:
								for i := uint32(0); i < 2; i++ {
									tiletypes[offs+t+0x80+i] = 0x01
									tiletypes[offs+t+0xC0+i] = 0x01
								}
							}
						}
					} else if x >= 0x30 && x <= 0x37 {
						stairs := x & 0x03
						fmt.Fprintf(s.Logger, "    stair%d $%02x at $%04x\n", stairs, x, t)
						markExit(stairExitTo[stairs], fmt.Sprintf("stair%d", stairs))
					} else if x >= 0x38 && x <= 0x39 {
						var stairs uint32
						stairs = 0 // TODO confirm
						fmt.Fprintf(s.Logger, "    stair%d $%02x at $%04x\n", stairs, x, t)
						markExit(stairExitTo[stairs], fmt.Sprintf("stair%d", stairs))
					}
				}

				// scan edges of map for open doorways:
				for t := uint32(0); t < 0x40; t++ {
					if st, _, ok := this.MoveBy(DirNorth); ok {
						x := tiletypes[offs+t]
						if x == 0x80 || x == 0x84 {
							markExit(st, "north open doorway")
						} else if x == 0x00 {
							//markExit(st, "north open walkway")
						}

						x = tiletypes[offs+0x0180+t]
						if x >= 0xF0 {
							markExit(st, "north door")
						}
					}

					if st, _, ok := this.MoveBy(DirSouth); ok {
						x := tiletypes[offs+0x0FC0+t]
						if x == 0x80 || x == 0x84 {
							markExit(st, "south open doorway")
						} else if x == 0x00 {
							//markExit(st, "south open walkway")
						}

						x = tiletypes[offs+0x0EC0+t]
						if x >= 0xF0 {
							markExit(st, "south door")
						}
					}

					if st, _, ok := this.MoveBy(DirWest); ok {
						x := tiletypes[offs+t<<6]
						if x == 0x81 || x == 0x85 {
							markExit(st, "west open doorway")
						} else if x == 0x00 {
							//markExit(st, "west open walkway")
						}

						x = tiletypes[offs+0x04+t<<6]
						if x >= 0xF0 {
							markExit(st, "west door")
						}
					}

					if st, _, ok := this.MoveBy(DirEast); ok {
						x := tiletypes[offs+0x003F+t<<6]
						if x == 0x81 || x == 0x85 {
							markExit(st, "east open doorway")
						} else if x == 0x00 {
							//markExit(st, "east open walkway")
						}

						x = tiletypes[offs+0x003B+t<<6]
						if x >= 0xF0 {
							markExit(st, "east door")
						}
					}
				}
			}

			for _, st := range doorwaysTo {
				lifo = append(lifo, st)
			}
		}

		// which supertiles this entrance should render:
		fmt.Fprintf(s.Logger, "  render: %#v\n", toRender)

		// render all supertiles found:
		if len(toRender) >= 1 {
			for _, st := range toRender[1:] {
				// loadSupertile:
				write16(s.WRAM[:], 0xA0, uint16(st))
				if err = s.ExecAt(loadSupertilePC, donePC); err != nil {
					t.Fatal(err)
				}

				// render the supertile to an Image:
				if err = renderSupertile(s, &wg, maptiles, uint16(st)); err != nil {
					t.Fatal(err)
				}
			}

			// gfx output is:
			//  s.VRAM: $4000[0x2000] = 4bpp tile graphics
			//  s.WRAM: $2000[0x2000] = BG1 64x64 tile map  [64][64]uint16
			//  s.WRAM: $4000[0x2000] = BG2 64x64 tile map  [64][64]uint16
			//  s.WRAM:$12000[0x1000] = BG1 64x64 tile type [64][64]uint8
			//  s.WRAM:$12000[0x1000] = BG2 64x64 tile type [64][64]uint8
			//  s.WRAM: $C300[0x0200] = CGRAM palette
		}

	}

	wg.Wait()

	// condense all maps into one image at different scale levels:
	for _, divider := range []int{ /*8, 4, */ 2, 1} {
		supertilepx := 512 / divider

		for _, sc := range []struct {
			S draw.Scaler
			N string
		}{
			{draw.NearestNeighbor, "nn"},
			{draw.ApproxBiLinear, "ab"},
			//{draw.BiLinear, "bl"},
			//{draw.CatmullRom, "cr"},
		} {
			wga := sync.WaitGroup{}
			all := image.NewNRGBA(image.Rect(0, 0, 0x10*supertilepx, (0x130*supertilepx)/0x10))
			for st := 0; st < 0x128; st++ {
				stMap := maptiles[st]
				if stMap == nil {
					continue
				}
				row := st / 0x10
				col := st % 0x10
				wga.Add(1)
				go func() {
					sc.S.Scale(
						all,
						image.Rect(col*supertilepx, row*supertilepx, col*supertilepx+supertilepx, row*supertilepx+supertilepx),
						stMap,
						stMap.Bounds(),
						draw.Src,
						nil,
					)
					wga.Done()
				}()
			}
			wga.Wait()
			if err = exportPNG(fmt.Sprintf("data/all-%d-%s.png", divider, sc.N), all); err != nil {
				panic(err)
			}
		}
	}
}

type Supertile uint16

func (s Supertile) String() string { return fmt.Sprintf("$%03x", uint16(s)) }

func (s Supertile) MoveBy(dir DoorDir) (Supertile, DoorDir, bool) {
	switch dir {
	case DirNorth:
		return Supertile(uint16(s) - 0x10), dir, uint16(s)&0xF0 > 0
	case DirSouth:
		return Supertile(uint16(s) + 0x10), dir, uint16(s)&0xF0 < 0xF0
	case DirWest:
		return Supertile(uint16(s) - 1), dir, uint16(s)&0x0F > 0
	case DirEast:
		return Supertile(uint16(s) + 1), dir, uint16(s)&0x0F < 0xF
	}
	return s, dir, false
}

type StaircaseInterRoom struct {
	Pos uint16 // $06B0
}

type Door struct {
	Type DoorType // $1980
	Pos  uint16   // $19A0
	Dir  DoorDir  // $19C0
}

type DoorDir uint8

const (
	DirNorth DoorDir = iota
	DirSouth
	DirWest
	DirEast
)

func (d DoorDir) MoveEG2(s Supertile) (Supertile, bool) {
	if s < 0x100 {
		return s, false
	}

	switch d {
	case DirNorth:
		return s - 0x10, s&0xF0 > 0
	case DirSouth:
		return s + 0x10, s&0xF0 < 0xF0
	case DirWest:
		return s - 1, s&0x0F > 0
	case DirEast:
		return s + 1, s&0x0F < 0x02
	}
	return s, false
}

func (d DoorDir) Opposite() DoorDir {
	switch d {
	case DirNorth:
		return DirSouth
	case DirSouth:
		return DirNorth
	case DirWest:
		return DirEast
	case DirEast:
		return DirWest
	}
	return d
}

func (d DoorDir) String() string {
	switch d {
	case DirNorth:
		return "north"
	case DirSouth:
		return "south"
	case DirWest:
		return "west"
	case DirEast:
		return "east"
	}
	return ""
}

type DoorType uint8

func (t DoorType) IsExit() bool {
	if t >= 0x04 && t <= 0x06 {
		// exit door:
		return true
	}
	if t >= 0x0A && t <= 0x12 {
		// exit door:
		return true
	}
	if t == 0x2A {
		// bombable exit door:
		return true
	}
	return false
}

func (t DoorType) IsStairwell() bool {
	return t >= 0x20 && t <= 0x26
}

func renderSupertile(s *System, wg *sync.WaitGroup, maptiles []image.Image, supertile uint16) (err error) {
	// dump VRAM+WRAM for each supertile:
	vram := make([]byte, 65536)
	copy(vram, (*(*[65536]byte)(unsafe.Pointer(&s.VRAM[0])))[:])
	wram := make([]byte, 131072)
	copy(wram, s.WRAM[:])

	wg.Add(1)
	go func(st uint16, wram []byte, vram []byte) {
		var err error

		//ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", st), wram, 0644)
		//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)
		//ioutil.WriteFile(fmt.Sprintf("data/%03X.cgram", st), wram[0xC300:0xC700], 0644)

		cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
		pal := cgramToPalette(cgram)

		// render BG image:
		if true {
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

			// store full underworld rendering for inclusion into EG map:
			maptiles[st] = g

			if err = exportPNG(fmt.Sprintf("data/%03X.bg1.png", st), g); err != nil {
				panic(err)
			}
		}

		// render VRAM BG tiles to a PNG:
		if false {
			tiles := 0x4000 / 32
			g := image.NewPaletted(image.Rect(0, 0, 16*8, (tiles/16)*8), pal)
			for t := 0; t < tiles; t++ {
				// palette 2
				z := uint16(t) | (2 << 10)
				draw4bppTile(
					g,
					z,
					vram[0x4000:0x8000],
					t%16,
					t/16,
				)
			}

			if err = exportPNG(fmt.Sprintf("data/%03X.vram.png", st), g); err != nil {
				panic(err)
			}
		}

		wg.Done()
	}(supertile, wram, vram)

	return err
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

func read16(b []byte, addr uint32) uint16 {
	return binary.LittleEndian.Uint16(b[addr : addr+2])
}

func read8(b []byte, addr uint32) uint8 {
	return b[addr]
}

func write8(b []byte, addr uint32, value uint8) {
	b[addr] = value
}

func write16(b []byte, addr uint32, value uint16) {
	binary.LittleEndian.PutUint16(b[addr:addr+2], value)
}

func write24(b []byte, addr uint32, value uint32) {
	binary.LittleEndian.PutUint16(b[addr:addr+2], uint16(value&0x00FFFF))
	b[addr+3] = byte(value >> 16)
}

type System struct {
	// emulated system:
	Bus *bus.Bus
	CPU *cpu65c816.CPU
	*HWIO

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
		s.HWIO = &HWIO{s: s}
		for b := uint32(0); b < 0x70; b++ {
			bank := b << 16
			err = s.Bus.Attach(
				s.HWIO,
				"hwio",
				bank|0x2000,
				bank|0x7FFF,
			)
			if err != nil {
				return
			}

			bank = (b + 0x80) << 16
			err = s.Bus.Attach(
				s.HWIO,
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

func (s *System) ReadWRAM24(offs uint32) uint32 {
	lohi := uint32(binary.LittleEndian.Uint16(s.WRAM[offs : offs+2]))
	bank := uint32(s.WRAM[offs+3])
	return bank<<16 | lohi
}

func (s *System) ReadWRAM16(offs uint32) uint16 {
	return binary.LittleEndian.Uint16(s.WRAM[offs : offs+2])
}

func (s *System) ReadWRAM8(offs uint32) uint8 {
	return s.WRAM[offs]
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
			fmt.Fprintln(s.LoggerCPU)
		}
		if s.GetPC() == targetPC {
			break
		}

		nCycles, abort := s.CPU.Step()
		cycles += uint64(nCycles)

		if abort {
			// fake that it's ok:
			stopPC = s.GetPC()
			expectedPC = s.GetPC()
			return
		}
	}

	stopPC = s.GetPC()
	return
}

func (s *System) ExecAt(startPC, donePC uint32) (err error) {
	s.SetPC(startPC)
	return s.Exec(donePC)
}

func (s *System) Exec(donePC uint32) (err error) {
	var stopPC uint32
	var expectedPC uint32
	var cycles uint64

	if stopPC, expectedPC, cycles = s.RunUntil(donePC, 0x1000_0000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		return
	}

	return
}

type DMARegs [16]byte

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
	s *System

	dmaregs [8]DMARegs
	dma     [8]DMAChannel

	ppu struct {
		incrMode      bool   // false = increment after $2118, true = increment after $2119
		incrAmt       uint32 // 1, 32, or 128
		addrRemapping byte
		addr          uint32
	}

	// mapped to $5000-$7FFF
	Dyn [0x3000]byte
}

func (h *HWIO) Read(address uint32) (value byte) {
	offs := address & 0xFFFF
	if offs >= 0x5000 {
		value = h.Dyn[offs-0x5000]
		return
	}

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] -> $%02x\n", offs, value)
	//}
	return
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

	if h.s.Logger != nil {
		fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x\n", offs, value)
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
