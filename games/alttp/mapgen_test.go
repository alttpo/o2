package alttp

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/emulator/bus"
	"github.com/alttpo/snes/emulator/cpu65c816"
	"github.com/alttpo/snes/emulator/memory"
	"github.com/alttpo/snes/mapping/lorom"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/ioutil"
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

const drawOverlays = false

var (
	b02LoadUnderworldSupertilePC     uint32 = 0x02_5200
	b01LoadAndDrawRoomPC             uint32
	b01LoadAndDrawRoomSetSupertilePC uint32
	b00HandleRoomTagsPC              uint32 = 0x00_5300
	loadEntrancePC                   uint32
	setEntranceIDPC                  uint32
	loadSupertilePC                  uint32
	runMainRoutingPC                 uint32
	donePC                           uint32
)

func TestGenerateMap(t *testing.T) {
	var err error

	var f *os.File
	f, err = os.Open("alttp-jp.sfc")
	if err != nil {
		t.Skip(err)
	}

	var e *System

	// create the CPU-only SNES emulator:
	e = &System{
		Logger:    os.Stdout,
		LoggerCPU: nil,
	}
	if err = e.CreateEmulator(); err != nil {
		t.Fatal(err)
	}

	_, err = f.Read(e.ROM[:])
	if err != nil {
		t.Fatal(err)
	}
	err = f.Close()
	if err != nil {
		t.Fatal(err)
	}

	var a *asm.Emitter

	// initialize game:
	e.CPU.Reset()
	//#_008029: JSR Sound_LoadIntroSongBank		// skip this
	// this is useless zeroing of memory; don't need to run it
	//#_00802C: JSR Startup_InitializeMemory
	if err = e.Exec(0x00_8029); err != nil {
		t.Fatal(err)
	}

	{
		// must execute in bank $01
		a = asm.NewEmitter(e.HWIO.Dyn[0x01_5100&0xFFFF-0x5000:], true)
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
		a.WriteTextTo(e.Logger)
	}

	// this routine renders a supertile assuming gfx tileset and palettes already loaded:
	{
		// emit into our custom $02:5100 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b02LoadUnderworldSupertilePC&0xFFFF-0x5000:], true)
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
		a.WriteTextTo(e.Logger)
	}

	if false {
		// TODO: pit detection using Link_ControlHandler
		// bank 07
		// force a pit detection:
		// set $02E4 = 0 to allow control of link
		// set $55 = 0 to disable cape
		// set $5D base state to $01 to check pits
		// set $5B = $02
		// JSL Link_Main#_078000
		// output $59 != 0 if pit detected; $A0 changed
	}

	{
		// emit into our custom $00:5000 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[:], true)
		a.SetBase(0x00_5000)
		a.SEP(0x30)

		a.Comment("InitializeTriforceIntro#_0CF03B: sets up initial state")
		a.JSL(0x0C_F03B)
		a.Comment("LoadDefaultTileAttributes#_0FFD2A")
		a.JSL(0x0F_FD2A)

		// general world state:
		a.Comment("disable rain")
		a.LDA_imm8_b(0x02)
		a.STA_long(0x7EF3C5)

		a.Comment("no bed cutscene")
		a.LDA_imm8_b(0x10)
		a.STA_long(0x7EF3C6)

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
		a.Comment("JSL Module_MainRouting")
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
		a.WriteTextTo(e.Logger)
	}

	{
		// emit into our custom $00:5300 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b00HandleRoomTagsPC&0xFFFF-0x5000:], true)
		a.SetBase(b00HandleRoomTagsPC)

		a.SEP(0x30)

		a.Comment("Module07_Underworld")
		a.LDA_imm8_b(0x07)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.STZ_dp(0xB0)

		//write8(e.WRAM[:], 0x04BA, 0)
		a.Comment("no cutscene")
		a.STZ_abs(0x02E4)
		a.Comment("enable tags")
		a.STZ_abs(0x04C7)

		//a.Comment("Graphics_LoadChrHalfSlot#_00E43A")
		//a.JSL(0x00_E43A)
		a.Comment("Underworld_HandleRoomTags#_01C2FD")
		a.JSL(0x01_C2FD)

		// check if submodule changed:
		a.LDA_dp(0x11)
		a.BEQ("no_submodule")

		runMainRoutingPC = a.Label("continue_submodule")
		a.Comment("JSL Module_MainRouting")
		a.JSL(0x00_80B5)

		a.Label("no_submodule")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(0x85FC)

		// fake NMI:
		//a.REP(0x30)
		//a.PHD()
		//a.PHB()
		//a.LDA_imm16_w(0)
		//a.TCD()
		//a.PHK()
		//a.PLB()
		//a.SEP(0x30)
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0) // NMI_DoUpdates
		//a.PLB()
		//a.PLD()
		a.LDA_dp(0x11)
		a.BNE("continue_submodule")

		a.STZ_dp(0x11)
		a.WDM(0xAA)

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	{
		// skip over music & sfx loading since we did not implement APU registers:
		a = newEmitterAt(e, 0x02_8293, true)
		//#_028293: JSR Underworld_LoadSongBankIfNeeded
		a.JMP_abs_imm16_w(0x82BC)
		//.exit
		//#_0282BC: SEP #$20
		//#_0282BE: RTL
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out RebuildHUD:
		a = newEmitterAt(e, 0x0D_FA88, true)
		//RebuildHUD_Keys:
		//	#_0DFA88: STA.l $7EF36F
		a.RTL()
		a.WriteTextTo(e.Logger)
	}

	//s.LoggerCPU = os.Stdout
	_ = loadSupertilePC

	// run the initialization code:
	if err = e.ExecAt(0x00_5000, donePC); err != nil {
		t.Fatal(err)
	}

	//RoomsWithPitDamage#_00990C [0x70]uint16
	roomsWithPitDamage := make(map[Supertile]bool, 0x128)
	for i := Supertile(0); i < 0x128; i++ {
		roomsWithPitDamage[i] = false
	}
	for i := 0; i <= 0x70; i++ {
		romaddr, _ := lorom.BusAddressToPak(0x00_990C)
		st := Supertile(read16(e.ROM[:], romaddr+uint32(i)<<1))
		roomsWithPitDamage[st] = true
	}

	const entranceCount = 0x85
	entranceGroups := make([]Entrance, entranceCount)
	supertiles := make(map[Supertile]*RoomState, 0x128)

	// scan underworld for certain tile types:
	if false {
		// poke the entrance ID into our asm code:
		e.HWIO.Dyn[setEntranceIDPC-0x5000] = 0x00
		// load the entrance and draw the room:
		if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
			t.Fatal(err)
		}

		for st := uint16(0); st < 0x128; st++ {
			// load and draw current supertile:
			write16(e.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, st)
			if err = e.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
				panic(err)
			}

			found := false
			for t, v := range e.WRAM[0x12000:0x14000] {
				if v == 0x0A {
					found = true
					fmt.Printf("%s: %s = $0A\n", Supertile(st), MapCoord(t))
				}
			}

			if found {
				ioutil.WriteFile(fmt.Sprintf("data/%03x.tmap", st), e.WRAM[0x12000:0x14000], 0644)
			}
		}
		return
	}

	entranceRooms := map[uint8][]uint16{
		0x00: {0x104},
		0x01: {},
		0x02: {0x12, 0x2, 0x11, 0x21, 0x22, 0x32, 0x42},
		0x03: {0x60, 0x50, 0x1, 0x72, 0x82, 0x81, 0x71, 0x70, 0x80, 0x52, 0x62, 0x61, 0x51, 0x41},
		0x04: {},
		0x05: {},
		0x06: {0xf0, 0xf1},
		0x07: {},
		0x08: {0xc9, 0xb9, 0xa9, 0xaa, 0xa8, 0xba, 0xb8, 0x99, 0xda, 0xd9, 0xd8, 0xc8, 0x89},
		0x09: {0x84, 0x74, 0x73, 0x83, 0x75, 0x85},
		0x0a: {},
		0x0b: {},
		0x0c: {0x63, 0x53, 0x43, 0x33},
		0x0d: {0xf2, 0xf3},
		0x0e: {},
		0x0f: {0xf4, 0xf5},
		0x10: {},
		0x11: {0xe3},
		0x12: {0xe2},
		0x13: {0xf8, 0xe8},
		0x14: {},
		0x15: {0x23, 0x24, 0x14, 0x4, 0xb5, 0xc5, 0xc4, 0xb4, 0xa4, 0xd5, 0x13, 0x15, 0xb6, 0xc6, 0xd6, 0xc7, 0xb7},
		0x16: {0xfb, 0xeb},
		0x17: {},
		0x18: {},
		0x19: {},
		0x1a: {0xfd, 0xed},
		0x1b: {},
		0x1c: {0xfe},
		0x1d: {0xee},
		0x1e: {0xff, 0xef},
		0x1f: {0xdf},
		0x20: {},
		0x21: {0xf9, 0xfa},
		0x22: {0xea},
		0x23: {},
		0x24: {0xe0, 0xd0, 0xc0, 0xb0, 0x40, 0x30, 0x20},
		0x25: {0x28, 0x38, 0x37, 0x36, 0x26, 0x76, 0x66, 0x16, 0x6, 0x35, 0x34, 0x54, 0x46},
		0x26: {0x4a, 0x9, 0x3a, 0xa, 0x4b, 0x3b, 0x2b, 0x2a, 0x1a, 0x6a, 0x5a, 0x19, 0x1b, 0xb},
		0x27: {0x98, 0xd2, 0xc2, 0xc1, 0xb1, 0xb2, 0xa2, 0x93, 0x92, 0x91, 0xa0, 0x90, 0xb3, 0xa3, 0xa1, 0xc3, 0xd1, 0x97},
		0x28: {0x56, 0x57},
		0x29: {},
		0x2a: {0x58, 0x67, 0x68},
		0x2b: {0x59, 0x49, 0x39, 0x29},
		0x2c: {0xe1},
		0x2d: {0xe, 0x1e, 0x3e, 0x4e, 0x6e, 0x5e, 0x7e, 0x9e, 0xbe, 0xce, 0xbf, 0x4f, 0x9f, 0xaf, 0xae, 0x8e, 0x7f, 0x5f, 0x3f, 0x1f, 0x2e},
		0x2e: {0xe6, 0xe7},
		0x2f: {},
		0x30: {0xe4, 0xe5},
		0x31: {},
		0x32: {0x55},
		0x33: {0x77, 0x31, 0x27, 0x17, 0xa7, 0x7, 0x87},
		0x34: {0xdb, 0xcb, 0xcc, 0xbc, 0xac, 0xbb, 0xab, 0x64, 0x65, 0x45, 0x44, 0xdc},
		0x35: {},
		0x36: {0x10},
		0x37: {0xc, 0x8c, 0x1c, 0x8b, 0x7b, 0x9b, 0x7d, 0x7c, 0x9c, 0x9d, 0x8d, 0x6b, 0x5b, 0x5c, 0x5d, 0x6d, 0x6c, 0xa5, 0x95, 0x96, 0x3d, 0x4d, 0xa6, 0x4c, 0x1d, 0xd},
		0x38: {0x8, 0x18},
		0x39: {0x2f},
		0x3a: {0x3c, 0x2c},
		0x3b: {},
		0x3c: {0x100},
		0x3d: {0x11e},
		0x3e: {0x101},
		0x3f: {},
		0x40: {0x102},
		0x41: {0x117},
		0x42: {0x103},
		0x43: {},
		0x44: {},
		0x45: {0x105},
		0x46: {0x11f},
		0x47: {0x106},
		0x48: {},
		0x49: {0x107},
		0x4a: {},
		0x4b: {0x108},
		0x4c: {0x109},
		0x4d: {0x10a},
		0x4e: {0x10b},
		0x4f: {0x10c},
		0x50: {},
		0x51: {0x11b},
		0x52: {},
		0x53: {0x11c},
		0x54: {},
		0x55: {},
		0x56: {0x120},
		0x57: {0x110},
		0x58: {0x112},
		0x59: {0x111},
		0x5a: {},
		0x5b: {0x113},
		0x5c: {0x114},
		0x5d: {0x115},
		0x5e: {},
		0x5f: {0x10d},
		0x60: {0x10f},
		0x61: {0x119, 0x11d},
		0x62: {},
		0x63: {0x116},
		0x64: {0x121},
		0x65: {0x122},
		0x66: {},
		0x67: {0x118},
		0x68: {0x11a},
		0x69: {0x10e},
		0x6a: {},
		0x6b: {},
		0x6c: {0x123},
		0x6d: {0x124},
		0x6e: {},
		0x6f: {0x125},
		0x70: {},
		0x71: {0x126},
		0x72: {},
		0x73: {},
		0x74: {},
		0x75: {},
		0x76: {},
		0x77: {},
		0x78: {},
		0x79: {},
		0x7a: {},
		0x7b: {0x0},
		0x7c: {},
		0x7d: {},
		0x7e: {},
		0x7f: {},
		0x80: {},
		0x81: {},
		0x82: {0x3},
		0x83: {0x127},
		0x84: {},
	}

	// iterate over entrances:
	wg := sync.WaitGroup{}
	for eID := uint8(0); eID < entranceCount; eID++ {
		fmt.Fprintf(e.Logger, "entrance $%02x\n", eID)

		// poke the entrance ID into our asm code:
		e.HWIO.Dyn[setEntranceIDPC-0x5000] = eID
		// load the entrance and draw the room:
		if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
			t.Fatal(err)
		}

		g := &entranceGroups[eID]
		g.EntranceID = eID
		g.Supertile = Supertile(e.ReadWRAM16(0xA0))

		g.Rooms = make([]*RoomState, 0, 0x20)

		// function to create a room and track it:
		createRoom := func(st Supertile) (room *RoomState) {
			var ok bool
			if room, ok = supertiles[st]; ok {
				fmt.Printf("    reusing room %s\n", st)
				//if eID != room.EntranceID {
				//	panic(fmt.Errorf("conflicting entrances for room %s", st))
				//}
				return
			}

			fmt.Printf("    creating room %s\n", st)

			// load and draw current supertile:
			write16(e.WRAM[:], 0xA0, uint16(st))
			if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
				panic(err)
			}

			//// load and draw current supertile:
			//write16(e.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, uint16(st))
			//if err = e.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
			//	panic(err)
			//}

			room = &RoomState{
				Supertile:         st,
				Rendered:          nil,
				Hookshot:          make(map[MapCoord]byte, 0x2000),
				TilesVisitedStar0: make(map[MapCoord]empty, 0x2000),
				TilesVisitedStar1: make(map[MapCoord]empty, 0x2000),
				TilesVisitedTag0:  make(map[MapCoord]empty, 0x2000),
				TilesVisitedTag1:  make(map[MapCoord]empty, 0x2000),
			}
			room.TilesVisited = room.TilesVisitedStar0
			wram := (&room.WRAM)[:]
			tiles := (&room.Tiles)[:]

			copy(room.VRAMTileSet[:], e.VRAM[0x4000:0x8000])
			copy(wram, e.WRAM[:])
			copy(tiles, e.WRAM[0x12000:0x14000])

			g.Rooms = append(g.Rooms, room)
			supertiles[st] = room

			//ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", uint16(st)), wram, 0644)
			ioutil.WriteFile(fmt.Sprintf("data/%03X.tmap", uint16(st)), tiles, 0644)

			if false {
				room.WarpExitTo = Supertile(read8(wram, 0xC000))
				room.StairExitTo = [4]Supertile{
					Supertile(read8(wram, uint32(0xC001))),
					Supertile(read8(wram, uint32(0xC002))),
					Supertile(read8(wram, uint32(0xC003))),
					Supertile(read8(wram, uint32(0xC004))),
				}
				room.WarpExitLayer = MapCoord(read8(wram, uint32(0x063C))&2) << 11
				room.StairTargetLayer = [4]MapCoord{
					MapCoord(read8(wram, uint32(0x063D))&2) << 11,
					MapCoord(read8(wram, uint32(0x063E))&2) << 11,
					MapCoord(read8(wram, uint32(0x063F))&2) << 11,
					MapCoord(read8(wram, uint32(0x0640))&2) << 11,
				}

				//fmt.Fprintf(s.Logger, "    WARPTO   = %s\n", Supertile(read8(wram, 0xC000)))
				//fmt.Fprintf(s.Logger, "    STAIR0TO = %s\n", Supertile(read8(wram, 0xC001)))
				//fmt.Fprintf(s.Logger, "    STAIR1TO = %s\n", Supertile(read8(wram, 0xC002)))
				//fmt.Fprintf(s.Logger, "    STAIR2TO = %s\n", Supertile(read8(wram, 0xC003)))
				//fmt.Fprintf(s.Logger, "    STAIR3TO = %s\n", Supertile(read8(wram, 0xC004)))
				//fmt.Fprintf(s.Logger, "    DARK     = %v\n", room.IsDarkRoom())

				// process doors first:
				doors := make([]Door, 0, 16)
				for m := 0; m < 16; m++ {
					tpos := read16(wram[:], uint32(0x19A0+(m<<1)))
					// stop marker:
					if tpos == 0 {
						//fmt.Fprintf(s.Logger, "    door stop at marker\n")
						break
					}

					door := Door{
						Pos:  MapCoord(tpos >> 1),
						Type: DoorType(read16(wram[:], uint32(0x1980+(m<<1)))),
						Dir:  Direction(read16(wram[:], uint32(0x19C0+(m<<1)))),
					}
					doors = append(doors, door)

					fmt.Fprintf(e.Logger, "    door: %v\n", door)

					isDoorEdge, _, _, _ := door.Pos.IsDoorEdge()

					{
						// open up doors that are in front of interroom stairwells:
						var stair MapCoord

						switch door.Dir {
						case DirNorth:
							stair = door.Pos + 0x01
							break
						case DirSouth:
							stair = door.Pos + 0xC1
							break
						case DirEast:
							stair = door.Pos + 0x43
							break
						case DirWest:
							stair = door.Pos + 0x40
							break
						}

						v := tiles[stair]
						if v >= 0x30 && v <= 0x39 {
							tiles[door.Pos+0x41+0x00] = 0x00
							tiles[door.Pos+0x41+0x01] = 0x00
							tiles[door.Pos+0x41+0x40] = 0x00
							tiles[door.Pos+0x41+0x41] = 0x00
						}
					}

					if door.Type.IsExit() {
						lyr, row, col := door.Pos.RowCol()
						// patch up the door tiles to prevent reachability from exiting:
						for y := uint16(0); y < 4; y++ {
							for x := uint16(0); x < 4; x++ {
								t := lyr | (row+y)<<6 | (col + x)
								if tiles[t] >= 0xF0 {
									tiles[t] = 0x00
								}
							}
						}
						continue
					}

					if door.Type == 0x30 {
						// exploding wall:
						pos := int(door.Pos)
						fmt.Printf("    exploding wall %s\n", door.Pos)
						for c := 0; c < 11; c++ {
							for r := 0; r < 12; r++ {
								tiles[pos+(r<<6)-c] = 0
								tiles[pos+(r<<6)+1+c] = 0
							}
						}
						continue
					}

					if isDoorEdge {
						// blow open edge doorways:
						var (
							start        MapCoord
							tn           MapCoord
							doorTileType uint8
							doorwayTile  uint8
							adj          int
						)

						var ok bool
						lyr, _, _ := door.Pos.RowCol()

						switch door.Dir {
						case DirNorth:
							start = door.Pos + 0x81
							doorwayTile = 0x80 | uint8(lyr>>10)
							adj = 1
							break
						case DirSouth:
							start = door.Pos + 0x41
							doorwayTile = 0x80 | uint8(lyr>>10)
							adj = 1
							break
						case DirEast:
							start = door.Pos + 0x42
							doorwayTile = 0x81 | uint8(lyr>>10)
							adj = 0x40
							break
						case DirWest:
							start = door.Pos + 0x42
							doorwayTile = 0x81 | uint8(lyr>>10)
							adj = 0x40
							break
						}

						doorTileType = tiles[start]
						if doorTileType < 0xF0 {
							// don't blow this doorway; it's custom:
							continue
						}

						tn = start
						canBlow := func(v uint8) bool {
							if v == 0x01 || v == 0x00 {
								return true
							}
							if v == doorTileType {
								return true
							}
							if v >= 0x28 && v <= 0x2B {
								return true
							}
							if v == 0x10 {
								// slope?? found in sanctuary $002
								return true
							}
							return false
						}
						for i := 0; i < 12; i++ {
							v := tiles[tn]
							if canBlow(v) {
								fmt.Printf("    blow open %s\n", tn)
								tiles[tn] = doorwayTile
								fmt.Printf("    blow open %s\n", MapCoord(int(tn)+adj))
								tiles[int(tn)+adj] = doorwayTile
							} else {
								panic(fmt.Errorf("something blocking the doorway at %s: $%02x", tn, v))
								break
							}

							tn, _, ok = tn.MoveBy(door.Dir, 1)
							if !ok {
								break
							}
						}
						continue
					}

					//if !isDoorEdge
					{
						var (
							start        MapCoord
							tn           MapCoord
							doorTileType uint8
							maxCount     int
							count        int
							doorwayTile  uint8
							adj          int
						)

						var ok bool
						lyr, _, _ := door.Pos.RowCol()

						switch door.Dir {
						case DirNorth:
							start = door.Pos + 0x81
							maxCount = 12
							doorwayTile = 0x80 | uint8(lyr>>10)
							adj = 1
							break
						case DirSouth:
							start = door.Pos + 0x41
							maxCount = 12
							doorwayTile = 0x80 | uint8(lyr>>10)
							adj = 1
							break
						case DirEast:
							start = door.Pos + 0x42
							maxCount = 10
							doorwayTile = 0x81 | uint8(lyr>>10)
							adj = 0x40
							break
						case DirWest:
							start = door.Pos + 0x42
							maxCount = 10
							doorwayTile = 0x81 | uint8(lyr>>10)
							adj = 0x40
							break
						}

						var mustStop func(uint8) bool

						doorTileType = tiles[start]
						if doorTileType >= 0x80 && doorTileType <= 0x8D {
							mustStop = func(v uint8) bool {
								if v == 0x01 {
									return false
								}
								if v >= 0x28 && v <= 0x2B {
									return false
								}
								if v == doorwayTile {
									return false
								}
								if v >= 0xF0 {
									return false
								}
								return true
							}
						} else if doorTileType >= 0xF0 {
							oppositeDoorType := uint8(0)
							if doorTileType >= 0xF8 {
								oppositeDoorType = doorTileType - 8
							} else if doorTileType >= 0xF0 {
								oppositeDoorType = doorTileType + 8
							}

							mustStop = func(v uint8) bool {
								if v == 0x01 {
									return false
								}
								if v == doorwayTile {
									return false
								}
								if v == oppositeDoorType {
									return false
								}
								if v == doorTileType {
									return false
								}
								if v >= 0x28 && v <= 0x2B {
									// ledge tiles can be found in doorway in fairy cave $008:
									return false
								}
								return true
							}
						} else {
							// bad door starter tile type
							fmt.Fprintf(e.Logger, fmt.Sprintf("unrecognized door tile at %s: $%02x\n", start, doorTileType))
							continue
						}

						// check many tiles behind door for opposite door tile:
						i := 0
						tn = start
						for ; i < maxCount; i++ {
							v := tiles[tn]
							if mustStop(v) {
								break
							}
							tn, _, ok = tn.MoveBy(door.Dir, 1)
							if !ok {
								break
							}
						}
						count = i

						// blow open the doorway:
						tn = start
						for i := 0; i < count; i++ {
							v := tiles[tn]
							if mustStop(v) {
								break
							}

							//fmt.Printf("    blow open %s\n", tn)
							tiles[tn] = doorwayTile
							//fmt.Printf("    blow open %s\n", mapCoord(int(tn)+adj))
							tiles[int(tn)+adj] = doorwayTile
							tn, _, _ = tn.MoveBy(door.Dir, 1)
						}
					}
				}
				room.Doors = doors

				// find layer-swap tiles in doorways:
				swapCount := read16(wram, 0x044E)
				room.SwapLayers = make(map[MapCoord]empty, swapCount*4)
				for i := uint16(0); i < swapCount; i += 2 {
					t := MapCoord(read16(wram, uint32(0x06C0+i)))

					// mark the 2x2 tile as a layer-swap:
					room.SwapLayers[t+0x00] = empty{}
					room.SwapLayers[t+0x01] = empty{}
					room.SwapLayers[t+0x40] = empty{}
					room.SwapLayers[t+0x41] = empty{}
					// have to put it on both layers? ew
					room.SwapLayers[t|0x1000+0x00] = empty{}
					room.SwapLayers[t|0x1000+0x01] = empty{}
					room.SwapLayers[t|0x1000+0x40] = empty{}
					room.SwapLayers[t|0x1000+0x41] = empty{}
				}

				// find interroom stair objects:
				stairCount := uint32(0)
				for _, n := range []uint32{0x0438, 0x043A, 0x047E, 0x0482, 0x0480, 0x0484, 0x04A2, 0x04A6, 0x04A4, 0x04A8} {
					index := uint32(read16(wram, n))
					if index > stairCount {
						stairCount = index
					}
				}
				for i := uint32(0); i < stairCount; i += 2 {
					t := MapCoord(read16(wram, 0x06B0+i))
					room.Stairs = append(room.Stairs, t)
					fmt.Fprintf(e.Logger, "    interroom stair at %s\n", t)
				}

				for i := uint32(0); i < 0x20; i += 2 {
					pos := MapCoord(read16(wram, 0x0540+i) >> 1)
					if pos == 0 {
						break
					}
					fmt.Printf(
						"    manipulable(%s): %02x, %04x\n",
						pos,
						i,
						read16(wram, 0x0500+i), // MANIPPROPS
					)
				}

				for i := uint32(0); i < 6; i++ {
					gt := read16(wram, 0x06E0+i<<1)
					if gt == 0 {
						break
					}

					fmt.Printf("    chest($%04x)\n", gt)

					if gt&0x8000 != 0 {
						// locked cell door:
						t := MapCoord((gt & 0x7FFF) >> 1)
						if tiles[t] == 0x58+uint8(i) {
							tiles[t+0x00] = 0x00
							tiles[t+0x01] = 0x00
							tiles[t+0x40] = 0x00
							tiles[t+0x41] = 0x00
						}
						if tiles[t|0x1000] == 0x58+uint8(i) {
							tiles[t|0x1000+0x00] = 0x00
							tiles[t|0x1000+0x01] = 0x00
							tiles[t|0x1000+0x40] = 0x00
							tiles[t|0x1000+0x41] = 0x00
						}
					}
				}
			}

			if false {
				// clear all enemy health to see if this triggers something:
				for i := uint32(0); i < 16; i++ {
					write8(room.WRAM[:], 0x0DD0+i, 0)
				}
				room.HandleRoomTags(e)
			}

			ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap", uint16(st)), (&room.Tiles)[:], 0644)

			return
		}

		{
			// if this is the entrance, Link should be already moved to his starting position:
			wram := (&e.WRAM)[:]
			linkX := read16(wram, 0x22)
			linkY := read16(wram, 0x20)
			linkLayer := read16(wram, 0xEE)
			g.EntryCoord = AbsToMapCoord(linkX, linkY, linkLayer)
			fmt.Fprintf(e.Logger, "  link coord = {%04x, %04x, %04x}\n", linkX, linkY, linkLayer)
		}

		for _, st := range entranceRooms[eID] {
			createRoom(Supertile(st))
		}

		// render all supertiles found:
		if len(g.Rooms) >= 1 {
			for _, room := range g.Rooms {
				//if room.Supertile == g.Supertile {
				//	// entrance supertile is already rendered
				//	continue
				//}

				fmt.Fprintf(e.Logger, "  render %s\n", room.Supertile)

				if true {
					// Change room overlay:
					fmt.Fprintf(e.Logger, "  apply room overlay\n")
					write8((&room.WRAM)[:], 0x10, 0x07)
					write8((&room.WRAM)[:], 0x11, 0x03)
					write8((&room.WRAM)[:], 0xB0, 0x00)
					copy((&e.WRAM)[:], (&room.WRAM)[:])
					e.HWIO.ResetDMA()
					e.HWIO.ResetPPU()
					if err = e.ExecAtFor(runMainRoutingPC, 0x10_0000); err != nil {
						fmt.Fprintf(e.Logger, "%v\n", err)
					}
					e.CPU.Reset()
					e.HWIO.ResetDMA()
					e.HWIO.ResetPPU()
					// only extract the tilemap changes:
					copy((&room.WRAM)[0x12000:0x14000], (&e.WRAM)[0x12000:0x14000])
					copy((&room.Tiles)[:], (&e.WRAM)[0x12000:0x14000])
				}

				if false {
					// loadSupertile:
					copy((&e.WRAM)[:], (&room.WRAM)[:])
					write16((&e.WRAM)[:], 0xA0, uint16(room.Supertile))
					if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
						t.Fatal(err)
					}
					copy((&room.VRAMTileSet)[:], (&e.VRAM)[0x4000:0x8000])
					copy((&room.WRAM)[:], (&e.WRAM)[:])
				}

				{
					wg.Add(1)
					go drawSupertile(&wg, room)

					// render VRAM BG tiles to a PNG:
					if false {
						cgram := (*(*[0x100]uint16)(unsafe.Pointer(&room.WRAM[0xC300])))[:]
						pal := cgramToPalette(cgram)

						tiles := 0x4000 / 32
						g := image.NewPaletted(image.Rect(0, 0, 16*8, (tiles/16)*8), pal)
						for t := 0; t < tiles; t++ {
							// palette 2
							z := uint16(t) | (2 << 10)
							draw4bppTile(
								g,
								z,
								(&room.VRAMTileSet)[:],
								t%16,
								t/16,
							)
						}

						if err = exportPNG(fmt.Sprintf("data/%03X.vram.png", uint16(room.Supertile)), g); err != nil {
							panic(err)
						}
					}
				}
			}
		}
	}

	wg.Wait()

	// condense all maps into one image:
	renderAll("eg1", entranceGroups, 0x00, 0x10)
	renderAll("eg2", entranceGroups, 0x10, 0x3)
	renderAll("all", entranceGroups, 0, 0x13)
}

