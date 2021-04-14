package qusb2snes

import (
	"fmt"
	"o2/snes"
)

type Queue struct {
	snes.BaseQueue

	ws   WebSocketClient
	name string
}

func (q *Queue) Close() error {
	return q.ws.Close()
}

func (q *Queue) Init() {
	// TODO: Attach
}

func (q *Queue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	for _, req := range reqs {
		seq = append(seq, snes.CommandWithCompletion{
			Command:    &readCommand{req},
			Completion: batchComplete,
		})
	}
	return seq
}

func (q *Queue) MakeWriteCommands(reqs []snes.Write, batchComplete snes.Completion) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	for _, req := range reqs {
		seq = append(seq, snes.CommandWithCompletion{
			Command:    &writeCommand{req},
			Completion: batchComplete,
		})
	}
	return seq
}

type readCommand struct {
	Request snes.Read
}

func (r *readCommand) Execute(queue snes.Queue) error {
	q, ok := queue.(*Queue)
	if !ok {
		return fmt.Errorf("queue is not of expected internal type")
	}

	_ = q
	// TODO: GetAddress
	data := []byte{}

	completed := r.Request.Completion
	if completed != nil {
		completed(snes.Response{
			IsWrite: false,
			Address: r.Request.Address,
			Size:    r.Request.Size,
			Extra:   r.Request.Extra,
			Data:    data,
		})
	}

	return nil
}

type writeCommand struct {
	Request snes.Write
}

func (r *writeCommand) Execute(_ snes.Queue) error {

	// TODO: PutAddress
	// Qusb supports multiple chunks in one request!
	// https://github.com/Skarsnik/QUsb2snes/blob/master/usb2snes.h#L84

	completed := r.Request.Completion
	if completed != nil {
		completed(snes.Response{
			IsWrite: true,
			Address: r.Request.Address,
			Size:    r.Request.Size,
			Extra:   r.Request.Extra,
			Data:    r.Request.Data,
		})
	}
	return nil
}
