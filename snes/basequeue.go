package snes

import (
	"fmt"
	"io"
	"log"
)

type BaseQueue struct {
	// driver name
	name string

	// command execution queue:
	cq       chan CommandWithCompletion
	cqClosed bool

	closer io.Closer
}

func (q *BaseQueue) Init(name string, closer io.Closer) {
	q.name = name
	q.cq = make(chan CommandWithCompletion, 64)
	q.closer = closer

	go q.handleQueue()
}

func (q *BaseQueue) Enqueue(cmd Command) {
	if q.cqClosed {
		return
	}
	q.cq <- CommandWithCompletion{
		Command:    cmd,
		Completion: nil,
	}
}

func (q *BaseQueue) EnqueueWithCompletion(cmd Command, completion chan<- error) {
	if q.cqClosed {
		if completion != nil {
			completion <- fmt.Errorf("%s: device connection is closed", q.name)
		}
		return
	}
	q.cq <- CommandWithCompletion{
		Command:    cmd,
		Completion: completion,
	}
}

func (q *BaseQueue) EnqueueMulti(cmds CommandSequence) {
	if q.cqClosed {
		return
	}
	for _, cmd := range cmds {
		q.cq <- CommandWithCompletion{
			Command:    cmd,
			Completion: nil,
		}
	}
}

func (q *BaseQueue) EnqueueMultiWithCompletion(cmds CommandSequence, completion chan<- error) {
	if q.cqClosed {
		if completion != nil {
			completion <- fmt.Errorf("%s: device connection is closed", q.name)
		}
		return
	}

	// enqueue all commands except the last without a callback:
	last := len(cmds) - 1
	if last > 0 {
		for _, cmd := range cmds[0 : last-1] {
			q.cq <- CommandWithCompletion{
				Command:    cmd,
				Completion: nil,
			}
		}
	}

	// only supply a callback to the last command in the sequence:
	q.cq <- CommandWithCompletion{
		Command:    cmds[last],
		Completion: completion,
	}
}

func (q *BaseQueue) handleQueue() {
	q.cqClosed = false
	var err error
	doClose := func() {
		if q.cqClosed {
			log.Printf("%s: already closed\n", q.name)
			return
		}

		if err != nil {
			log.Printf("%s: %v\n", q.name, err)
		}

		log.Printf("%s: calling Close()\n", q.name)
		if q.closer != nil {
			err = q.closer.Close()
			if err != nil {
				log.Printf("%s: %v\n", q.name, err)
			}
		}

		log.Printf("%s: closing chan\n", q.name)
		close(q.cq)
		q.cqClosed = true
	}
	defer doClose()

	for pair := range q.cq {
		cmd := pair.Command
		if cmd == nil {
			break
		}
		if _, ok := cmd.(*CloseCommand); ok {
			log.Printf("%s: processing CloseCommand\n", q.name)
			doClose()
		}
		if _, ok := cmd.(*DrainQueueCommand); ok {
			// close and recreate queue:
			log.Printf("%s: processing DrainQueueCommand\n", q.name)
			close(q.cq)
			q.cq = make(chan CommandWithCompletion, 64)
		}

		err = cmd.Execute(q)
		if pair.Completion != nil {
			pair.Completion <- err
		} else if err != nil {
			log.Println(err)
		}
	}
}

func (q *BaseQueue) MakeReadCommands(reqs []ReadRequest) CommandSequence {
	panic("implement me")
}

func (q *BaseQueue) MakeWriteCommands(reqs []WriteRequest) CommandSequence {
	panic("implement me")
}
