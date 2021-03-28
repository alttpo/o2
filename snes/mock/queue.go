package mock

import (
	"fmt"
	"o2/snes"
	"time"
)

type Queue struct {
	snes.BaseQueue

	wram        [0x20000]byte
	nothing     [0x100]byte

	frameTicker *time.Ticker
}

func (q *Queue) Close() error {
	q.frameTicker.Stop()
	q.frameTicker = nil
	return nil
}

func (q *Queue) Init() {
	q.frameTicker = time.NewTicker(16_639_265 * time.Nanosecond)
	go func() {
		// 5,369,317.5/89,341.5 ~= 60.0988 frames / sec ~= 16,639,265.605 ns / frame
		for range q.frameTicker.C {
			// increment frame timer:
			q.wram[0x1A]++
		}
	}()
}

func (q *Queue) MakeReadCommands(reqs ...snes.ReadRequest) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	for _, req := range reqs {
		seq = append(seq, &readCommand{req})
	}
	return seq
}

func (q *Queue) MakeWriteCommands(reqs ...snes.WriteRequest) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	for _, req := range reqs {
		seq = append(seq, &writeCommand{req})
	}
	return seq
}

type readCommand struct {
	Request snes.ReadRequest
}

func (r *readCommand) Execute(queue snes.Queue) error {
	q, ok := queue.(*Queue)
	if !ok {
		return fmt.Errorf("queue is not of expected internal type")
	}

	completed := r.Request.Completion
	if completed == nil {
		return nil
	}

	var data []byte
	if r.Request.Address >= 0xF50000 && r.Request.Address < 0xF70000 {
		// read from wram:
		data = q.wram[r.Request.Address-0xF50000 : r.Request.Size]
	} else {
		// read from nothing:
		data = q.nothing[0:r.Request.Size]
	}

	// wait 2ms before returning response to simulate the delay of FX Pak Pro device:
	<-time.After(time.Millisecond * 2)

	completed <- snes.ReadOrWriteResponse{
		IsWrite: false,
		Address: r.Request.Address,
		Size:    r.Request.Size,
		Data:    data,
	}

	return nil
}

type writeCommand struct {
	Request snes.WriteRequest
}

func (r *writeCommand) Execute(_ snes.Queue) error {
	<-time.After(time.Millisecond * 2)
	completed := r.Request.Completion
	if completed != nil {
		completed <- snes.ReadOrWriteResponse{
			IsWrite: true,
			Address: r.Request.Address,
			Size:    r.Request.Size,
			Data:    r.Request.Data,
		}
	}
	return nil
}