func renderAll(fname string, entranceGroups []Entrance, rowStart int, rowCount int) {
	var err error

	const divider = 1
	supertilepx := 512 / divider

	wga := sync.WaitGroup{}

	all := image.NewNRGBA(image.Rect(0, 0, 0x10*supertilepx, (rowCount*0x10*supertilepx)/0x10))
	// clear the image and remove alpha layer
	draw.Draw(
		all,
		all.Bounds(),
		image.NewUniform(color.NRGBA{0, 0, 0, 255}),
		image.Point{},
		draw.Src)

	greenTint := image.NewUniform(color.NRGBA{0, 255, 0, 64})
	redTint := image.NewUniform(color.NRGBA{255, 0, 0, 56})
	cyanTint := image.NewUniform(color.NRGBA{0, 255, 255, 64})
	blueTint := image.NewUniform(color.NRGBA{0, 0, 255, 64})

	black := image.NewUniform(color.RGBA{0, 0, 0, 255})
	yellow := image.NewUniform(color.RGBA{255, 255, 0, 255})

	for i := range entranceGroups {
		g := &entranceGroups[i]
		for _, room := range g.Rooms {
			st := int(room.Supertile)
			stMap := room.Rendered
			if stMap == nil {
				continue
			}

			row := st/0x10 - rowStart
			col := st % 0x10
			if row < 0 || row >= rowCount {
				continue
			}

			wga.Add(1)
			go func(room *RoomState) {
				stx := col * supertilepx
				sty := row * supertilepx
				draw.Draw(
					all,
					image.Rect(stx, sty, stx+supertilepx, sty+supertilepx),
					stMap,
					image.Point{},
					draw.Src,
				)

				// highlight tiles that are reachable:
				const drawPitOverlays = true
				if drawOverlays {
					maxRange := 0x2000
					if room.IsDarkRoom() {
						maxRange = 0x1000
					}

					// draw supertile over pits, bombable floors, and warps:
					for j := range room.ExitPoints {
						ep := &room.ExitPoints[j]
						if !ep.WorthMarking {
							continue
						}

						_, er, ec := ep.Point.RowCol()
						x := int(ec) << 3
						y := int(er) << 3
						fd0 := font.Drawer{
							Dst:  all,
							Src:  black,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + x + 1), fixed.I(sty + y + 1)},
						}
						fd1 := font.Drawer{
							Dst:  all,
							Src:  yellow,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + x), fixed.I(sty + y)},
						}
						stStr := fmt.Sprintf("%02X", uint16(ep.Supertile))
						fd0.DrawString(stStr)
						fd1.DrawString(stStr)
					}

					// draw supertile over stairs:
					for j := range room.Stairs {
						sn := room.StairExitTo[j]
						_, er, ec := room.Stairs[j].RowCol()

						x := int(ec) << 3
						y := int(er) << 3
						fd0 := font.Drawer{
							Dst:  all,
							Src:  black,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + 8 + x + 1), fixed.I(sty - 8 + y + 1 + 12)},
						}
						fd1 := font.Drawer{
							Dst:  all,
							Src:  yellow,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + 8 + x), fixed.I(sty - 8 + y + 12)},
						}
						stStr := fmt.Sprintf("%02X", uint16(sn))
						fd0.DrawString(stStr)
						fd1.DrawString(stStr)
					}

					for t := 0; t < maxRange; t++ {
						v := room.Reachable[t]
						if v == 0x01 {
							continue
						}

						tt := MapCoord(t)
						lyr, tr, tc := tt.RowCol()
						overlay := greenTint
						if lyr != 0 {
							overlay = cyanTint
						}
						if v == 0x20 || v == 0x62 {
							overlay = redTint
						}

						x := int(tc) << 3
						y := int(tr) << 3
						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							overlay,
							image.Point{},
							draw.Over,
						)
					}

					for t, d := range room.Hookshot {
						_, tr, tc := t.RowCol()
						x := int(tc) << 3
						y := int(tr) << 3

						overlay := blueTint
						_ = d

						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							overlay,
							image.Point{},
							draw.Over,
						)
					}
				} else if drawPitOverlays {
					// only draw red pit overlays:
					maxRange := 0x2000
					if room.IsDarkRoom() {
						maxRange = 0x1000
					}

					for t := 0; t < maxRange; t++ {
						v := room.Tiles[t]
						if v != 0x20 {
							continue
						}

						tt := MapCoord(t)
						_, tr, tc := tt.RowCol()

						x := int(tc) << 3
						y := int(tr) << 3
						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							redTint,
							image.Point{},
							draw.Over,
						)
					}
				}

				wga.Done()
			}(room)
		}
	}
	wga.Wait()

	if err = exportPNG(fmt.Sprintf("data/%s.png", fname), all); err != nil {
		panic(err)
	}
}

