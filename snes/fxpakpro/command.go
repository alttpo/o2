package fxpakpro

import "go.bug.st/serial"

type Command interface {
	Execute(f serial.Port) error
}

type CommandWithCallbacks struct {
	Command    Command
	OnComplete func()
	OnError    func(error)
}

type CallbackCommand struct {
	Callback func() error
}

func (c *CallbackCommand) Execute(f serial.Port) error {
	err := c.Callback()
	return err
}
