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
	fnResetSpriteProperties
	fnToggleStarTileGraphics
	// Overworld_DrawMap16_Persist: A = map16 tile, X = tilemap offset, 16-bit A/X/Y; writes $7E2000,X and queues a
	// stripe to draw it into the BG tilemap in VRAM:
	fnOverworldDrawMap16Persist
	// RoomDraw_16x16Single: Y = manipulable slot * 2, 16-bit A/X/Y; draws the slot's replacement tiles ($0560..$05C0)
	// at its tilemap position ($0540) into $7E2000, its attributes into $7F2000 and queues a stripe for VRAM. It ends
	// with RTS so it is called through fnBank01RTL (see emitJSRBank01):
	fnRoomDraw16x16Single
	// an RTL instruction in bank $01, used as the return address to call RTS routines in bank $01 from elsewhere:
	fnBank01RTL
)

func (g *Game) isVTRandomizer() bool {
	return string(g.rom.Header.Title[0:3]) == "VT "
}

func (g *Game) fillRomFunctions() {
	romVersion := g.rom.Header.MaskROMVersion
	if g.rom.Header.DestinationCode == snes.RegionNorthAmerica && romVersion == 0 {
		g.fillRomUS10()
	} else if g.rom.Header.DestinationCode == snes.RegionJapan && romVersion == 0 {
		g.fillRomJP10()
	} else {
		g.fillRomJP10()
		log.Printf("unsupported %s 1.%d ROM version; assuming JP 1.0", snes.RegionNames[g.rom.Header.DestinationCode], romVersion)
	}
}

func (g *Game) fillRomUS10() {
	// USA 1.0
	g.romFunctions[fnUpdatePaletteArmorGloves] = 0x1BEDF9
	g.romFunctions[fnUpdatePaletteSword] = 0x1BED03
	g.romFunctions[fnUpdatePaletteShield] = 0x1BED29
	g.romFunctions[fnDecompGfxSword] = 0x00D2C8
	g.romFunctions[fnDecompGfxShield] = 0x00D308
	g.romFunctions[fnLoadSpriteGfx] = 0x00FC62
	g.romFunctions[fnOverworldFinishMirrorWarp] = 0x02B260
	g.romFunctions[fnOverworldCreatePyramidHole] = 0x1BC2A7
	g.romFunctions[fnResetSpriteProperties] = 0x0DB871
	g.romFunctions[fnToggleStarTileGraphics] = 0x00FDA7
	g.romFunctions[fnOverworldDrawMap16Persist] = 0x1BC97C
	g.romFunctions[fnRoomDraw16x16Single] = 0x01D828
	g.romFunctions[fnBank01RTL] = 0x01D827 // PushBlock_Handler's RTL, right before RoomDraw_16x16Single
}

func (g *Game) fillRomJP10() {
	// JP 1.0
	g.romFunctions[fnUpdatePaletteArmorGloves] = 0x1BEDF9
	g.romFunctions[fnUpdatePaletteSword] = 0x1BED03
	g.romFunctions[fnUpdatePaletteShield] = 0x1BED29
	g.romFunctions[fnDecompGfxSword] = 0x00D308
	g.romFunctions[fnDecompGfxShield] = 0x00D348
	g.romFunctions[fnLoadSpriteGfx] = 0x00FC62
	g.romFunctions[fnOverworldFinishMirrorWarp] = 0x02B186
	g.romFunctions[fnOverworldCreatePyramidHole] = 0x1BC2A7
	g.romFunctions[fnResetSpriteProperties] = 0x0DB871
	g.romFunctions[fnToggleStarTileGraphics] = 0x00FDA7
	g.romFunctions[fnOverworldDrawMap16Persist] = 0x1BC97C
	g.romFunctions[fnRoomDraw16x16Single] = 0x01D826
	g.romFunctions[fnBank01RTL] = 0x01D825 // PushBlock_Handler's RTL, right before RoomDraw_16x16Single
}
