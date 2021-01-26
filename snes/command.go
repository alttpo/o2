package snes

type Command interface {
	Execute(conn Conn) error
}

type CommandWithCallback struct {
	Command    Command
	OnComplete func(error)
}

type NoOpCommand struct{}

func (c *NoOpCommand) Execute(conn Conn) error {
	return nil
}

type CloseCommand struct{}

func (c *CloseCommand) Execute(conn Conn) error {
	return nil
}
