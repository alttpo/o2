package engine

import (
	"fmt"
	"o2/interfaces"
	"o2/snes"
)

type ROMViewModel struct {
	commands map[string]interfaces.Command

	c *ViewModel

	// public fields for JSON:
	IsLoaded bool   `json:"isLoaded"`
	Name     string `json:"name"` // filename loaded from (no path)
	Title    string `json:"title"`
	Region   string `json:"region"`
	Version  string `json:"version"`
}

func (v *ROMViewModel) Update() {
	rom := v.c.nextRom
	v.IsLoaded = rom != nil

	if v.IsLoaded {
		v.Title = string(rom.Header.Title[:])
		v.Region = snes.RegionNames[rom.Header.DestinationCode]
		v.Version = fmt.Sprintf("1.%d", rom.Header.MaskROMVersion)
	} else {
		v.Title = ""
		v.Region = ""
		v.Version = ""
	}
}

func (v *ROMViewModel) CommandFor(command string) (ce interfaces.Command, err error) {
	var ok bool
	ce, ok = v.commands[command]
	if !ok {
		err = fmt.Errorf("no command '%s' found", command)
	}
	return
}

func NewROMViewModel(c *ViewModel) *ROMViewModel {
	v := &ROMViewModel{
		c: c,
	}

	v.commands = map[string]interfaces.Command{
		"name": &ROMNameCommand{v},
		"data": &ROMDataCommand{v},
	}

	return v
}

// Commands:

type ROMNameCommand struct{ v *ROMViewModel }

type ROMNameCommandArgs struct {
	Name string `json:"name"`
}

func (ce *ROMNameCommand) CreateArgs() interfaces.CommandArgs { return &ROMNameCommandArgs{} }
func (ce *ROMNameCommand) Execute(args interfaces.CommandArgs) error {
	return ce.v.NameProvided(args.(*ROMNameCommandArgs))
}

func (v *ROMViewModel) NameProvided(args *ROMNameCommandArgs) error {
	v.Name = args.Name
	return nil
}

type ROMDataCommand struct{ v *ROMViewModel }

func (ce *ROMDataCommand) CreateArgs() interfaces.CommandArgs {
	panic("this is a binary command")
}
func (ce *ROMDataCommand) Execute(args interfaces.CommandArgs) error {
	return ce.v.DataProvided(args.([]byte))
}

func (v *ROMViewModel) DataProvided(romImage []byte) error {
	rom, err := snes.NewROM(v.Name, romImage)
	if err != nil {
		return err
	}
	return v.c.ROMSelected(rom)
}
