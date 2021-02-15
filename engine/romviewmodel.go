package engine

import (
	"fmt"
	"o2/snes"
)

type ROMViewModel struct {
	commands map[string]CommandExecutor

	c *ViewModel

	// public fields for JSON:
	IsLoaded bool   `json:"isLoaded"`
	Name     string `json:"name"` // filename loaded from (no path)
	Title    string `json:"title"`
	Region   string `json:"region"`
	Version  string `json:"version"`
}

var regions = map[byte]string{
	0x00: "Japan",
	0x01: "North America",
	0x02: "Europe",
	0x03: "Sweden/Scandinavia",
	0x04: "Finland",
	0x05: "Denmark",
	0x06: "France",
	0x07: "Netherlands",
	0x08: "Spain",
	0x09: "Germany",
	0x0A: "Italy",
	0x0B: "China",
	0x0C: "Indonesia",
	0x0D: "Korea",
	0x0E: "Global (?)",
	0x0F: "Canada",
	0x10: "Brazil",
	0x11: "Australia",
	0x12: "Other (1)",
	0x13: "Other (2)",
	0x14: "Other (3)",
}

func (v *ROMViewModel) Update() {
	rom := v.c.nextRom
	v.IsLoaded = rom != nil

	if v.IsLoaded {
		v.Title = string(rom.Header.Title[:])
		v.Region = regions[rom.Header.DestinationCode]
		v.Version = fmt.Sprintf("1.%d", rom.Header.MaskROMVersion)
	} else {
		v.Title = ""
		v.Region = ""
		v.Version = ""
	}
}

func (v *ROMViewModel) CommandExecutor(command string) (ce CommandExecutor, err error) {
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

	v.commands = map[string]CommandExecutor{
		"name": &NameCommand{v},
		"data": &DataCommand{v},
	}

	return v
}

// Commands:

type NameCommand struct {
	v *ROMViewModel
}

type NameCommandArgs struct {
	Name string `json:"name"`
}

func (ce *NameCommand) CreateArgs() CommandArgs {
	return &NameCommandArgs{}
}
func (ce *NameCommand) Execute(args CommandArgs) error {
	return ce.v.NameProvided(args.(*NameCommandArgs))
}

func (v *ROMViewModel) NameProvided(args *NameCommandArgs) error {
	v.Name = args.Name
	return nil
}

type DataCommand struct {
	v *ROMViewModel
}

func (ce *DataCommand) CreateArgs() CommandArgs {
	panic("this is a binary command")
}
func (ce *DataCommand) Execute(args CommandArgs) error {
	return ce.v.DataProvided(args.([]byte))
}

func (v *ROMViewModel) DataProvided(romImage []byte) error {
	rom, err := snes.NewROM(v.Name, romImage)
	if err != nil {
		return err
	}
	return v.c.ROMSelected(rom)
}
