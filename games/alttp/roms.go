package alttp

import (
	"log"
	"o2/snes"
)

type romFunction int

const (
	fnUpdatePaletteArmorGloves romFunction = iota
	fnUpdatePaletteSword
	fnUpdatePaletteShield
	fnDecompGfxSword
	fnDecompGfxShield
	fnLoadSpriteGfx
	fnOverworldFinishMirrorWarp
	fnOverworldCreatePyramidHole
	// Overworld_AlterWeathervane and OpenGargoylesDomain: draw the destroyed weathervane (area $18) and the opened
	// Thieves' Town entrance (area $58) and set bit $20 of the area's flags; JSL, RTL, return in 8-bit mode:
	fnOverworldAlterWeathervane
	fnOverworldOpenGargoylesDomain
	// FireRodShot_BecomeSkullWoodsFire: lights the fire around the Skull Woods entrance (ancilla $34) and starts its
	// entrance animation ($04C6 = 2); JSL in 8-bit mode:
	fnSkullWoodsFire
	fnResetSpriteProperties
	fnToggleStarTileGraphics
	// Overworld_DrawMap16_Persist: A = map16 tile, X = tilemap offset, 16-bit A/X/Y; writes $7E2000,X and queues a
	// stripe to draw it into the BG tilemap in VRAM:
	fnOverworldDrawMap16Persist
	// RoomDraw_16x16Single: Y = manipulable slot * 2, 16-bit A/X/Y; draws the slot's replacement tiles ($0560..$05C0)
	// at its tilemap position ($0540) into $7E2000, its attributes into $7F2000 and queues a stripe for VRAM. It ends
	// with RTS so it is called through fnBank01RTL (see asm.Emitter.JSRBanked):
	fnRoomDraw16x16Single
	// an RTL instruction in bank $01, used as the return address to call RTS routines in bank $01 from elsewhere:
	fnBank01RTL
)

func (g *Game) isVTRandomizer() bool {
	return string(g.rom.Header.Title[0:3]) == "VT "
}

func (g *Game) fillRomFunctions() {
	fillRomFunctions(g.rom, g.romFunctions)
}

// fillRomFunctions fills fns with the addresses of the functions in the ROM's version of the game; used by both the
// Game and the Patcher.
func fillRomFunctions(rom *snes.ROM, fns map[romFunction]uint32) {
	romVersion := rom.Header.MaskROMVersion
	if rom.Header.DestinationCode == snes.RegionNorthAmerica && romVersion == 0 {
		fillRomUS10(fns)
	} else if rom.Header.DestinationCode == snes.RegionJapan && romVersion == 0 {
		fillRomJP10(fns)
	} else {
		fillRomJP10(fns)
		log.Printf("unsupported %s 1.%d ROM version; assuming JP 1.0", snes.RegionNames[rom.Header.DestinationCode], romVersion)
	}
}

func fillRomUS10(fns map[romFunction]uint32) {
	// USA 1.0
	fns[fnUpdatePaletteArmorGloves] = 0x1BEDF9
	fns[fnUpdatePaletteSword] = 0x1BED03
	fns[fnUpdatePaletteShield] = 0x1BED29
	fns[fnDecompGfxSword] = 0x00D2C8
	fns[fnDecompGfxShield] = 0x00D308
	fns[fnLoadSpriteGfx] = 0x00FC62
	fns[fnOverworldFinishMirrorWarp] = 0x02B260
	fns[fnOverworldCreatePyramidHole] = 0x1BC2A7
	fns[fnOverworldAlterWeathervane] = 0x1BC21D
	fns[fnOverworldOpenGargoylesDomain] = 0x1BC264
	fns[fnSkullWoodsFire] = 0x099C4F
	fns[fnResetSpriteProperties] = 0x0DB871
	fns[fnToggleStarTileGraphics] = 0x00FDA7
	fns[fnOverworldDrawMap16Persist] = 0x1BC97C
	fns[fnRoomDraw16x16Single] = 0x01D828
	fns[fnBank01RTL] = 0x01D827 // PushBlock_Handler's RTL, right before RoomDraw_16x16Single
}

func fillRomJP10(fns map[romFunction]uint32) {
	// JP 1.0
	fns[fnUpdatePaletteArmorGloves] = 0x1BEDF9
	fns[fnUpdatePaletteSword] = 0x1BED03
	fns[fnUpdatePaletteShield] = 0x1BED29
	fns[fnDecompGfxSword] = 0x00D308
	fns[fnDecompGfxShield] = 0x00D348
	fns[fnLoadSpriteGfx] = 0x00FC62
	fns[fnOverworldFinishMirrorWarp] = 0x02B186
	fns[fnOverworldCreatePyramidHole] = 0x1BC2A7
	fns[fnOverworldAlterWeathervane] = 0x1BC21D
	fns[fnOverworldOpenGargoylesDomain] = 0x1BC264
	fns[fnSkullWoodsFire] = 0x099C3B
	fns[fnResetSpriteProperties] = 0x0DB871
	fns[fnToggleStarTileGraphics] = 0x00FDA7
	fns[fnOverworldDrawMap16Persist] = 0x1BC97C
	fns[fnRoomDraw16x16Single] = 0x01D826
	fns[fnBank01RTL] = 0x01D825 // PushBlock_Handler's RTL, right before RoomDraw_16x16Single
}
