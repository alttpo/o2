package retroarch

import (
	"bytes"
	"fmt"
	"log"
	"o2/snes"
	"o2/snes/lorom"
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

func (cmd *readCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
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
	completed := cmd.Request.Completion
	if completed == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("READ_CORE_RAM ")
	sb.WriteString(fmt.Sprintf("%06x %d\n", lorom.PakAddressToBus(cmd.Request.Address), cmd.Request.Size))
	reqStr := sb.String()

	err = c.WriteTimeout([]byte(reqStr), time.Millisecond * 200)
	if err != nil {
		q.Close()
		return
	}

	var rsp []byte
	rsp, err = c.ReadTimeout(time.Millisecond * 500)
	if err != nil {
		q.Close()
		return
	}

	// parse ASCII response:
	var n int
	var addr uint32
	r := bytes.NewReader(rsp)
	n, err = fmt.Fscanf(r, "READ_CORE_RAM %x", &addr)
	if err != nil {
		q.Close()
		return
	}

	data := make([]byte, 0, cmd.Request.Size)
	for {
		var v byte
		n, err = fmt.Fscanf(r, " %02x", &v)
		if err != nil || n == 0 {
			break
		}
		data = append(data, v)
	}

	completed(snes.Response{
		IsWrite: false,
		Address: cmd.Request.Address,
		Size:    cmd.Request.Size,
		Extra:   cmd.Request.Extra,
		Data:    data,
	})

	return nil
}

type writeCommand struct {
	Request snes.Write
}

const hextable = "0123456789abcdef"

func (cmd *writeCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
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
	sb.WriteString(fmt.Sprintf("%06x ", lorom.PakAddressToBus(cmd.Request.Address)))
	// emit hex data:
	lasti := len(cmd.Request.Data) - 1
	for i, v := range cmd.Request.Data {
		sb.WriteByte(hextable[(v>>4)&0xF])
		sb.WriteByte(hextable[v&0xF])
		if i < lasti {
			sb.WriteByte(' ')
		}
	}
	sb.WriteByte('\n')
	reqStr := sb.String()

	log.Printf("retroarch: > %s", reqStr)
	err = q.c.WriteTimeout([]byte(reqStr), time.Millisecond * 200)
	if err != nil {
		q.Close()
		return
	}

	completed := cmd.Request.Completion
	if completed != nil {
		completed(snes.Response{
			IsWrite: true,
			Address: cmd.Request.Address,
			Size:    cmd.Request.Size,
			Extra:   cmd.Request.Extra,
			Data:    cmd.Request.Data,
		})
	}

	err = nil
	return
}
