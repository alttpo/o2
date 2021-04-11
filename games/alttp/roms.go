package alttp

import "o2/snes"

type romFunction int

const (
    fnUpdatePaletteArmorGloves romFunction = iota
    fnUpdatePaletteSword
    fnUpdatePaletteShield
    fnDecompGfxSword
    fnDecompGfxShield
)

func (g *Game) fillRomFunctions() {
    if g.rom.Header.DestinationCode == snes.RegionJapan {
        g.romFunctions[fnUpdatePaletteArmorGloves] = 0x1BEDF9
        g.romFunctions[fnUpdatePaletteSword] = 0x1BED03
        g.romFunctions[fnUpdatePaletteShield] = 0x1BED29
        g.romFunctions[fnDecompGfxSword] = 0x00D308
        g.romFunctions[fnDecompGfxShield] = 0x00D348
    } else if g.rom.Header.DestinationCode == snes.RegionNorthAmerica {
        g.romFunctions[fnUpdatePaletteArmorGloves] = 0x1BEDF9
        g.romFunctions[fnUpdatePaletteSword] = 0x1BED03
        g.romFunctions[fnUpdatePaletteShield] = 0x1BED29
        g.romFunctions[fnDecompGfxSword] = 0x00D2C8
        g.romFunctions[fnDecompGfxShield] = 0x00D308
    }
}
