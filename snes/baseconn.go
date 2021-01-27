package snes

import "log"

type BaseConn struct {
	// driver name
	name string

	// command execution queue:
	cq chan CommandWithCallback
}

func (c *BaseConn) Close() (err error) {
	return nil
}

func (c *BaseConn) Init(name string) {
	c.name = name
	c.cq = make(chan CommandWithCallback, 64)

	go c.handleQueue()
}

func (c *BaseConn) Enqueue(cmd Command) {
	c.cq <- CommandWithCallback{
		Command:    cmd,
		OnComplete: nil,
	}
}

func (c *BaseConn) EnqueueWithCallback(cmd Command, onComplete func(err error)) {
	c.cq <- CommandWithCallback{
		Command:    cmd,
		OnComplete: onComplete,
	}
}

func (c *BaseConn) EnqueueMulti(cmds CommandSequence) {
	for _, cmd := range cmds {
		c.cq <- CommandWithCallback{
			Command:    cmd,
			OnComplete: nil,
		}
	}
}

func (c *BaseConn) EnqueueMultiWithCallback(cmds CommandSequence, onComplete func(err error)) {
	// enqueue all commands except the last without a callback:
	last := len(cmds) - 1
	if last > 0 {
		for _, cmd := range cmds[0 : last-1] {
			c.cq <- CommandWithCallback{
				Command:    cmd,
				OnComplete: nil,
			}
		}
	}

	// only supply a callback to the last command in the sequence:
	c.cq <- CommandWithCallback{
		Command:    cmds[last],
		OnComplete: onComplete,
	}
}

func (c *BaseConn) handleQueue() {
	isClosed := false
	var err error
	doClose := func() {
		if isClosed {
			return
		}

		if err != nil {
			log.Printf("%s: %v\n", c.name, err)
		}

		err = c.Close()
		if err != nil {
			log.Printf("%s: %v\n", c.name, err)
		}

		close(c.cq)
		isClosed = true
	}
	defer doClose()

	for pair := range c.cq {
		cmd := pair.Command
		if cmd == nil {
			break
		}
		if _, ok := cmd.(*CloseCommand); ok {
			doClose()
		}
		if _, ok := cmd.(*DrainQueueCommand); ok {
			// close and recreate queue:
			close(c.cq)
			c.cq = make(chan CommandWithCallback, 64)
		}

		err = cmd.Execute(c)
		if pair.OnComplete != nil {
			pair.OnComplete(err)
		} else if err != nil {
			log.Println(err)
		}
	}
}

func (c *BaseConn) MakeReadCommands(reqs []ReadRequest) CommandSequence {
	panic("implement me")
}

func (c *BaseConn) MakeWriteCommands(reqs []WriteRequest) CommandSequence {
	panic("implement me")
}
