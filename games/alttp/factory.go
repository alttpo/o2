package alttp

import (
	"fmt"
	"o2/games"
	"o2/snes"
)

var gameName = "ALTTP"

type Factory struct{}

var factory *Factory

func FactoryInstance() *Factory { return factory }

func (f *Factory) IsROMSupported(rom *snes.ROM) (ok bool, whyNot string) {
	// Removed because DR DoorRandomizer updates header version from 1 to 2.
	var headerVersion = rom.Header.HeaderVersion()
	if headerVersion != 1 && headerVersion != 2 {
		return false, fmt.Sprintf("ROM header version is %d, not 1 or 2", rom.Header.HeaderVersion())
	}
	if rom.Header.MapMode != 0x20 && rom.Header.MapMode != 0x30 {
		return false, fmt.Sprintf("Map mode is 0x%02X, not 0x20 or 0x30", rom.Header.MapMode)
	}
	if rom.Header.ROMSize < 0x0A {
		return false, fmt.Sprintf("ROM size is 0x%02X, which is less than 0x0A", rom.Header.ROMSize)
	}
	if rom.Header.OldMakerCode != 0x01 {
		return false, fmt.Sprintf("Maker code is 0x%02X, not 0x01", rom.Header.OldMakerCode)
	}
	return true, ""
}

func (f *Factory) CanPlay(rom *snes.ROM) (ok bool, whyNot string) {
	romVersion := rom.Header.MaskROMVersion
	if rom.Header.DestinationCode == snes.RegionNorthAmerica && romVersion == 0 {
		return true, ""
	} else if rom.Header.DestinationCode == snes.RegionJapan && romVersion == 0 {
		return true, ""
	}
	// TODO: read header of ROM to determine what variants are supported or not

	return false, "Not NA or JP ROM"
}

func (f *Factory) Patcher(rom *snes.ROM) games.Patcher {
	return &Patcher{rom: rom}
}

func init() {
	factory = &Factory{}
	games.Register(gameName, factory)
}
