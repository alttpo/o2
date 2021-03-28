package snes

import (
	"fmt"
	"log"
)

type BaseQueue struct {
	// driver name
	name string

	// command execution queue:
	cq       chan CommandWithCompletion
	cqClosed bool

	// derived Queue struct:
	queue Queue
}

func (b *BaseQueue) BaseInit(name string, queue Queue) {
	if queue == nil {
		panic("queue must not be nil")
	}

	b.name = name
	b.cq = make(chan CommandWithCompletion, 64)
	b.queue = queue

	go b.handleQueue()
}

func (b *BaseQueue) Enqueue(cmd Command) {
	if b.cqClosed {
		return
	}
	b.cq <- CommandWithCompletion{
		Command:    cmd,
		Completion: nil,
	}
}

func (b *BaseQueue) EnqueueWithCompletion(cmd Command, completion chan<- error) {
	if b.cqClosed {
		if completion != nil {
			completion <- fmt.Errorf("%s: device connection is closed", b.name)
		}
		return
	}
	b.cq <- CommandWithCompletion{
		Command:    cmd,
		Completion: completion,
	}
}

func (b *BaseQueue) EnqueueMulti(cmds CommandSequence) {
	if b.cqClosed {
		return
	}
	for _, cmd := range cmds {
		b.cq <- CommandWithCompletion{
			Command:    cmd,
			Completion: nil,
		}
	}
}

func (b *BaseQueue) EnqueueMultiWithCompletion(cmds CommandSequence, completion chan<- error) {
	if b.cqClosed {
		if completion != nil {
			completion <- fmt.Errorf("%s: device connection is closed", b.name)
		}
		return
	}

	// enqueue all commands except the last without a callback:
	last := len(cmds) - 1
	if last > 0 {
		for _, cmd := range cmds[0 : last-1] {
			b.cq <- CommandWithCompletion{
				Command:    cmd,
				Completion: nil,
			}
		}
	}

	// only supply a callback to the last command in the sequence:
	b.cq <- CommandWithCompletion{
		Command:    cmds[last],
		Completion: completion,
	}
}

func (b *BaseQueue) handleQueue() {
	q := b.queue

	b.cqClosed = false
	var err error
	doClose := func() {
		if b.cqClosed {
			log.Printf("%s: already closed\n", b.name)
			return
		}

		if err != nil {
			log.Printf("%s: %v\n", b.name, err)
		}

		log.Printf("%s: calling Close()\n", b.name)
		if q != nil {
			err = q.Close()
			if err != nil {
				log.Printf("%s: %v\n", b.name, err)
			}
		}

		log.Printf("%s: closing chan\n", b.name)
		close(b.cq)
		b.cqClosed = true
	}
	defer doClose()

	for pair := range b.cq {
		cmd := pair.Command
		if cmd == nil {
			break
		}
		if _, ok := cmd.(*CloseCommand); ok {
			log.Printf("%s: processing CloseCommand\n", b.name)
			doClose()
		}
		if _, ok := cmd.(*DrainQueueCommand); ok {
			// close and recreate queue:
			log.Printf("%s: processing DrainQueueCommand\n", b.name)
			close(b.cq)
			b.cq = make(chan CommandWithCompletion, 64)
		}

		err = cmd.Execute(q)
		if pair.Completion != nil {
			pair.Completion <- err
		} else if err != nil {
			log.Println(err)
		}
	}
}

func (b *BaseQueue) MakeReadCommands(reqs ...Read) CommandSequence {
	panic("implement me")
}

func (b *BaseQueue) MakeWriteCommands(reqs ...Write) CommandSequence {
	panic("implement me")
}