type empty = struct{}

type Direction uint8

const (
	DirNorth Direction = iota
	DirSouth
	DirWest
	DirEast
	DirNone
)

func (d Direction) MoveEG2(s Supertile) (Supertile, bool) {
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

func (d Direction) Opposite() Direction {
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

func (d Direction) String() string {
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

func (d Direction) RotateCW() Direction {
	switch d {
	case DirNorth:
		return DirEast
	case DirEast:
		return DirSouth
	case DirSouth:
		return DirWest
	case DirWest:
		return DirNorth
	}
	return d
}

func (d Direction) RotateCCW() Direction {
	switch d {
	case DirNorth:
		return DirWest
	case DirWest:
		return DirSouth
	case DirSouth:
		return DirEast
	case DirEast:
		return DirNorth
	}
	return d
}

type Supertile uint16

func (s Supertile) String() string { return fmt.Sprintf("$%03x", uint16(s)) }

func (s Supertile) MoveBy(dir Direction) (sn Supertile, sd Direction, ok bool) {
	// don't move within EG2:
	if s&0xFF00 != 0 {
		ok = false
	}

	sn, sd, ok = s, dir, false
	switch dir {
	case DirNorth:
		sn = Supertile(uint16(s) - 0x10)
		ok = uint16(s)&0xF0 > 0
		break
	case DirSouth:
		sn = Supertile(uint16(s) + 0x10)
		ok = uint16(s)&0xF0 < 0xF0
		break
	case DirWest:
		sn = Supertile(uint16(s) - 1)
		ok = uint16(s)&0x0F > 0
		break
	case DirEast:
		sn = Supertile(uint16(s) + 1)
		ok = uint16(s)&0x0F < 0xF
		break
	}

	// don't cross EG maps:
	if sn&0xFF00 != 0 {
		ok = false
	}

	return
}

type Door struct {
	Type DoorType  // $1980
	Pos  MapCoord  // $19A0
	Dir  Direction // $19C0
}

func (d *Door) ContainsCoord(t MapCoord) bool {
	dl, dr, dc := d.Pos.RowCol()
	tl, tr, tc := t.RowCol()
	if tl != dl {
		return false
	}
	if tc < dc || tc >= dc+4 {
		return false
	}
	if tr < dr || dr >= dr+4 {
		return false
	}
	return true
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
		// bombable cave exit:
		return true
	}
	//if t == 0x2E {
	//	// bombable door exit(?):
	//	return true
	//}
	return false
}

func (t DoorType) IsLayer2() bool {
	if t == 0x02 {
		return true
	}
	if t == 0x04 {
		return true
	}
	if t == 0x06 {
		return true
	}
	if t == 0x0C {
		return true
	}
	if t == 0x10 {
		return true
	}
	if t == 0x24 {
		return true
	}
	if t == 0x26 {
		return true
	}
	if t == 0x3A {
		return true
	}
	if t == 0x3C {
		return true
	}
	if t == 0x3E {
		return true
	}
	if t == 0x40 {
		return true
	}
	if t == 0x44 {
		return true
	}
	if t >= 0x48 && t <= 0x66 {
		return true
	}
	return false
}

func (t DoorType) IsStairwell() bool {
	return t >= 0x20 && t <= 0x26
}

func (t DoorType) String() string {
	return fmt.Sprintf("$%02x", uint8(t))
}

type MapCoord uint16

func (t MapCoord) String() string {
	_, row, col := t.RowCol()
	return fmt.Sprintf("$%04x={%02x,%02x}", uint16(t), row, col)
}

func (t MapCoord) IsLayer2() bool {
	return t&0x1000 != 0
}

type LinkState uint8

const (
	StateWalk LinkState = iota
	StateFall
	StateSwim
	StatePipe
)

func (s LinkState) String() string {
	switch s {
	case StateWalk:
		return "walk"
	case StateFall:
		return "fall"
	case StateSwim:
		return "swim"
	case StatePipe:
		return "pipe"
	default:
		panic("bad LinkState")
	}
}

func AbsToMapCoord(absX, absY, layer uint16) MapCoord {
	// modeled after RoomTag_GetTilemapCoords#_01CDA5
	c := ((absY)&0x01F8)<<3 | ((absX)&0x01F8)>>3
	if layer != 0 {
		return MapCoord(c | 0x1000)
	}
	return MapCoord(c)
}

func (t MapCoord) ToAbsCoord(st Supertile) (x uint16, y uint16) {
	_, row, col := t.RowCol()
	x = col<<3 + 0x1
	y = row<<3 - 0xE

	// add absolute position from supertile:
	y += (uint16(st) & 0xF0) << 5
	x += (uint16(st) & 0x0F) << 9
	return
}

func (t MapCoord) MoveBy(dir Direction, increment int) (MapCoord, Direction, bool) {
	it := int(t)
	row := (it & 0xFC0) >> 6
	col := it & 0x3F

	// don't allow perpendicular movement along the outer edge
	// this prevents accidental/leaky flood fill along the edges
	if row == 0 || row == 0x3F {
		if dir != DirNorth && dir != DirSouth {
			return t, dir, false
		}
	}
	if col == 0 || col == 0x3F {
		if dir != DirWest && dir != DirEast {
			return t, dir, false
		}
	}

	switch dir {
	case DirNorth:
		if row >= 0+increment {
			return MapCoord(it - (increment << 6)), dir, true
		}
		return t, dir, false
	case DirSouth:
		if row <= 0x3F-increment {
			return MapCoord(it + (increment << 6)), dir, true
		}
		return t, dir, false
	case DirWest:
		if col >= 0+increment {
			return MapCoord(it - increment), dir, true
		}
		return t, dir, false
	case DirEast:
		if col <= 0x3F-increment {
			return MapCoord(it + increment), dir, true
		}
		return t, dir, false
	default:
		panic("bad direction")
	}

	return t, dir, false
}

func (t MapCoord) Row() MapCoord {
	return t & 0x0FFF >> 6
}

func (t MapCoord) Col() MapCoord {
	return t & 0x003F
}

func (t MapCoord) RowCol() (layer, row, col uint16) {
	layer = uint16(t & 0x1000)
	row = uint16((t & 0x0FFF) >> 6)
	col = uint16(t & 0x3F)
	return
}

func (t MapCoord) IsEdge() (ok bool, dir Direction, row, col uint16) {
	_, row, col = t.RowCol()
	if row == 0 {
		ok, dir = true, DirNorth
		return
	}
	if row == 0x3F {
		ok, dir = true, DirSouth
		return
	}
	if col == 0 {
		ok, dir = true, DirWest
		return
	}
	if col == 0x3F {
		ok, dir = true, DirEast
		return
	}
	return
}

func (t MapCoord) OnEdge(d Direction) MapCoord {
	lyr, row, col := t.RowCol()
	switch d {
	case DirNorth:
		return MapCoord(lyr | (0x00 << 6) | col)
	case DirSouth:
		return MapCoord(lyr | (0x3F << 6) | col)
	case DirWest:
		return MapCoord(lyr | (row << 6) | 0x00)
	case DirEast:
		return MapCoord(lyr | (row << 6) | 0x3F)
	default:
		panic("bad direction")
	}
	return t
}

func (t MapCoord) IsDoorEdge() (ok bool, dir Direction, row, col uint16) {
	_, row, col = t.RowCol()
	if row <= 0x08 {
		ok, dir = true, DirNorth
		return
	}
	if row >= 0x3F-8 {
		ok, dir = true, DirSouth
		return
	}
	if col <= 0x08 {
		ok, dir = true, DirWest
		return
	}
	if col >= 0x3F-8 {
		ok, dir = true, DirEast
		return
	}
	return
}

func (t MapCoord) OppositeDoorEdge() MapCoord {
	lyr, row, col := t.RowCol()
	if row <= 0x08 {
		return MapCoord(lyr | (0x3A << 6) | col)
	}
	if row >= 0x3F-8 {
		return MapCoord(lyr | (0x06 << 6) | col)
	}
	if col <= 0x08 {
		return MapCoord(lyr | (row << 6) | 0x3A)
	}
	if col >= 0x3F-8 {
		return MapCoord(lyr | (row << 6) | 0x06)
	}
	panic("not at an edge")
	return t
}

func (t MapCoord) FlipVertical() MapCoord {
	lyr, row, col := t.RowCol()
	row = 0x40 - row
	return MapCoord(lyr | (row << 6) | col)
}

func (t MapCoord) OppositeEdge() MapCoord {
	lyr, row, col := t.RowCol()
	if row == 0x00 {
		return MapCoord(lyr | (0x3F << 6) | col)
	}
	if row == 0x3F {
		return MapCoord(lyr | (0x00 << 6) | col)
	}
	if col == 0x00 {
		return MapCoord(lyr | (row << 6) | 0x3F)
	}
	if col == 0x3F {
		return MapCoord(lyr | (row << 6) | 0x00)
	}
	panic("not at an edge")
}

type ScanState struct {
	t MapCoord
	d Direction
	s LinkState
}

type ExitPoint struct {
	Supertile
	Point MapCoord
	Direction
	WorthMarking bool
}

type EntryPoint struct {
	Supertile
	Point MapCoord
	Direction
	//LinkState
	From ExitPoint
}

func (ep EntryPoint) String() string {
	//return fmt.Sprintf("{%s, %s, %s, %s}", ep.Supertile, ep.Point, ep.Direction, ep.LinkState)
	return fmt.Sprintf("{%s, %s, %s}", ep.Supertile, ep.Point, ep.Direction)
}

type RoomState struct {
	Supertile

	Rendered image.Image

	EntryPoints []EntryPoint
	ExitPoints  []ExitPoint

	WarpExitTo       Supertile
	StairExitTo      [4]Supertile
	WarpExitLayer    MapCoord
	StairTargetLayer [4]MapCoord

	Doors      []Door
	Stairs     []MapCoord
	SwapLayers map[MapCoord]empty // $06C0[size=$044E >> 1]

	TilesVisited map[MapCoord]empty

	TilesVisitedStar0 map[MapCoord]empty
	TilesVisitedStar1 map[MapCoord]empty
	TilesVisitedTag0  map[MapCoord]empty
	TilesVisitedTag1  map[MapCoord]empty

	Tiles     [0x2000]byte
	Reachable [0x2000]byte
	Hookshot  map[MapCoord]byte

	WRAM        [0x20000]byte
	VRAMTileSet [0x4000]byte

	markedPit   bool
	markedFloor bool
	lifoSpace   [0x2000]ScanState
	lifo        []ScanState
}

func (r *RoomState) push(s ScanState) {
	r.lifo = append(r.lifo, s)
}

func (r *RoomState) pushAllDirections(t MapCoord, s LinkState) {
	mn, ms, mw, me := false, false, false, false
	// can move in any direction:
	if tn, dir, ok := t.MoveBy(DirNorth, 1); ok {
		mn = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}
	if tn, dir, ok := t.MoveBy(DirWest, 1); ok {
		mw = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}
	if tn, dir, ok := t.MoveBy(DirEast, 1); ok {
		me = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}
	if tn, dir, ok := t.MoveBy(DirSouth, 1); ok {
		ms = true
		r.push(ScanState{t: tn, d: dir, s: s})
	}

	// check diagonals at pits; cannot squeeze between solid areas though:
	if mn && mw && r.canDiagonal(r.Tiles[t-0x40]) && r.canDiagonal(r.Tiles[t-0x01]) {
		r.push(ScanState{t: t - 0x41, d: DirNorth, s: s})
	}
	if mn && me && r.canDiagonal(r.Tiles[t-0x40]) && r.canDiagonal(r.Tiles[t+0x01]) {
		r.push(ScanState{t: t - 0x3F, d: DirNorth, s: s})
	}
	if ms && mw && r.canDiagonal(r.Tiles[t+0x40]) && r.canDiagonal(r.Tiles[t-0x01]) {
		r.push(ScanState{t: t + 0x3F, d: DirSouth, s: s})
	}
	if ms && me && r.canDiagonal(r.Tiles[t+0x40]) && r.canDiagonal(r.Tiles[t+0x01]) {
		r.push(ScanState{t: t + 0x41, d: DirSouth, s: s})
	}
}

func (r *RoomState) canDiagonal(v byte) bool {
	return v == 0x20 || // pit
		(v&0xF0 == 0xB0) // somaria/pipe
}

func (r *RoomState) IsDarkRoom() bool {
	return read8((&r.WRAM)[:], 0xC005) != 0
}

// isAlwaysWalkable checks if the tile is always walkable on, regardless of state
func (r *RoomState) isAlwaysWalkable(v uint8) bool {
	return v == 0x00 || // no collision
		v == 0x09 || // shallow water
		v == 0x22 || // manual stairs
		v == 0x23 || v == 0x24 || // floor switches
		(v >= 0x0D && v <= 0x0F) || // spikes / floor ice
		v == 0x3A || v == 0x3B || // star tiles
		v == 0x40 || // thick grass
		v == 0x4B || // warp
		v == 0x60 || // rupee tile
		(v >= 0x68 && v <= 0x6B) || // conveyors
		v == 0xA0 // north/south dungeon swap door (for HC to sewers)
}

// isMaybeWalkable checks if the tile could be walked on depending on what state it's in
func (r *RoomState) isMaybeWalkable(t MapCoord, v uint8) bool {
	return v&0xF0 == 0x70 || // pots/pegs/blocks
		v == 0x62 || // bombable floor
		v == 0x66 || v == 0x67 // crystal pegs (orange/blue):
}

func (r *RoomState) canHookThru(v uint8) bool {
	return v == 0x00 || // no collision
		v == 0x08 || v == 0x09 || // water
		(v >= 0x0D && v <= 0x0F) || // spikes / floor ice
		v == 0x1C || v == 0x0C || // layer pass through
		v == 0x20 || // pit
		v == 0x22 || // manual stairs
		v == 0x23 || v == 0x24 || // floor switches
		(v >= 0x28 && v <= 0x2B) || // ledge tiles
		v == 0x3A || v == 0x3B || // star tiles
		v == 0x40 || // thick grass
		v == 0x4B || // warp
		v == 0x60 || // rupee tile
		(v >= 0x68 && v <= 0x6B) || // conveyors
		v == 0xB6 || // somaria start
		v == 0xBC // somaria start
}

// isHookable determines if the tile can be attached to with a hookshot
func (r *RoomState) isHookable(v uint8) bool {
	return v == 0x27 || // general hookable object
		(v >= 0x58 && v <= 0x5D) || // chests (TODO: check $0500 table for kind)
		v&0xF0 == 0x70 // pot/peg/block
}

func (r *RoomState) FindReachableTiles(
	entryPoint EntryPoint,
	visit func(s ScanState, v uint8),
) {
	m := &r.Tiles

	// if we ever need to wrap
	f := visit

	r.lifo = r.lifoSpace[:0]
	r.push(ScanState{t: entryPoint.Point, d: entryPoint.Direction})

	// handle the stack of locations to traverse:
	for len(r.lifo) != 0 {
		lifoLen := len(r.lifo) - 1
		s := r.lifo[lifoLen]
		r.lifo = r.lifo[:lifoLen]

		if _, ok := r.TilesVisited[s.t]; ok {
			continue
		}

		v := m[s.t]

		if s.s == StatePipe {
			// allow 00 and 01 in pipes for TR $015 center area:
			if v == 0x00 || v == 0x01 {
				// continue in the same direction:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// straight:
			if v == 0xB0 || v == 0xB1 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// check for pipe exit 3 tiles in advance:
				// this is done to skip collision tiles between B0/B1 and BE
				if tn, dir, ok := s.t.MoveBy(s.d, 3); ok && m[tn] == 0xBE {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
					continue
				}

				// continue in the same direction:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					// if the pipe crosses another direction pipe skip over that bit of pipe:
					if m[tn] == v^0x01 {
						if tn, _, ok := tn.MoveBy(dir, 2); ok && v == m[tn] {
							r.push(ScanState{t: tn, d: dir, s: StatePipe})
							continue
						}
					}

					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// west to south or north to east:
			if v == 0xB2 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirWest {
					if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirNorth {
					if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}
			// south to east or west to north:
			if v == 0xB3 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirSouth {
					if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirWest {
					if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}
			// north to west or east to south:
			if v == 0xB4 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirNorth {
					if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirEast {
					if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}
			// east to north or south to west:
			if v == 0xB5 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if s.d == DirEast {
					if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				} else if s.d == DirSouth {
					if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
				}
				continue
			}

			// line exit:
			if v == 0xB6 {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// check for 2 pit tiles beyond exit:
				t := s.t
				var ok bool
				if t, _, ok = t.MoveBy(s.d, 1); !ok || m[t] != 0x20 {
					continue
				}
				if t, _, ok = t.MoveBy(s.d, 1); !ok || m[t] != 0x20 {
					continue
				}

				// continue in the same direction but not in pipe-follower state:
				if tn, dir, ok := t.MoveBy(s.d, 1); ok && m[tn] == 0x00 {
					r.push(ScanState{t: tn, d: dir})
				}
				continue
			}

			// south, west, east junction:
			if v == 0xB7 {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// north, west, east junction:
			if v == 0xB8 {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// north, east, south junction:
			if v == 0xB9 {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// north, west, south junction:
			if v == 0xBA {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// 4-way junction:
			if v == 0xBB {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// possible exit:
			if v == 0xBC {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				// continue in the same direction:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}

				// check for exits across pits:
				if tn, dir, ok := s.t.MoveBy(s.d.RotateCW(), 1); ok && m[tn] == 0x20 {
					if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x20 {
						if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x00 {
							r.push(ScanState{t: tn, d: dir})
						}
					}
				}
				if tn, dir, ok := s.t.MoveBy(s.d.RotateCCW(), 1); ok && m[tn] == 0x20 {
					if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x20 {
						if tn, _, ok = tn.MoveBy(dir, 1); ok && m[tn] == 0x00 {
							r.push(ScanState{t: tn, d: dir})
						}
					}
				}

				continue
			}

			// cross-over:
			if v == 0xBD {
				// do not mark as visited in case we cross from the other direction later:
				//r.TilesVisited[s.t] = empty{}
				f(s, v)

				// continue in the same direction:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StatePipe})
				}
				continue
			}

			// pipe exit:
			if v == 0xBE {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// continue in the same direction but not in pipe-follower state:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
				continue
			}

			continue
		}

		if s.s == StateSwim {
			if s.t&0x1000 == 0 {
				panic("swimming in layer 1!")
			}

			if v == 0x02 || v == 0x03 {
				// collision:
				r.TilesVisited[s.t] = empty{}
				continue
			}

			if v == 0x0A {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// flip to walking:
				t := s.t & ^MapCoord(0x1000)
				if tn, dir, ok := t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StateWalk})
					continue
				}
				continue
			}

			if v == 0x1D {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// flip to walking:
				t := s.t & ^MapCoord(0x1000)
				if tn, dir, ok := t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StateWalk})
					continue
				}
				continue
			}

			if v == 0x3D {
				r.TilesVisited[s.t] = empty{}
				f(s, v)

				// flip to walking:
				t := s.t & ^MapCoord(0x1000)
				if tn, dir, ok := t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir, s: StateWalk})
					continue
				}
				continue
			}

			// can swim over mostly everything on layer 2:
			r.TilesVisited[s.t] = empty{}
			f(s, v)
			r.pushAllDirections(s.t, StateSwim)
			continue
		}

		if v == 0x08 {
			// deep water:
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// flip to swimming layer and state:
			t := s.t ^ 0x1000
			if m[t] != 0x1C && m[t] != 0x0D {
				r.push(ScanState{t: t, d: s.d, s: StateSwim})
			}

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir, s: StateWalk})
				continue
			}
			continue
		}

		if r.isAlwaysWalkable(v) || r.isMaybeWalkable(s.t, v) {
			// no collision:
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// can move in any direction:
			r.pushAllDirections(s.t, StateWalk)

			// check for water below us:
			t := s.t | 0x1000
			if t != s.t && m[t] == 0x08 {
				if v != 0x08 && v != 0x0D {
					r.pushAllDirections(t, StateSwim)
				}
			}
			continue
		}

		if v == 0x0A {
			// deep water ladder:
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// transition to swim state on other layer:
			t := s.t | 0x1000
			r.TilesVisited[t] = empty{}
			f(ScanState{t: t, d: s.d, s: StateSwim}, v)

			if tn, dir, ok := t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir, s: StateSwim})
			}
			continue
		}

		// layer pass through:
		if v == 0x1C {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if s.t&0x1000 == 0 {
				// $1C falling onto $0C means scrolling floor:
				if m[s.t|0x1000] == 0x0C {
					// treat as regular floor:
					r.pushAllDirections(s.t, StateWalk)
				} else {
					// drop to lower layer:
					r.push(ScanState{t: s.t | 0x1000, d: s.d})
				}
			}

			// detect a hookable tile across this pit:
			r.scanHookshot(s.t, s.d)

			continue
		} else if v == 0x0C {
			panic(fmt.Errorf("what to do for $0C at %s", s.t))
		}

		// north-facing stairs:
		if v == 0x1D {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}
		// north-facing stairs, layer changing:
		if v >= 0x1E && v <= 0x1F {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 2); ok {
				// swap layers:
				tn ^= 0x1000
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// pit:
		if v == 0x20 {
			// Link can fall into pit but cannot move beyond it:

			// don't mark as visited since it's possible we could also fall through this pit tile from above
			// TODO: fix this to accommodate both position and direction in the visited[] check and introduce
			// a Falling direction
			//r.TilesVisited[s.t] = empty{}
			f(s, v)

			// check what's beyond the pit:
			func() {
				t := s.t
				var ok bool
				if t, _, ok = t.MoveBy(s.d, 1); !ok || m[t] != 0x20 {
					return
				}
				if t, _, ok = t.MoveBy(s.d, 1); !ok {
					return
				}

				v = m[t]

				// somaria line start:
				if v == 0xB6 || v == 0xBC {
					r.TilesVisited[t] = empty{}
					f(ScanState{t: t, d: s.d}, v)

					// find corresponding B0..B1 directional line to follow:
					if tn, dir, ok := t.MoveBy(DirNorth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					if tn, dir, ok := t.MoveBy(DirWest, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					if tn, dir, ok := t.MoveBy(DirEast, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					if tn, dir, ok := t.MoveBy(DirSouth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
						r.push(ScanState{t: tn, d: dir, s: StatePipe})
					}
					return
				}
			}()

			// detect a hookable tile across this pit:
			r.scanHookshot(s.t, s.d)

			continue
		}

		// ledge tiles:
		if v >= 0x28 && v <= 0x2B {
			// ledge much not be approached from its perpendicular direction:
			ledgeDir := Direction(v - 0x28)
			if ledgeDir != s.d && ledgeDir != s.d.Opposite() {
				continue
			}

			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// check for hookable tiles across from this ledge:
			r.scanHookshot(s.t, s.d)

			// check 4 tiles from ledge for pit:
			t, dir, ok := s.t.MoveBy(s.d, 4)
			if !ok {
				continue
			}

			// pit tile on same layer?
			v = m[t]
			if v == 0x20 {
				// visit it next:
				r.push(ScanState{t: t, d: dir})
			} else if v == 0x1C { // or 0x0C ?
				// swap layers:
				t ^= 0x1000

				// check again for pit tile on the opposite layer:
				v = m[t]
				if v == 0x20 {
					// visit it next:
					r.push(ScanState{t: t, d: dir})
				}
			} else if v == 0x0C {
				panic(fmt.Errorf("TODO handle $0C in pit case t=%s", t))
			} else if v == 0x00 {
				// open floor:
				r.push(ScanState{t: t, d: dir})
			}

			continue
		}

		// interroom stair exits:
		if v >= 0x30 && v <= 0x37 {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// don't continue beyond a staircase unless it's our entry point:
			if len(r.lifo) == 0 {
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			}
			continue
		}

		// 38=Straight interroom stairs north/down edge (39= south/up edge):
		if v == 0x38 || v == 0x39 {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// don't continue beyond a staircase unless it's our entry point:
			if len(r.lifo) == 0 {
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			}
			continue
		}

		// south-facing single-layer auto stairs:
		if v == 0x3D {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}
		// south-facing layer-swap auto stairs:
		if v >= 0x3E && v <= 0x3F {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 2); ok {
				// swap layers:
				tn ^= 0x1000
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// spiral staircase:
		// $5F is the layer 2 version of $5E (spiral staircase)
		if v == 0x5E || v == 0x5F {
			r.TilesVisited[s.t] = empty{}
			f(s, m[s.t])

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// doorways:
		if v >= 0x80 && v <= 0x87 {
			if v&1 == 0 {
				// north-south
				if s.d == DirNone {
					// scout in both directions:
					if _, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					if _, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					continue
				}

				if s.d != DirNorth && s.d != DirSouth {
					panic(fmt.Errorf("north-south door approached from perpendicular direction %s at %s", s.d, s.t))
				}

				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
					// don't move past door edge:
					continue
				}
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			} else {
				// east-west
				if s.d == DirNone {
					// scout in both directions:
					if _, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					if _, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						r.push(ScanState{t: s.t, d: dir})
					}
					continue
				}

				if s.d != DirEast && s.d != DirWest {
					panic(fmt.Errorf("east-west door approached from perpendicular direction %s at %s", s.d, s.t))
				}

				r.TilesVisited[s.t] = empty{}
				f(s, v)

				if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
					// don't move past door edge:
					continue
				}
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
			}
			continue
		}
		// east-west teleport door
		if v == 0x89 {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
				// don't move past door edge:
				continue
			}
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}
		// entrance door (8E = north-south?, 8F = east-west??):
		if v == 0x8E || v == 0x8F {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if s.d == DirNone {
				// scout in both directions:
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					r.push(ScanState{t: tn, d: dir})
				}
				continue
			}

			if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
				// don't move past door edge:
				continue
			}
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// Layer/dungeon toggle doorways:
		if v >= 0x90 && v <= 0xAF {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if ok, edir, _, _ := s.t.IsDoorEdge(); ok && edir == s.d {
				// don't move past door edge:
				continue
			}
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				r.push(ScanState{t: tn, d: dir})
			}
			continue
		}

		// TR pipe entrance:
		if v == 0xBE {
			r.TilesVisited[s.t] = empty{}
			f(s, v)

			// find corresponding B0..B1 directional pipe to follow:
			if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				// skip over 2 tiles
				if tn, dir, ok = tn.MoveBy(dir, 2); !ok {
					continue
				}
				r.push(ScanState{t: tn, d: dir, s: StatePipe})
			}
			continue
		}

		// doors:
		if v >= 0xF0 {
			// determine a direction to head in if we have none:
			if s.d == DirNone {
				var ok bool
				var t MapCoord
				if t, _, ok = s.t.MoveBy(DirNorth, 1); ok && m[t] == 0x00 {
					s.d = DirSouth
				} else if t, _, ok = s.t.MoveBy(DirEast, 1); ok && m[t] == 0x00 {
					s.d = DirWest
				} else if t, _, ok = s.t.MoveBy(DirSouth, 1); ok && m[t] == 0x00 {
					s.d = DirNorth
				} else if t, _, ok = s.t.MoveBy(DirWest, 1); ok && m[t] == 0x00 {
					s.d = DirEast
				} else {
					// maybe we're too far in the door:
					continue
				}

				r.push(ScanState{t: t, d: s.d})
				continue
			}

			r.TilesVisited[s.t] = empty{}
			f(s, v)

			if t, _, ok := s.t.MoveBy(s.d, 2); ok {
				r.push(ScanState{t: t, d: s.d})
			}
			continue
		}

		// anything else is considered solid:
		//r.TilesVisited[s.t] = empty{}
		continue
	}
}

