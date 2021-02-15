package engine

import (
	"fmt"
	"o2/snes"
)

type ROMViewModel struct {
	commands map[string]CommandExecutor

	c *ViewModel

	romName  string
	romImage []byte
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
	v.romName = args.Name
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
	v.romImage = romImage
	rom, err := snes.NewROM(v.romName, v.romImage)
	if err != nil {
		return err
	}
	return v.c.ROMSelected(rom)
}
