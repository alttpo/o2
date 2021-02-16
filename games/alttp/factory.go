package alttp

import (
	"o2/games"
	"o2/snes"
)

type Factory struct{}

func (f *Factory) IsBestProvider(rom *snes.ROM) bool {
	return rom.Header.GameCode == 0x30e20124
}

func (f *Factory) IsROMSupported(rom *snes.ROM) (ok bool, whyNot string) {
	// TODO: read header of ROM to determine what variants are supported or not
	return true, ""
}

func (f *Factory) Patcher(rom *snes.ROM) games.Patcher {
	return &Patcher{rom: rom}
}

func (f *Factory) NewGame(rom *snes.ROM, conn snes.Conn) (games.Game, error) {
	return &Game{rom, conn, false}, nil
}

func init() {
	games.Register("ALTTP", &Factory{})
}