func (r *RoomState) scanHookshot(t MapCoord, d Direction) {
	var ok bool
	i := 0
	pt := t
	st := t
	shot := false

	m := &r.Tiles

	// estimating 0x10 8x8 tiles horizontally/vertically as max stretch of hookshot:
	const maxTiles = 0x10

	if m[t] >= 0x28 && m[t] <= 0x2B {
		// find opposite ledge first:
		ledgeTile := m[t]
		for ; i < maxTiles; i++ {
			// advance 1 tile:
			if t, _, ok = t.MoveBy(d, 1); !ok {
				return
			}

			if m[t] == ledgeTile {
				break
			}
		}
		if m[t] != ledgeTile {
			return
		}
	}

	for ; i < maxTiles; i++ {
		// the previous tile technically doesn't need to be walkable but it prevents
		// infinite loops due to not taking direction into account in the visited[] map
		// and not marking pit tiles as visited
		if r.isHookable(m[t]) && r.isAlwaysWalkable(m[pt]) {
			shot = true
			r.push(ScanState{t: pt, d: d})
			break
		}

		if !r.canHookThru(m[t]) {
			return
		}

		// advance 1 tile:
		pt = t
		if t, _, ok = t.MoveBy(d, 1); !ok {
			return
		}
	}

	if shot {
		// mark range as hookshot:
		t = st
		for j := 0; j < i; j++ {
			r.Hookshot[t] |= 1 << d

			if t, _, ok = t.MoveBy(d, 1); !ok {
				return
			}
		}
	}
}

