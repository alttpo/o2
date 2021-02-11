package snes

import (
	"fmt"
	"io"
	"log"
)

type BaseConn struct {
	// driver name
	name string

	// command execution queue:
	cq chan CommandWithCallback
	cqClosed bool

	closer io.Closer
}

func (c *BaseConn) Init(name string, closer io.Closer) {
	c.name = name
	c.cq = make(chan CommandWithCallback, 64)
	c.closer = closer

	go c.handleQueue()
}

func (c *BaseConn) Enqueue(cmd Command) {
	if c.cqClosed {
		return
	}
	c.cq <- CommandWithCallback{
		Command:    cmd,
		OnComplete: nil,
	}
}

func (c *BaseConn) EnqueueWithCallback(cmd Command, onComplete func(err error)) {
	if c.cqClosed {
		if onComplete != nil {
			onComplete(fmt.Errorf("%s: device connection is closed", c.name))
		}
		return
	}
	c.cq <- CommandWithCallback{
		Command:    cmd,
		OnComplete: onComplete,
	}
}

func (c *BaseConn) EnqueueMulti(cmds CommandSequence) {
	if c.cqClosed {
		return
	}
	for _, cmd := range cmds {
		c.cq <- CommandWithCallback{
			Command:    cmd,
			OnComplete: nil,
		}
	}
}

func (c *BaseConn) EnqueueMultiWithCallback(cmds CommandSequence, onComplete func(err error)) {
	if c.cqClosed {
		if onComplete != nil {
			onComplete(fmt.Errorf("%s: device connection is closed", c.name))
		}
		return
	}

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
	c.cqClosed = false
	var err error
	doClose := func() {
		if c.cqClosed {
			log.Printf("%s: already closed\n", c.name)
			return
		}

		if err != nil {
			log.Printf("%s: %v\n", c.name, err)
		}

		log.Printf("%s: calling Close()\n", c.name)
		if c.closer != nil {
			err = c.closer.Close()
			if err != nil {
				log.Printf("%s: %v\n", c.name, err)
			}
		}

		log.Printf("%s: closing chan\n", c.name)
		close(c.cq)
		c.cqClosed = true
	}
	defer doClose()

	for pair := range c.cq {
		cmd := pair.Command
		if cmd == nil {
			break
		}
		if _, ok := cmd.(*CloseCommand); ok {
			log.Printf("%s: processing CloseCommand\n", c.name)
			doClose()
		}
		if _, ok := cmd.(*DrainQueueCommand); ok {
			// close and recreate queue:
			log.Printf("%s: processing DrainQueueCommand\n", c.name)
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
