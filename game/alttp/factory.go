package alttp

import (
	"o2/game"
	"o2/snes"
)

type Factory struct{}

func (f *Factory) IsROMCompatible(rom *snes.ROM) bool {
	return rom.Header.GameCode == 0x30e20124
}

func (f *Factory) NewGame(rom *snes.ROM, conn snes.Conn) (game.Game, error) {
	return &Game{rom, conn, false}, nil
}

func init() {
	game.Register("ALTTP", &Factory{})
}