func (room *RoomState) HandleRoomTags(e *System) bool {
	// if no tags present, don't check them:
	oldAE, oldAF := read8(room.WRAM[:], 0xAE), read8(room.WRAM[:], 0xAF)
	if oldAE == 0 && oldAF == 0 {
		return false
	}

	old04BC := read8(room.WRAM[:], 0x04BC)

	// prepare emulator for execution within this supertile:
	copy(e.WRAM[:], room.WRAM[:])
	copy(e.WRAM[0x12000:0x14000], room.Tiles[:])

	if room.Supertile == 0x07C {
		e.LoggerCPU = e.Logger
	}
	if err := e.ExecAt(b00HandleRoomTagsPC, 0); err != nil {
		panic(err)
	}
	e.LoggerCPU = nil

	// update room state:
	copy(room.WRAM[:], e.WRAM[:])
	copy(room.Tiles[:], e.WRAM[0x12000:0x14000])

	// if $AE or $AF (room tags) are modified, then the tag was activated:
	newAE, newAF := read8(room.WRAM[:], 0xAE), read8(room.WRAM[:], 0xAF)
	if newAE != oldAE || newAF != oldAF {
		return true
	}

	new04BC := read8(room.WRAM[:], 0x04BC)
	if new04BC != old04BC {
		return true
	}

	return false
}

