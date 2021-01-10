package fxpakpro

import "go.bug.st/serial"

type Command interface {
	Execute(f serial.Port) error
}

type CallbackCommand struct {
	Callback func() error
}

func (c *CallbackCommand) Execute(f serial.Port) error {
	return c.Callback()
}
