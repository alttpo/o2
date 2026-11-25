package underworldmap

import "fmt"

// romAddresses are the addresses of the game routines and data the generator runs and reads, which differ between
// versions of the game. The names follow the US and JP disassemblies (../usdasm, ../jpdasm).
type romAddresses struct {
	name string

	// RunModule: runs the module in $10 (JSL, RTL):
	runModule uint32
	// PrepareOAMForTransfer and DoNMIUpdates: queue up and run the NMI's VRAM updates (JSR, in bank $00):
	prepareOAMForTransfer uint32
	doNMIUpdates          uint32
	// LoadSongBank: talks to the APU, which isn't emulated; patched to RTS:
	loadSongBank uint32
	// Module06_UnderworldLoad's JSR LoadUnderworldSongBankIfNeeded and its .exit, which the JSR is patched to jump to:
	underworldSongBankCall uint32
	underworldSongBankExit uint32
	// RebuildHUD_Keys: draws the HUD, which isn't needed; patched to RTL:
	rebuildHUDKeys uint32
	// RoomDraw_LampCones: draws dark rooms' lamp cone mask (object $2A) over their BG2 ($7E4000), which the map
	// leaves out to show the room lit; patched to RTS:
	roomDrawLampCones uint32

	// Decompress: decompresses the data at the 24-bit pointer in $C8 to the 24-bit pointer in $00, with 8-bit A and
	// X (JSR in bank $00); and its SEP #$10 before RTS, where Y is the size of the output. Its results are cached as
	// it is what loading a room spends most of its time on:
	decompress     uint32
	decompressExit uint32
	// InitializeTilesets and its PLB : RTL: loads the BG and sprite graphics sheets of $0AA1..$0AA4 into VRAM (JSL),
	// which every room load does again; its effects are cached by its inputs:
	initializeTilesets    uint32
	initializeTilesetsPLB uint32
	// LoadAndDrawRoom's .next_quadrant loop, which uploads the room's tilemaps to VRAM, and the PLA after it, which
	// the loop is patched to jump to: the map draws the tilemaps from WRAM:
	uploadRoomTilemaps     uint32
	uploadRoomTilemapsDone uint32
	// InitializeTilesets after its PHB : PHK : PLB, and where it starts loading the BG sheets after the sprite
	// sheets, which the map has no use for; the first jumps to the second:
	initializeTilesetsSprites uint32
	initializeTilesetsBG      uint32

	// the startup code's JSR LoadIntroSongBank, which the reset routine is run up to:
	resetDone uint32
	// LoadDefaultTileTypes (JSL):
	loadDefaultTileTypes uint32
	// Intro_InitializeDefaultGFX (JSL), which loads the default graphics and its tail after
	// DecompressAnimatedUnderworldTiles, which resets the fade and mosaic state before a room loads:
	introInitializeDefaultGFX     uint32
	introInitializeDefaultGFXTail uint32
	// InitializeSaveFile and CopySaveToWRAM (JSL): a new save file in slot 1 for the room flags:
	initializeSaveFile uint32
	copySaveToWRAM     uint32

	// PullBlocksTorchesFromROM's PHB, from which it resets the blocks, torches and pots of the dungeon and returns
	// (JSR in bank $02, with the data bank to restore pushed by the caller):
	pullBlocksTorchesPHB uint32
	// Module06_UnderworldLoad right after its JSR LoadUnderworldEntrance: loads and draws the room in $A0, its
	// graphics and palettes from the entrance's state (JMP in bank $02, ends with RTL):
	module06AfterLoadEntrance uint32

	// RoomsWithPitDamage: the rooms whose pits hurt Link instead of dropping him to the room below:
	roomsWithPitDamage      uint32
	roomsWithPitDamageCount int
}