type Entrance struct {
	EntranceID uint8
	Supertile

	EntryCoord MapCoord

	Rooms      []*RoomState
	Supertiles map[Supertile]*RoomState
}

func drawSupertile(wg *sync.WaitGroup, room *RoomState) {
	var err error
	defer wg.Done()

	// gfx output is:
	//  s.VRAM: $4000[0x2000] = 4bpp tile graphics
	//  s.WRAM: $2000[0x2000] = BG1 64x64 tile map  [64][64]uint16
	//  s.WRAM: $4000[0x2000] = BG2 64x64 tile map  [64][64]uint16
	//  s.WRAM:$12000[0x1000] = BG1 64x64 tile type [64][64]uint8
	//  s.WRAM:$12000[0x1000] = BG2 64x64 tile type [64][64]uint8
	//  s.WRAM: $C300[0x0200] = CGRAM palette

	wram := (&room.WRAM)[:]

	// assume WRAM has rendering state as well:
	isDark := room.IsDarkRoom()

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)

	cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
	pal := cgramToPalette(cgram)

	// render BG image:
	if room.Rendered == nil {
		g := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		bg1 := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
		bg2 := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
		doBG2 := !isDark

		bg1wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x2000])))[:]
		bg2wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x4000])))[:]
		tileset := (&room.VRAMTileSet)[:]

		//subdes := read8(wram, 0x1D)
		n0414 := read8(wram, 0x0414)
		translucent := n0414 == 0x07
		halfColor := n0414 == 0x04
		flip := n0414 == 0x03
		if translucent || halfColor {
			// render bg1 and bg2 separately

			// draw from back to front order:
			// BG2 priority 0:
			if doBG2 {
				renderBG(bg2, bg2wram, tileset, 0)
			}

			// BG1 priority 0:
			renderBG(bg1, bg1wram, tileset, 0)

			// BG2 priority 1:
			if doBG2 {
				renderBG(bg2, bg2wram, tileset, 1)
			}

			// BG1 priority 1:
			renderBG(bg1, bg1wram, tileset, 1)

			// combine bg1 and bg2:
			sat := func(v uint32) uint16 {
				if v > 0xffff {
					return 0xffff
				}
				return uint16(v)
			}

			if halfColor {
				// color math: add, half
				for y := 0; y < 512; y++ {
					for x := 0; x < 512; x++ {
						if bg2.ColorIndexAt(x, y) != 0 {
							r1, g1, b1, _ := bg1.At(x, y).RGBA()
							r2, g2, b2, _ := bg2.At(x, y).RGBA()
							c := color.RGBA64{
								R: sat(r1>>1 + r2>>1),
								G: sat(g1>>1 + g2>>1),
								B: sat(b1>>1 + b2>>1),
								A: 0xffff,
							}
							g.Set(x, y, c)
						} else {
							g.Set(x, y, bg1.At(x, y))
						}
					}
				}
			} else {
				// color math: add
				for y := 0; y < 512; y++ {
					for x := 0; x < 512; x++ {
						r1, g1, b1, _ := bg1.At(x, y).RGBA()
						r2, g2, b2, _ := bg2.At(x, y).RGBA()
						c := color.RGBA64{
							R: sat(r1 + r2),
							G: sat(g1 + g2),
							B: sat(b1 + b2),
							A: 0xffff,
						}
						g.Set(x, y, c)
					}
				}
			}
		} else if flip {
			// draw from back to front order:

			// BG1 priority 1:
			renderBG(bg1, bg1wram, tileset, 1)

			// BG1 priority 0:
			renderBG(bg1, bg1wram, tileset, 0)

			// BG2 priority 1:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 1)
			}

			// BG2 priority 0:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 0)
			}

			draw.Draw(g, g.Bounds(), bg1, image.Point{}, draw.Src)
		} else {
			// draw from back to front order:
			// BG2 priority 0:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 0)
			}

			// BG1 priority 0:
			renderBG(bg1, bg1wram, tileset, 0)

			// BG2 priority 1:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 1)
			}

			// BG1 priority 1:
			renderBG(bg1, bg1wram, tileset, 1)

			draw.Draw(g, g.Bounds(), bg1, image.Point{}, draw.Src)
		}

		//if isDark {
		//	// darken the room
		//	draw.Draw(
		//		g,
		//		g.Bounds(),
		//		image.NewUniform(color.RGBA64{0, 0, 0, 0x8000}),
		//		image.Point{},
		//		draw.Over,
		//	)
		//}

		// INIDISP contains PPU brightness
		brightness := read8(wram, 0x13) & 0xF
		if brightness < 15 {
			draw.Draw(
				g,
				g.Bounds(),
				image.NewUniform(color.RGBA64{0, 0, 0, uint16(brightness) << 12}),
				image.Point{},
				draw.Over,
			)
		}

		// draw supertile number in top-left:
		stStr := fmt.Sprintf("%03X", uint16(room.Supertile))
		(&font.Drawer{
			Dst:  g,
			Src:  image.NewUniform(color.RGBA{0, 0, 0, 255}),
			Face: inconsolata.Bold8x16,
			Dot:  fixed.Point26_6{fixed.I(5), fixed.I(5 + 12)},
		}).DrawString(stStr)
		(&font.Drawer{
			Dst:  g,
			Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
			Face: inconsolata.Bold8x16,
			Dot:  fixed.Point26_6{fixed.I(4), fixed.I(4 + 12)},
		}).DrawString(stStr)

		// store full underworld rendering for inclusion into EG map:
		room.Rendered = g

		if err = exportPNG(fmt.Sprintf("data/%03X.png", uint16(room.Supertile)), g); err != nil {
			panic(err)
		}
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

	bo := bufio.NewWriterSize(po, 8*1024*1024)

	err = png.Encode(bo, g)
	if err != nil {
		return
	}

	err = bo.Flush()
	if err != nil {
		return
	}

	return
}

