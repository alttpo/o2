package qusb2snes

import (
	"encoding/json"
	"fmt"
	"o2/snes"
	"time"
)

type Queue struct {
	snes.BaseQueue

	d       *Driver
	encoder *json.Encoder
	decoder *json.Decoder
}

func (q *Queue) Close() error {
	// TODO: QUsb2Snes should have a "Detach" command
	return nil
}

func (q *Queue) Init() {
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

	// wait 2ms before returning response to simulate the delay of FX Pak Pro device:
	<-time.After(time.Millisecond * 2)

	completed := r.Request.Completion

	_ = q
	q.encoder.Encode(&map[string]interface{}{
	})
	data := []byte{}

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
	<-time.After(time.Millisecond * 2)
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
