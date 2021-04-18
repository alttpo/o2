package alttp

import "o2/snes"

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

func (g *Game) fillRomFunctions() {
	if g.rom.Header.DestinationCode == snes.RegionNorthAmerica {
		// USA 1.0
		g.romFunctions[fnUpdatePaletteArmorGloves] = 0x1BEDF9
		g.romFunctions[fnUpdatePaletteSword] = 0x1BED03
		g.romFunctions[fnUpdatePaletteShield] = 0x1BED29
		g.romFunctions[fnDecompGfxSword] = 0x00D2C8
		g.romFunctions[fnDecompGfxShield] = 0x00D308
		g.romFunctions[fnLoadSpriteGfx] = 0x00FC62
		g.romFunctions[fnOverworldFinishMirrorWarp] = 0x02B260
		g.romFunctions[fnOverworldCreatePyramidHole] = 0x1BC2A7
	} else if g.rom.Header.DestinationCode == snes.RegionJapan {
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
}
