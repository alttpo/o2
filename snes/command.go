package snes

type Command interface {
	Execute(queue Queue) error
}

type Completion func(Command, error)

type CommandWithCompletion struct {
	Command    Command
	Completion Completion
}

type CommandSequence []CommandWithCompletion

func (seq CommandSequence) EnqueueTo(queue Queue) error {
	for _, cmd := range seq {
		if err := queue.Enqueue(cmd); err != nil {
			return err
		}
	}
	return nil
}

type NoOpCommand struct{}

func (c *NoOpCommand) Execute(queue Queue) error {
	return nil
}

// Special Command to close the device connection
type CloseCommand struct{}

func (c *CloseCommand) Execute(queue Queue) error {
	return nil
}

// Special Command to drain any subsequent Commands from the queue without executing them
type DrainQueueCommand struct{}

func (c *DrainQueueCommand) Execute(queue Queue) error {
	return nil
}
