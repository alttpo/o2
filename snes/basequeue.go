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

func (b *BaseQueue) Enqueue(cmd CommandWithCompletion) error {
	if b.cqClosed {
		return fmt.Errorf("%s: device connection is closed", b.name)
	}
	b.cq <- cmd
	return nil
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
			pair.Completion(err)
		} else if err != nil {
			log.Println(err)
		}
	}
}

func (b *BaseQueue) MakeReadCommands(reqs []Read, complete func(error)) CommandSequence {
	panic("implement me")
}

func (b *BaseQueue) MakeWriteCommands(reqs []Write, complete func(error)) CommandSequence {
	panic("implement me")
}
