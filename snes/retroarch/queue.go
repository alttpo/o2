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

	q.lock.Lock()
	c := q.c
	q.lock.Unlock()
	if c == nil {
		return fmt.Errorf("retroarch: read: connection is closed")
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

	err = c.WriteTimeout([]byte(reqStr), time.Millisecond * 200)
	if err != nil {
		q.Close()
		return
	}

	var data []byte
	data, err = c.ReadTimeout(time.Millisecond * 500)
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

const hextable = "0123456789abcdef"

func (r *writeCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
	q, ok := queue.(*Queue)
	if !ok {
		return fmt.Errorf("queue is not of expected internal type")
	}

	q.lock.Lock()
	c := q.c
	q.lock.Unlock()
	if c == nil {
		return fmt.Errorf("retroarch: write: connection is closed")
	}

	var sb strings.Builder
	sb.WriteString("WRITE_CORE_RAM ")
	// TODO: translate address to bus address
	sb.WriteString(fmt.Sprintf("%06x ", r.Request.Address))
	// emit hex data:
	lasti := len(r.Request.Data) - 1
	for i, v := range r.Request.Data {
		sb.WriteByte(hextable[(v>>4)&0xF])
		sb.WriteByte(hextable[v&0xF])
		if i < lasti {
			sb.WriteByte(' ')
		}
	}
	sb.WriteByte('\n')
	reqStr := sb.String()

	err = q.c.WriteTimeout([]byte(reqStr), time.Millisecond * 200)
	if err != nil {
		q.Close()
		return
	}

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

	err = nil
	return
}
