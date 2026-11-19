package snes

import (
	"log"
	"o2/util"
	"sync"
	"time"
)

const chanSize = 1

type BaseQueue struct {
	// driver name
	name string

	// command execution queue; it is never closed, so Enqueue can never send on a closed channel. closed is closed
	// instead when handleQueue stops, and sendMu lets doClose wait out any Enqueue that is mid-send before it fails
	// the commands left in cq:
	cq     chan CommandWithCompletion
	closed chan struct{}
	sendMu sync.RWMutex

	// derived Queue struct:
	queue Queue
}

func (b *BaseQueue) BaseInit(name string, queue Queue) {
	if queue == nil {
		panic("queue must not be nil")
	}

	b.name = name
	b.cq = make(chan CommandWithCompletion, chanSize)
	b.closed = make(chan struct{})
	b.queue = queue

	go b.handleQueue()
}

func (b *BaseQueue) Enqueue(cmd CommandWithCompletion) (err error) {
	b.sendMu.RLock()
	defer b.sendMu.RUnlock()

	// check closed before the select below, which would pick a free cq slot at random over a closed queue:
	select {
	case <-b.closed:
		return &TerminalError{ErrDeviceDisconnected}
	default:
	}

	// don't need a timeout here since the queue should always guarantee process forward with its own timeouts
	select {
	case b.cq <- cmd:
		return nil
	case <-b.closed:
		return &TerminalError{ErrDeviceDisconnected}
	}
}

func (b *BaseQueue) handleQueue() {
	defer func() {
		if r := recover(); r != nil {
			util.LogPanic(r)
		}
	}()

	q := b.queue

	var err error
	doClose := func() {
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
		// unblock waiting Enqueues, then wait for them to finish, after which every Enqueue sees closed and nothing
		// more can land in cq:
		close(b.closed)
		b.sendMu.Lock()
		var stranded []CommandWithCompletion
	drain:
		for {
			select {
			case pair := <-b.cq:
				stranded = append(stranded, pair)
			default:
				break drain
			}
		}
		b.sendMu.Unlock()
		log.Printf("%s: closed chan\n", b.name)

		// fail the commands that were queued but never executed, so nothing waits on them forever:
		for _, pair := range stranded {
			if pair.Completion != nil {
				pair.Completion(pair.Command, &TerminalError{ErrDeviceDisconnected})
			}
		}
	}
	defer doClose()

channelLoop:
	for pair := range b.cq {
		//log.Printf("%s: dequeue command; len = %d\n", b.name, len(b.cq))
		cmd := pair.Command

		if cmd == nil {
			break
		}

		terminal := false

		if _, ok := cmd.(*CloseCommand); ok {
			log.Printf("%s: processing CloseCommand\n", b.name)
			terminal = true
		}

		{
			// give the command 15 seconds to execute:
			const timeoutDuration = time.Second * 15

			done := make(chan struct{})
			keepAlive := make(chan struct{}, 16)
			started := time.Now()
			go func() {
				defer func() {
					if err := recover(); err != nil {
						util.LogPanic(err)
					}

					close(done)
				}()

				err = cmd.Execute(q, keepAlive)
			}()

			timeout := time.NewTimer(timeoutDuration)
		timeoutLoop:
			for {
				select {
				case <-done:
					timeout.Stop()
					break timeoutLoop
				case <-keepAlive:
					timeout.Reset(timeoutDuration)
					break
				case <-timeout.C:
					timeout.Stop()
					log.Printf("%s: timed out executing command\n", b.name)
					break channelLoop
				}
			}
			stopped := time.Now()
			executionTime := stopped.Sub(started).Milliseconds()
			_ = executionTime
			//log.Printf("%s: command execution took %d msec", b.name, executionTime)
		}

		// wrap the error if it is a terminal case:
		if err != nil && q.IsTerminalError(err) {
			log.Printf("snes: basequeue: handleQueue: terminal error: %v\n", err)
			err = &TerminalError{err}
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
