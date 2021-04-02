package alttp

import (
	"o2/games"
	"o2/snes"
)

var gameName = "ALTTP"

type Factory struct{}

var factory *Factory

func FactoryInstance() *Factory { return factory }

func (f *Factory) IsROMSupported(rom *snes.ROM) bool {
	if rom.Header.Version() != 1 {
		return false
	}
	if rom.Header.MapMode != 0x20 && rom.Header.MapMode != 0x30 {
		return false
	}
	if rom.Header.ROMSize < 0x0A {
		return false
	}
	if rom.Header.OldMakerCode != 0x01 {
		return false
	}
	return true
}

func (f *Factory) CanPlay(rom *snes.ROM) (ok bool, whyNot string) {
	// TODO: read header of ROM to determine what variants are supported or not
	return true, ""
}

func (f *Factory) Patcher(rom *snes.ROM) games.Patcher {
	return &Patcher{rom: rom}
}

func init() {
	factory = &Factory{}
	games.Register(gameName, factory)
}
