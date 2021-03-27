package snes

type Command interface {
	Execute(conn Conn) error
}

type CommandSequence []Command

type CommandWithCompletion struct {
	Command    Command
	Completion chan<- error
}

type NoOpCommand struct{}

func (c *NoOpCommand) Execute(conn Conn) error {
	return nil
}

// Special Command to close the device connection
type CloseCommand struct{}

func (c *CloseCommand) Execute(conn Conn) error {
	return nil
}

// Special Command to drain any subsequent Commands from the queue without executing them
type DrainQueueCommand struct{}

func (c *DrainQueueCommand) Execute(conn Conn) error {
	return nil
}
