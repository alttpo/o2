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

func (seq CommandSequence) EnqueueTo(queue Queue) (err error) {
	for _, cmd := range seq {
		err = queue.Enqueue(cmd)
		if err != nil {
			return
		}
	}
	return
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
