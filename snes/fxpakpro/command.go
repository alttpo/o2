package fxpakpro

import "go.bug.st/serial"

type Command interface {
	Execute(f serial.Port) error
}

type CommandWithCallback struct {
	Command    Command
	OnComplete func(error)
}

type DummyCommand struct {}

func (c *DummyCommand) Execute(f serial.Port) error {
	return nil
}
