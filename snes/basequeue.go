package snes

import (
	"fmt"
	"log"
)

const chanSize = 8

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
	b.cq = make(chan CommandWithCompletion, chanSize)
	b.queue = queue

	go b.handleQueue()
}

func (b *BaseQueue) Enqueue(cmd CommandWithCompletion) (err error) {
	// FIXME: no great way I can figure out how to avoid panic on closed channel send below.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%s: device connection is closed", b.name)
		}
	}()

	if b.cqClosed {
		err = fmt.Errorf("%s: device connection is closed", b.name)
		return
	}

	b.cq <- cmd
	return
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
		b.cqClosed = true
		close(b.cq)
		log.Printf("%s: closed chan\n", b.name)
	}
	defer doClose()

	for pair := range b.cq {
		//log.Printf("%s: dequeue command\n", b.name)
		cmd := pair.Command

		if cmd == nil {
			break
		}

		terminal := false

		if _, ok := cmd.(*CloseCommand); ok {
			log.Printf("%s: processing CloseCommand\n", b.name)
			terminal = true
		}
		if _, ok := cmd.(*DrainQueueCommand); ok {
			// close and recreate queue:
			log.Printf("%s: processing DrainQueueCommand\n", b.name)
			doClose()
			b.cq = make(chan CommandWithCompletion, chanSize)
		}

		err = cmd.Execute(q)
		// wrap the error if it is a terminal case:
		if err != nil && q.IsTerminalError(err) {
			err = ErrDeviceDisconnected{err}
			terminal = true
		}
		if pair.Completion != nil {
			pair.Completion(cmd, err)
		} else if err != nil {
			log.Println(err)
		}

		if terminal {
			break
		}
	}
}

func (b *BaseQueue) MakeReadCommands(reqs []Read, complete func(error)) CommandSequence {
	panic("implement me")
}

func (b *BaseQueue) MakeWriteCommands(reqs []Write, complete func(error)) CommandSequence {
	panic("implement me")
}
