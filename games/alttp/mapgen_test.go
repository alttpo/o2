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

func TestGenerateMap(t *testing.T) {
	var err error

	var f *os.File
	f, err = os.Open("alttp-jp.sfc")
	if err != nil {
		t.Skip(err)
	}

	var s *System

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

	//RoomsWithPitDamage#_00990C [0x70]uint16
	roomsWithPitDamage := make(map[Supertile]bool, 0x128)
	for i := Supertile(0); i < 0x128; i++ {
		roomsWithPitDamage[i] = false
	}
	for i := 0; i < 0x70; i++ {
		romaddr, _ := lorom.BusAddressToPak(0x00_990C)
		st := Supertile(read16(s.ROM[:], romaddr+uint32(i)<<1))
		roomsWithPitDamage[st] = true
	}

	const entranceCount = 0x85
	entranceGroups := make([]Entrance, entranceCount)
	supertiles := make(map[Supertile]*RoomState, 0x128)

	// iterate over entrances:
	wg := sync.WaitGroup{}
	for eID := uint8(0); eID < entranceCount; eID++ {
		fmt.Fprintf(s.Logger, "entrance $%02x\n", eID)

		// poke the entrance ID into our asm code:
		s.HWIO.Dyn[setEntranceIDPC-0x5000] = eID
		// load the entrance and draw the room:
		if err = s.ExecAt(loadEntrancePC, donePC); err != nil {
			t.Fatal(err)
		}

		g := &entranceGroups[eID]
		g.EntranceID = eID
		g.Supertile = Supertile(s.ReadWRAM16(0xA0))

		g.Rooms = make([]*RoomState, 0, 0x20)

		// function to create a room and track it:
		createRoom := func(st Supertile) (room *RoomState) {
			var ok bool
			if room, ok = supertiles[st]; ok {
				//fmt.Printf("reusing room %s\n", st)
				//if eID != room.EntranceID {
				//	panic(fmt.Errorf("conflicting entrances for room %s", st))
				//}
				return room
			}

			fmt.Printf("    creating room %s\n", st)
			room = &RoomState{
				Supertile:    st,
				Rendered:     nil,
				TilesVisited: make(map[MapCoord]empty, 0x2000),
			}

			// make a map full of $01 Collision and carve out reachable areas:
			for i := range room.Reachable {
				room.Reachable[i] = 0x01
			}

			// load and draw current supertile:
			write16(s.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, uint16(st))
			if err = s.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
				panic(err)
			}

			wram := (&room.WRAM)[:]
			tiles := (&room.Tiles)[:]

			copy(room.VRAMTileSet[:], s.VRAM[0x4000:0x8000])
			copy(wram, s.WRAM[:])
			copy(tiles, s.WRAM[0x12000:0x14000])

			g.Rooms = append(g.Rooms, room)
			supertiles[st] = room

			ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", uint16(st)), wram, 0644)
			ioutil.WriteFile(fmt.Sprintf("data/%03X.tmap", uint16(st)), tiles, 0644)

			// process doors first:
			doors := make([]Door, 0, 16)
			for m := 0; m < 16; m++ {
				tpos := read16(wram[:], uint32(0x19A0+(m<<1)))
				// stop marker:
				if tpos == 0 {
					break
				}

				door := Door{
					Pos:  MapCoord(tpos >> 1),
					Type: DoorType(read16(wram[:], uint32(0x1980+(m<<1)))),
					Dir:  Direction(read16(wram[:], uint32(0x19C0+(m<<1)))),
				}

				fmt.Fprintf(s.Logger, "    door: %v\n", door)

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
				} else if isDoorEdge, _, _, _ := door.Pos.IsDoorEdge(); !isDoorEdge {
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
							if v == doorwayTile {
								return false
							}
							if v >= 0xF0 {
								return false
							}
							return true
						}
					} else if doorTileType < 0xF0 {
						continue
					} else {
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
							return true
						}
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

				doors = append(doors, door)
			}
			room.Doors = doors

			ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap", uint16(st)), room.Tiles[:], 0644)

			return
		}

		// if this is the entrance, Link should be already moved to his starting position:
		linkX := read16((&s.WRAM)[:], 0x22)
		linkY := read16((&s.WRAM)[:], 0x20)
		linkLayer := read16((&s.WRAM)[:], 0xEE)
		g.EntryCoord = AbsToMapCoord(linkX, linkY, linkLayer)
		fmt.Fprintf(s.Logger, "  link coord = {%04x, %04x, %04x}\n", linkX, linkY, linkLayer)

		{
			room := createRoom(g.Supertile)
			// render the entrance supertile in the background:
			wg.Add(1)
			go drawSupertile(&wg, room)
		}

		// build a stack (LIFO) of supertile entry points to visit:
		lifo := make([]EntryPoint, 0, 0x100)
		// TODO: determine entrance direction
		lifo = append(lifo, EntryPoint{g.Supertile, g.EntryCoord, DirNone})

		// process the LIFO:
		for len(lifo) != 0 {
			// pop off the stack:
			lifoEnd := len(lifo) - 1
			ep := lifo[lifoEnd]
			lifo = lifo[0:lifoEnd]

			this := ep.Supertile

			fmt.Fprintf(s.Logger, "  ep = %s\n", ep)

			// create a room by emulation:
			room := createRoom(this)

			wram := (&room.WRAM)[:]

			//WARPTO   = $7EC000
			warpExitTo := Supertile(read8(wram[:], 0xC000))
			// check if room causes pit damage vs warp:
			// RoomsWithPitDamage#_00990C [0x70]uint16
			pitDamages := roomsWithPitDamage[this]

			//STAIR0TO = $7EC001
			//STAIR1TO = $7EC002
			//STAIR2TO = $7EC003
			//STAIR3TO = $7EC004
			stairExitTo := [4]Supertile{
				Supertile(read8(wram[:], uint32(0xC001))),
				Supertile(read8(wram[:], uint32(0xC002))),
				Supertile(read8(wram[:], uint32(0xC003))),
				Supertile(read8(wram[:], uint32(0xC004))),
			}
			_ = stairExitTo

			fmt.Fprintf(s.Logger, "    WARPTO   = %s\n", Supertile(read8(wram[:], 0xC000)))
			fmt.Fprintf(s.Logger, "    STAIR0TO = %s\n", Supertile(read8(wram[:], 0xC001)))
			fmt.Fprintf(s.Logger, "    STAIR1TO = %s\n", Supertile(read8(wram[:], 0xC002)))
			fmt.Fprintf(s.Logger, "    STAIR2TO = %s\n", Supertile(read8(wram[:], 0xC003)))
			fmt.Fprintf(s.Logger, "    STAIR3TO = %s\n", Supertile(read8(wram[:], 0xC004)))
			fmt.Fprintf(s.Logger, "    DARK     = %v\n", room.IsDarkRoom())

			//exitSeen := make(map[Supertile]struct{}, 24)
			pushEntryPoint := func(ep EntryPoint, name string) {
				//if ep.Supertile == 0 {
				//	panic("exit to 0!!")
				//}

				// for EG2:
				if this >= 0x100 {
					ep.Supertile |= 0x100
				}

				// TODO: address this; probably would only need it to prevent accidental infinite loops
				//if _, ok := exitSeen[st]; ok {
				//	return
				//}
				//exitSeen[st] = struct{}{}

				lifo = append(lifo, ep)
				fmt.Fprintf(s.Logger, "    %s to %s\n", name, ep)
			}

			// dont need to read interroom stair list from $06B0; just link stair tile number to STAIRnTO exit

			// flood fill to find reachable tiles:
			findReachableTiles(
				&room.Tiles,
				room.TilesVisited,
				ep,
				func(t MapCoord, d Direction, v uint8) {
					// here we found a reachable tile:
					room.Reachable[t] = v

					// door objects:
					if v >= 0xF0 {
						//fmt.Printf("    door tile $%02x at %s\n", v, t)
						// dungeon exits are already patched out, so this should be a normal door
						lyr, row, col := t.RowCol()
						if row >= 0x3A {
							// south:
							if sn, sd, ok := this.MoveBy(DirSouth); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (0x06 << 6) | col), sd}, "south door")
							}
						} else if row <= 0x06 {
							// north:
							if sn, sd, ok := this.MoveBy(DirNorth); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (0x3A << 6) | col), sd}, "north door")
							}
						} else if col >= 0x3A {
							// east:
							if sn, sd, ok := this.MoveBy(DirEast); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (row << 6) | 0x06), sd}, "east door")
							}
						} else if col <= 0x06 {
							// west:
							if sn, sd, ok := this.MoveBy(DirWest); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (row << 6) | 0x3A), sd}, "west door")
							}
						}

						return
					}

					// interroom doorways:
					if (v >= 0x80 && v <= 0x8D) || (v >= 0x90 && v <= 97) {
						if ok, edir, _, _ := t.IsDoorEdge(); ok {
							if v&1 == 0 {
								// north-south normal doorway (no teleport doorways for north-south):
								if sn, _, ok := this.MoveBy(edir); ok {
									if v&0x10 == 0x10 {
										// layer swap:
										pushEntryPoint(EntryPoint{sn, t.OppositeDoorEdge() ^ 0x1000, edir}, "north-south doorway (layer swap)")
									} else {
										pushEntryPoint(EntryPoint{sn, t.OppositeDoorEdge(), edir}, "north-south doorway")
									}
								}
							} else {
								// east-west doorway:
								if v == 0x89 {
									// teleport doorway:
									if edir == DirWest {
										if v&0x10 == 0x10 {
											// layer swap:
											pushEntryPoint(EntryPoint{stairExitTo[2], t.OppositeDoorEdge() ^ 0x1000, edir}, "west teleport doorway (layer swap)")
										} else {
											pushEntryPoint(EntryPoint{stairExitTo[2], t.OppositeDoorEdge(), edir}, "west teleport doorway")
										}
									} else if edir == DirEast {
										if v&0x10 == 0x10 {
											// layer swap:
											pushEntryPoint(EntryPoint{stairExitTo[3], t.OppositeDoorEdge() ^ 0x1000, edir}, "east teleport doorway (layer swap)")
										} else {
											pushEntryPoint(EntryPoint{stairExitTo[3], t.OppositeDoorEdge(), edir}, "east teleport doorway")
										}
									} else {
										panic("invalid direction approaching east-west teleport doorway")
									}
								} else {
									// normal doorway:
									if sn, _, ok := this.MoveBy(edir); ok {
										if v&0x10 == 0x10 {
											// layer swap:
											pushEntryPoint(EntryPoint{sn, t.OppositeDoorEdge() ^ 0x1000, edir}, "east-west doorway (layer swap)")
										} else {
											pushEntryPoint(EntryPoint{sn, t.OppositeDoorEdge(), edir}, "east-west doorway")
										}
									}
								}
							}
						}
						return
					}

					// interroom stair exits:
					if v == 0x5E || v == 0x5F {
						panic(fmt.Errorf("should NEVER see $%02x at %s %s", v, t, d))
					}

					// these are stair EDGEs; don't think we care:
					if v == 0x38 {
						// north stairs going down:
						var vn uint8
						vn = room.Tiles[t+0x40]
						if vn < 0x30 && v >= 0x38 {
							panic(fmt.Errorf("stairs needs exit at %s %s", t, d))
						}
						// NOTE: hack the stairwell position
						pushEntryPoint(EntryPoint{stairExitTo[v&3], 0x1E9F, d}, fmt.Sprintf("straightStair(%s)", t))
						return
					} else if v == 0x39 {
						// south stairs going up:
						var vn uint8
						vn = room.Tiles[t-0x40]
						if vn < 0x30 && v >= 0x38 {
							panic(fmt.Errorf("stairs needs exit at %s %s", t, d))
						}
						// NOTE: hack the stairwell position
						pushEntryPoint(EntryPoint{stairExitTo[v&3], 0x011F, d}, fmt.Sprintf("straightStair(%s)", t))
						return
					}

					if v >= 0x30 && v < 0x38 {
						var vn uint8
						vn = room.Tiles[t-0x40]
						if vn == 0x80 || vn == 0x26 {
							vn = room.Tiles[t+0x40]
						}

						if vn == 0x5E || vn == 0x5F {
							// spiral staircase
							pushEntryPoint(EntryPoint{stairExitTo[v&3], t, d.Opposite()}, fmt.Sprintf("spiralStair(%s)", t))
							return
						} else if vn == 0x38 {
							// north stairs going down:
							// NOTE: hack the stairwell position
							pushEntryPoint(EntryPoint{stairExitTo[v&3], 0x1E9F, d}, fmt.Sprintf("northStair(%s)", t))
							return
						} else if vn == 0x39 {
							// south stairs going up:
							// NOTE: hack the stairwell position
							pushEntryPoint(EntryPoint{stairExitTo[v&3], 0x011F, d}, fmt.Sprintf("southStair(%s)", t))
							return
						} else if vn == 0x00 {
							// straight stairs:
							pushEntryPoint(EntryPoint{stairExitTo[v&3], t, d.Opposite()}, fmt.Sprintf("stair(%s)", t))
							return
						}
						panic(fmt.Errorf("unhandled stair exit at %s %s", t, d))
						return
					}

					// pit exits:
					if !pitDamages && warpExitTo != 0 {
						if v == 0x20 {
							// pit tile
							pushEntryPoint(EntryPoint{warpExitTo, t, d}, fmt.Sprintf("pit(%s)", t))
						} else if v == 0x62 {
							// bombable floor tile
							pushEntryPoint(EntryPoint{warpExitTo, t, d}, fmt.Sprintf("bombableFloor(%s)", t))
						}
						return
					}
				},
			)

			ioutil.WriteFile(fmt.Sprintf("data/%03X.rch", uint16(this)), room.Reachable[:], 0644)
		}

		// render all supertiles found:
		if len(g.Rooms) >= 1 {
			for _, room := range g.Rooms[1:] {
				// TODO: clone System instances and parallelize

				// loadSupertile:
				write16(s.WRAM[:], 0xA0, uint16(room.Supertile))
				if err = s.ExecAt(loadSupertilePC, donePC); err != nil {
					t.Fatal(err)
				}

				{
					fmt.Fprintf(s.Logger, "  render %s\n", room.Supertile)
					copy((&room.VRAMTileSet)[:], (&s.VRAM)[0x4000:0x8000])
					copy((&room.WRAM)[:], (&s.WRAM)[:])

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

	// condense all maps into one image at different scale levels:
	for _, divider := range []int{1} {
		supertilepx := 512 / divider

		wga := sync.WaitGroup{}

		all := image.NewNRGBA(image.Rect(0, 0, 0x10*supertilepx, (0x130*supertilepx)/0x10))
		// clear the image and remove alpha layer
		draw.Draw(
			all,
			all.Bounds(),
			image.NewUniform(color.NRGBA{0, 0, 0, 255}),
			image.Point{},
			draw.Src)

		greenTint := image.NewUniform(color.NRGBA{0, 255, 0, 64})

		for i := range entranceGroups {
			g := &entranceGroups[i]
			for _, room := range g.Rooms {
				st := int(room.Supertile)
				stMap := room.Rendered
				if stMap == nil {
					continue
				}

				row := st / 0x10
				col := st % 0x10
				wga.Add(1)
				go func(room *RoomState) {
					stx := col * supertilepx
					sty := row * supertilepx
					draw.NearestNeighbor.Scale(
						all,
						image.Rect(stx, sty, stx+supertilepx, sty+supertilepx),
						stMap,
						stMap.Bounds(),
						draw.Src,
						nil,
					)

					// green highlight spots that are reachable:
					if true {
						maxRange := 0x2000
						if room.IsDarkRoom() {
							maxRange = 0x1000
						}
						for t := 0; t < maxRange; t++ {
							if room.Reachable[t] == 0x01 {
								continue
							}

							_, tr, tc := MapCoord(t).RowCol()
							draw.Draw(all, image.Rect(stx+int(tc)<<3, sty+int(tr)<<3, stx+int(tc)<<3+8, sty+int(tr)<<3+8), greenTint, image.Point{}, draw.Over)
						}
					}

					wga.Done()
				}(room)
			}
		}

		wga.Wait()
		if err = exportPNG(fmt.Sprintf("data/all-%d.png", divider), all); err != nil {
			panic(err)
		}
	}
}

type empty = struct{}

type MapCoord uint16

func (t MapCoord) String() string {
	_, row, col := t.RowCol()
	return fmt.Sprintf("$%04x={%02x,%02x}", uint16(t), row, col)
}

type EntryPoint struct {
	Supertile
	Point MapCoord
	Direction
}

func (ep EntryPoint) String() string {
	return fmt.Sprintf("{%s, %s, %s}", ep.Supertile, ep.Point, ep.Direction)
}

func AbsToMapCoord(absX, absY, layer uint16) MapCoord {
	// modeled after RoomTag_GetTilemapCoords#_01CDA5
	c := ((absY+0xFFFF)&0x01F8)<<3 | ((absX+0x000E)&0x01F8)>>3
	if layer != 0 {
		return MapCoord(c | 0x1000)
	}
	return MapCoord(c)
}

func (t MapCoord) MoveBy(dir Direction, increment int) (MapCoord, Direction, bool) {
	it := int(t)
	row := (it & 0xFC0) >> 6
	col := it & 0x3F

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

func (t MapCoord) OppositeEdge() MapCoord {
	lyr, row, col := t.RowCol()
	if row == 0 {
		return MapCoord(lyr | (0x3F << 6) | col)
	}
	if row == 0x3F {
		return MapCoord(lyr | (0x00 << 6) | col)
	}
	if col == 0 {
		return MapCoord(lyr | (row << 6) | 0x3F)
	}
	if col == 0x3F {
		return MapCoord(lyr | (row << 6) | 0x00)
	}
	return t
}

func (t MapCoord) IsDoorEdge() (ok bool, dir Direction, row, col uint16) {
	_, row, col = t.RowCol()
	if row <= 0x06 {
		ok, dir = true, DirNorth
		return
	}
	if row >= 0x3A {
		ok, dir = true, DirSouth
		return
	}
	if col <= 0x06 {
		ok, dir = true, DirWest
		return
	}
	if col >= 0x3A {
		ok, dir = true, DirEast
		return
	}
	return
}

func (t MapCoord) OppositeDoorEdge() MapCoord {
	lyr, row, col := t.RowCol()
	if row <= 0x06 {
		return MapCoord(lyr | (0x3A << 6) | col)
	}
	if row >= 0x3A {
		return MapCoord(lyr | (0x06 << 6) | col)
	}
	if col <= 0x06 {
		return MapCoord(lyr | (row << 6) | 0x3A)
	}
	if col >= 0x3A {
		return MapCoord(lyr | (row << 6) | 0x06)
	}
	panic("not at an edge")
	return t
}

func findReachableTiles(
	m *[0x2000]uint8,
	visited map[MapCoord]empty,
	entryPoint EntryPoint,
	visit func(t MapCoord, d Direction, v uint8),
) {
	type state struct {
		t      MapCoord
		d      Direction
		inPipe bool
	}

	// if we ever need to wrap
	f := visit

	var lifoSpace [0x2000]state
	lifo := lifoSpace[:0]
	lifo = append(lifo, state{t: entryPoint.Point, d: entryPoint.Direction})

	pushAllDirections := func(t MapCoord) {
		// can move in any direction:
		if tn, dir, ok := t.MoveBy(DirNorth, 1); ok {
			lifo = append(lifo, state{t: tn, d: dir})
		}
		if tn, dir, ok := t.MoveBy(DirWest, 1); ok {
			lifo = append(lifo, state{t: tn, d: dir})
		}
		if tn, dir, ok := t.MoveBy(DirEast, 1); ok {
			lifo = append(lifo, state{t: tn, d: dir})
		}
		if tn, dir, ok := t.MoveBy(DirSouth, 1); ok {
			lifo = append(lifo, state{t: tn, d: dir})
		}
	}

	// handle the stack of locations to traverse:
	for len(lifo) != 0 {
		lifoLen := len(lifo) - 1
		s := lifo[lifoLen]
		lifo = lifo[:lifoLen]

		if _, ok := visited[s.t]; ok {
			continue
		}

		v := m[s.t]

		if v == 0x0A {
			panic("notify kan")
		}

		if s.inPipe {
			// pipe exit:
			if v == 0xBE {
				visited[s.t] = empty{}
				f(s.t, s.d, v)

				// continue in the same direction but not in pipe-follower state:
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					lifo = append(lifo, state{t: tn, d: dir})
				}
				continue
			}

			// TR west to south or north to east:
			if v == 0xB2 {
				if s.d == DirWest {
					if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				} else if s.d == DirNorth {
					if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				}
				continue
			}
			// TR south to east or west to north:
			if v == 0xB3 {
				if s.d == DirSouth {
					if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				} else if s.d == DirWest {
					if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				}
				continue
			}
			// TR north to west or east to south:
			if v == 0xB4 {
				if s.d == DirNorth {
					if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				} else if s.d == DirEast {
					if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				}
				continue
			}
			// TR east to north or south to west:
			if v == 0xB5 {
				if s.d == DirEast {
					if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				} else if s.d == DirSouth {
					if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
					}
				}
				continue
			}

			// for anything else we just continue in the same direction:
			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
			}
			continue
		}

		isPassable := v == 0x00 ||
			// water:
			v == 0x08 || v == 0x09 ||
			// layer passthrough:
			v == 0x0C || v == 0x1C ||
			// manual stairs:
			v == 0x22 ||
			// floor switches:
			v == 0x23 || v == 0x24 ||
			// spikes / floor ice:
			(v >= 0x0D && v <= 0x0F) ||
			// pots/pegs/blocks:
			v&0xF0 == 0x70 ||
			// star tiles:
			v == 0x3A || v == 0x3B ||
			// thick grass:
			v == 0x40 ||
			// warp:
			v == 0x4B ||
			// rupee tile:
			v == 0x60 ||
			// bombable floor:
			v == 0x62 || // TODO
			// crystal pegs (orange/blue):
			v == 0x66 || v == 0x67 ||
			// conveyors:
			(v >= 0x68 && v <= 0x6B) ||
			// north/south dungeon swap door (for HC to sewers)
			v == 0xA0

		if isPassable {
			// no collision:
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			// can move in any direction:
			pushAllDirections(s.t)
			continue
		}

		// north-facing stairs:
		if v == 0x1D {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				lifo = append(lifo, state{t: tn, d: dir})
			}
			continue
		}
		// north-facing stairs, layer changing:
		if v >= 0x1E && v <= 0x1F {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 2); ok {
				// swap layers:
				tn ^= 0x1000
				lifo = append(lifo, state{t: tn, d: dir})
			}
			continue
		}

		// pit:
		if v == 0x20 {
			// Link can fall into pit but cannot move beyond it:
			//visited[s.t] = empty{}
			f(s.t, s.d, v)
			continue
		}

		// ledge tiles:
		if v >= 0x28 && v <= 0x2B {
			visited[s.t] = empty{}

			// check 4 tiles from ledge for pit:
			t, dir, ok := s.t.MoveBy(s.d, 4)
			if !ok {
				continue
			}

			// pit tile on same layer?
			v = m[t]
			if v == 0x20 {
				// visit it next:
				lifo = append(lifo, state{t: t, d: dir})
			} else if v == 0x1C { // or 0x0C ?
				// swap layers:
				t ^= 0x1000

				// check again for pit tile on the opposite layer:
				v = m[t]
				if v == 0x20 {
					// visit it next:
					lifo = append(lifo, state{t: t, d: dir})
				}
			} else if v == 0x0C {
				panic(fmt.Errorf("TODO handle $0C in pit case t=%s", t))
			}

			continue
		}

		// interroom stair exits:
		if v >= 0x30 && v <= 0x37 {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			// don't continue beyond a staircase unless it's our entry point:
			if len(lifo) == 0 {
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					lifo = append(lifo, state{t: tn, d: dir})
				}
			}
			continue
		}

		// 38=Straight interroom stairs north/down edge (39= south/up edge):
		if v == 0x38 || v == 0x39 {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			// don't continue beyond a staircase:
			//if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
			//	lifo = append(lifo, state{t: tn, d: dir})
			//}
			continue
		}

		// south-facing single-layer auto stairs:
		if v == 0x3D {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				lifo = append(lifo, state{t: tn, d: dir})
			}
			continue
		}
		// south-facing layer-swap auto stairs:
		if v >= 0x3E && v <= 0x3F {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 2); ok {
				// swap layers:
				tn ^= 0x1000
				lifo = append(lifo, state{t: tn, d: dir})
			}
			continue
		}

		// spiral staircase:
		// $5F is the layer 2 version of $5E (spiral staircase)
		if v == 0x5E || v == 0x5F {
			visited[s.t] = empty{}

			// don't visit spiral stairs, just skip over them:
			//f(s.t, s.d, m[s.t])

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				lifo = append(lifo, state{t: tn, d: dir})
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
						lifo = append(lifo, state{t: s.t, d: dir})
					}
					if _, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
						lifo = append(lifo, state{t: s.t, d: dir})
					}
					continue
				}

				if s.d != DirNorth && s.d != DirSouth {
					panic(fmt.Errorf("north-south door approached from perpendicular direction %s at %s", s.d, s.t))
				}

				visited[s.t] = empty{}
				f(s.t, s.d, v)
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					lifo = append(lifo, state{t: tn, d: dir})
				}
			} else {
				// east-west
				if s.d == DirNone {
					// scout in both directions:
					if _, dir, ok := s.t.MoveBy(DirWest, 1); ok {
						lifo = append(lifo, state{t: s.t, d: dir})
					}
					if _, dir, ok := s.t.MoveBy(DirEast, 1); ok {
						lifo = append(lifo, state{t: s.t, d: dir})
					}
					continue
				}

				if s.d != DirEast && s.d != DirWest {
					panic(fmt.Errorf("east-west door approached from perpendicular direction %s at %s", s.d, s.t))
				}

				visited[s.t] = empty{}
				f(s.t, s.d, v)
				if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
					lifo = append(lifo, state{t: tn, d: dir})
				}
			}
			continue
		}
		// east-west teleport door
		if v == 0x89 {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				lifo = append(lifo, state{t: tn, d: dir})
			}
			continue
		}
		// entrance door (8E = north-south?, 8F = east-west??):
		if v == 0x8E || v == 0x8F {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			if s.d == DirNone {
				// scout in both directions:
				if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok {
					lifo = append(lifo, state{t: tn, d: dir})
				}
				if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok {
					lifo = append(lifo, state{t: tn, d: dir})
				}
				continue
			}

			if tn, dir, ok := s.t.MoveBy(s.d, 1); ok {
				lifo = append(lifo, state{t: tn, d: dir})
			}
			continue
		}

		// TODO layer toggle shutter doors:
		//(v >= 0x90 && v <= 0xAF)

		// TR pipe entrance:
		if v == 0xBE {
			visited[s.t] = empty{}
			f(s.t, s.d, v)

			// find corresponding B0..B1 directional pipe to follow:
			if tn, dir, ok := s.t.MoveBy(DirNorth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
			}
			if tn, dir, ok := s.t.MoveBy(DirWest, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
			}
			if tn, dir, ok := s.t.MoveBy(DirEast, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
			}
			if tn, dir, ok := s.t.MoveBy(DirSouth, 1); ok && (m[tn] >= 0xB0 && m[tn] <= 0xB1) {
				lifo = append(lifo, state{t: tn, d: dir, inPipe: true})
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

				lifo = append(lifo, state{t: t, d: s.d})
				continue
			}

			if true {
				visited[s.t] = empty{}
				f(s.t, s.d, v)

				if t, _, ok := s.t.MoveBy(s.d, 2); ok {
					lifo = append(lifo, state{t: t, d: s.d})
				}
			}
			continue
		}

		// anything else is considered solid:
		continue
	}
}

type Entrance struct {
	EntranceID uint8
	Supertile

	EntryCoord MapCoord

	Rooms      []*RoomState
	Supertiles map[Supertile]*RoomState
}

type RoomState struct {
	Supertile

	Rendered image.Image

	Doors []Door

	TilesVisited map[MapCoord]empty
	Tiles        [0x2000]byte
	Reachable    [0x2000]byte

	WRAM        [0x20000]byte
	VRAMTileSet [0x4000]byte
}

func (r *RoomState) IsDarkRoom() bool { return read8((&r.WRAM)[:], 0xC005) != 0 }

type Supertile uint16

func (s Supertile) String() string { return fmt.Sprintf("$%03x", uint16(s)) }

func (s Supertile) MoveBy(dir Direction) (sn Supertile, sd Direction, ok bool) {
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
	if sn&0xFF00 != s&0xFF00 {
		ok = false
	}

	return
}

type StaircaseInterRoom struct {
	Pos uint16 // $06B0
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

func (t DoorType) IsStairwell() bool {
	return t >= 0x20 && t <= 0x26
}

func (t DoorType) String() string {
	return fmt.Sprintf("$%02x", uint8(t))
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