var gammaRamp = [...]uint8{
	0x00, 0x01, 0x03, 0x06, 0x0a, 0x0f, 0x15, 0x1c,
	0x24, 0x2d, 0x37, 0x42, 0x4e, 0x5b, 0x69, 0x78,
	0x88, 0x90, 0x98, 0xa0, 0xa8, 0xb0, 0xb8, 0xc0,
	0xc8, 0xd0, 0xd8, 0xe0, 0xe8, 0xf0, 0xf8, 0xff,
}

func cgramToPalette(cgram []uint16) color.Palette {
	pal := make(color.Palette, 256)
	for i, bgr15 := range cgram {
		// convert BGR15 color format (MSB unused) to RGB24:
		b := (bgr15 & 0x7C00) >> 10
		g := (bgr15 & 0x03E0) >> 5
		r := bgr15 & 0x001F
		if false {
			pal[i] = color.NRGBA{
				R: gammaRamp[r],
				G: gammaRamp[g],
				B: gammaRamp[b],
				A: 0xff,
			}
		} else {
			pal[i] = color.NRGBA{
				R: uint8(r<<3 | r>>2),
				G: uint8(g<<3 | g>>2),
				B: uint8(b<<3 | b>>2),
				A: 0xff,
			}
		}
	}
	return pal
}

