package retroarch

import (
	"fmt"
	"o2/snes"
	"strings"
	"sync"
	"time"
)

type Queue struct {
	snes.BaseQueue

	closed chan struct{}

	c    *RAClient
	lock sync.Mutex
}

func (q *Queue) IsTerminalError(err error) bool {
	return false
}

func (q *Queue) Closed() <-chan struct{} {
	return q.closed
}

func (q *Queue) Close() error {
	defer q.lock.Unlock()
	q.lock.Lock()

	// don't close the underlying connection since it is reused for detection.

	if q.c == nil {
		return nil
	}

	q.c = nil
	close(q.closed)

	return nil
}

func (q *Queue) Init() {
	q.closed = make(chan struct{})
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

func (r *readCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
	q, ok := queue.(*Queue)
	if !ok {
		return fmt.Errorf("queue is not of expected internal type")
	}

	// nowhere to put the response?
	completed := r.Request.Completion
	if completed == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("READ_CORE_RAM ")
	// TODO: translate address to bus address
	sb.WriteString(fmt.Sprintf("%06x %d\n", r.Request.Address, r.Request.Size))
	reqStr := sb.String()

	err = q.c.WriteTimeout([]byte(reqStr), time.Second)
	if err != nil {
		q.Close()
		return
	}

	var data []byte
	data, err = q.c.ReadTimeout(time.Second)
	if err != nil {
		q.Close()
		return
	}

	completed(snes.Response{
		IsWrite: false,
		Address: r.Request.Address,
		Size:    r.Request.Size,
		Extra:   r.Request.Extra,
		Data:    data,
	})

	return nil
}

type writeCommand struct {
	Request snes.Write
}

func (r *writeCommand) Execute(_ snes.Queue, keepAlive snes.KeepAlive) error {
	<-time.After(time.Millisecond * 1)

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
