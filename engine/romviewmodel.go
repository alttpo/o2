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
		"boot": &ROMBootCommand{v},
		// get contents of patched rom:
		"patched": &ROMGetDataCommand{v},
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

// ROMGetDataCommand This command should only be used by the web server
type ROMGetDataCommand struct{ v *ROMViewModel }

func (ce *ROMGetDataCommand) CreateArgs() interfaces.CommandArgs { return nil }

func (ce *ROMGetDataCommand) Execute(args interfaces.CommandArgs) error {
	if ce.v.c.rom == nil {
		return nil
	}

	p, ok := args.(**snes.ROM)
	if !ok {
		return nil
	}

	*p = ce.v.c.rom
	return nil
}

type ROMBootCommand struct{ v *ROMViewModel }

func (ce *ROMBootCommand) CreateArgs() interfaces.CommandArgs { return nil }
func (ce *ROMBootCommand) Execute(_ interfaces.CommandArgs) (err error) {
	rom := ce.v.c.rom
	if rom == nil {
		return fmt.Errorf("rom not loaded")
	}
	queue := ce.v.c.dev
	if queue == nil {
		return fmt.Errorf("SNES not connected")
	}

	rc, ok := queue.(snes.ROMControl)
	if !ok {
		return fmt.Errorf("SNES driver does not support booting ROMs")
	}

	path, cmds := rc.MakeUploadROMCommands(rom.Name, rom.Contents)
	err = cmds.EnqueueTo(queue)
	if err != nil {
		return fmt.Errorf("could not upload ROM: %w", err)
	}

	err = rc.MakeBootROMCommands(path).EnqueueTo(queue)
	if err != nil {
		return fmt.Errorf("could not boot ROM: %w", err)
	}

	return nil
}