var (
	romJP10 = romAddresses{
		name:                          "JP 1.0",
		runModule:                     0x00_80B5,
		prepareOAMForTransfer:         0x00_85FC,
		doNMIUpdates:                  0x00_89E0,
		loadSongBank:                  0x00_8888,
		underworldSongBankCall:        0x02_8293,
		underworldSongBankExit:        0x02_82BC,
		rebuildHUDKeys:                0x0D_FA88,
		roomDrawLampCones:             0x01_9DE5,
		decompress:                    0x00_E7DE,
		decompressExit:                0x00_E7EA,
		initializeTilesets:            0x00_E1DB,
		initializeTilesetsPLB:         0x00_E30E,
		initializeTilesetsSprites:     0x00_E1DE,
		uploadRoomTilemaps:            0x02_C300,
		uploadRoomTilemapsDone:        0x02_C317,
		initializeTilesetsBG:          0x00_E259,
		resetDone:                     0x00_8029,
		loadDefaultTileTypes:          0x0F_FD2A,
		introInitializeDefaultGFX:     0x0C_C208,
		introInitializeDefaultGFXTail: 0x0C_C237,
		initializeSaveFile:            0x0C_DB3E,
		copySaveToWRAM:                0x0C_CEB2,
		pullBlocksTorchesPHB:          0x02_D854,
		module06AfterLoadEntrance:     0x02_8157,
		roomsWithPitDamage:            0x00_990C,
		roomsWithPitDamageCount:       57,
	}
	romUS10 = romAddresses{
		name:                          "US 1.0",
		runModule:                     0x00_80B5,
		prepareOAMForTransfer:         0x00_85FC,
		doNMIUpdates:                  0x00_89E0,
		loadSongBank:                  0x00_8888,
		underworldSongBankCall:        0x02_8389,
		underworldSongBankExit:        0x02_83B2,
		rebuildHUDKeys:                0x0D_FA68,
		roomDrawLampCones:             0x01_9DE5,
		decompress:                    0x00_E79E,
		decompressExit:                0x00_E7AA,
		initializeTilesets:            0x00_E19B,
		initializeTilesetsPLB:         0x00_E2CE,
		initializeTilesetsSprites:     0x00_E19E,
		uploadRoomTilemaps:            0x02_C590,
		uploadRoomTilemapsDone:        0x02_C5A7,
		initializeTilesetsBG:          0x00_E219,
		resetDone:                     0x00_8029,
		loadDefaultTileTypes:          0x0E_97D9,
		introInitializeDefaultGFX:     0x0C_C1F9,
		introInitializeDefaultGFXTail: 0x0C_C228,
		initializeSaveFile:            0x0C_DBDC,
		copySaveToWRAM:                0x0C_CFBB,
		pullBlocksTorchesPHB:          0x02_DAF0,
		module06AfterLoadEntrance:     0x02_824D,
		roomsWithPitDamage:            0x00_990C,
		roomsWithPitDamageCount:       57,
	}
)

// LoROM header fields, as offsets into the ROM:
const (
	headerTitle       = 0x7FC0
	headerTitleLen    = 21
	headerDestination = 0x7FD9
	headerVersion     = 0x7FDB
)

// addressesFor picks the addresses for the version of the game in rom by its header: US 1.0, or JP 1.0, which the
// randomizers are built on.
func addressesFor(rom []byte) (*romAddresses, error) {
	if len(rom) < 0x8000 {
		return nil, fmt.Errorf("underworldmap: ROM is too small")
	}
	destination, version := rom[headerDestination], rom[headerVersion]
	switch {
	case destination == 0x01 && version == 0:
		return &romUS10, nil
	case destination == 0x00 && version == 0:
		return &romJP10, nil
	default:
		return nil, fmt.Errorf(
			"underworldmap: unsupported ROM: destination code $%02x version 1.%d; only US 1.0 and JP 1.0 are supported",
			destination,
			version,
		)
	}
}

// roomHeaders reads the rooms' headers (RoomHeaderPointers, which LoadRoomHeader reads with
// `LDA.l RoomHeaderPointers,X : STA.b $0D : SEP #$20 : REP #$10 : LDA.b #^RoomHeaderPointers : STA.b $0F`, found by
// those bytes so that it works in any version of the game).
type roomHeaders struct {
	rom   []byte
	table uint32
	bank  uint8
}

func findRoomHeaders(rom []byte) *roomHeaders {
	for i := 0; i+15 <= len(rom); i++ {
		b := rom[i:]
		if b[0] == 0xBF && b[4] == 0x85 && b[5] == 0x0D && b[6] == 0xE2 && b[7] == 0x20 && b[8] == 0xC2 && b[9] == 0x10 &&
			b[10] == 0xA9 && b[12] == 0x85 && b[13] == 0x0F {
			return &roomHeaders{rom: rom, table: uint32(b[1]) | uint32(b[2])<<8 | uint32(b[3])<<16, bank: b[11]}
		}
	}
	return nil
}

// header returns the bytes of room's header.
func (h *roomHeaders) header(room uint16) ([]byte, bool) {
	if h == nil {
		return nil, false
	}
	o := romOffset(h.table) + uint32(room)*2
	if int(o)+2 > len(h.rom) {
		return nil, false
	}
	p := romOffset(uint32(h.bank)<<16 | uint32(h.rom[o]) | uint32(h.rom[o+1])<<8)
	if int(p)+14 > len(h.rom) {
		return nil, false
	}
	return h.rom[p : p+14], true
}

// arrivalLower returns whether Link comes into room `to` on the lower level ($0476) by the staircase from room
// `from`: from's staircase plane for its staircase to `to` (dung_hdr_staircase_plane, set by Module07_11_0B and
// Module07_0E_13 through kTeleportPitLevel1).
func (h *roomHeaders) arrivalLower(from, to uint16) (lower uint8, ok bool) {
	hdr, ok := h.header(from)
	if !ok || from&0xFF00 != to&0xFF00 {
		return 0, false
	}
	planes := [4]uint8{hdr[7] >> 2 & 3, hdr[7] >> 4 & 3, hdr[7] >> 6 & 3, hdr[8] & 3}
	for j := 0; j < 4; j++ {
		if hdr[10+j] == uint8(to) {
			if planes[j] >= 1 {
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}
