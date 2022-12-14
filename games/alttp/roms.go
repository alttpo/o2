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
}