func renderBG(g *image.Paletted, bg []uint16, tiles []uint8, prio uint8) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			z := bg[a]
			a++

			// priority check:
			if (z&0x2000 != 0) != (prio != 0) {
				continue
			}

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

func (s *System) ExecAtFor(startPC uint32, maxCycles uint64) (err error) {
	var stopPC uint32
	var expectedPC uint32
	var cycles uint64

	s.SetPC(startPC)

	if stopPC, expectedPC, cycles = s.RunUntil(0, maxCycles); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		return
	}

	return
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
			//case 2:
			//	panic("mode 2!!!")
			//case 3:
			//	panic("mode 3!!!")
			//case 4:
			//	panic("mode 4!!!")
			//case 5:
			//	panic("mode 5!!!")
			//case 6:
			//	panic("mode 6!!!")
			//case 7:
			//	panic("mode 7!!!")
			default:
				break copyloop
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

	DisableDMA bool

	ppu struct {
		incrMode      bool   // false = increment after $2118, true = increment after $2119
		incrAmt       uint16 // 1, 32, or 128
		addrRemapping byte
		addr          uint16
	}
	// mapped to $5000-$7FFF
	Dyn [0x3000]byte
}

func (h *HWIO) ResetDynRAM() {
	h.Dyn = [0x3000]byte{}
}

func (h *HWIO) ResetDMA() {
	h.dmaregs = [8]DMARegs{}
	h.dma = [8]DMAChannel{}
}

func (h *HWIO) ResetPPU() {
	h.ppu.incrMode = false
	h.ppu.incrAmt = 0
	h.ppu.addrRemapping = 0
	h.ppu.addr = 0
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
		if !h.DisableDMA {
			for c := range h.dma {
				if hdmaen&(1<<c) == 0 {
					continue
				}

				// channel enabled:
				h.dma[c].Transfer(&h.dmaregs[c], c, h)
			}
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
			fmt.Printf("unsupported VRAM address remapping mode %d\n", h.ppu.addrRemapping)
		}
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMAIN = $%02x\n", value)
		//}
		return
	}
	if offs == 0x2116 {
		// VMADDL
		h.ppu.addr = uint16(value) | h.ppu.addr&0xFF00
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMADDL = $%04x\n", h.ppu.addr)
		//}
		return
	}
	if offs == 0x2117 {
		// VMADDH
		h.ppu.addr = uint16(value)<<8 | h.ppu.addr&0x00FF
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

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x\n", offs, value)
	//}
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
