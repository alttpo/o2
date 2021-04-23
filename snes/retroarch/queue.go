package retroarch

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"o2/snes"
	"o2/snes/lorom"
	"o2/udpclient"
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
	if errors.Is(err, udpclient.ErrTimeout) {
		return true
	}
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

func (q *Queue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	for len(reqs) >= 8 {
		// queue up a batch read command:
		batch := reqs[:8]
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    &readCommand{batch},
			Completion: batchComplete,
		})

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    &readCommand{reqs},
			Completion: batchComplete,
		})
	}

	return cmds
}

func (q *Queue) MakeWriteCommands(reqs []snes.Write, batchComplete snes.Completion) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	for len(reqs) >= 8 {
		// queue up a batch read command:
		batch := reqs[:8]
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    &writeCommand{batch},
			Completion: batchComplete,
		})

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    &writeCommand{reqs},
			Completion: batchComplete,
		})
	}

	return cmds
}

type readCommand struct {
	Batch []snes.Read
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
	keepAlive <- struct{}{}

	// build multiple requests:
	var sb strings.Builder
	for _, req := range cmd.Batch {
		// nowhere to put the response?
		completed := req.Completion
		if completed == nil {
			continue
		}

		sb.WriteString("READ_CORE_RAM ")
		expectedAddr := lorom.PakAddressToBus(req.Address)
		sb.WriteString(fmt.Sprintf("%06x %d\n", expectedAddr, req.Size))
	}

	reqStr := sb.String()
	var rsp []byte

	defer func() {
		c.Unlock()
		if err != nil {
			q.Close()
		}
	}()
	c.Lock()

	// send all commands up front in one packet:
	err = c.WriteTimeout([]byte(reqStr), time.Second*5)
	if err != nil {
		return
	}
	keepAlive <- struct{}{}

	// responses come in multiple packets:
	for _, req := range cmd.Batch {
		// nowhere to put the response?
		completed := req.Completion
		if completed == nil {
			continue
		}

		expectedAddr := lorom.PakAddressToBus(req.Address)

		rsp, err = c.ReadTimeout(time.Second * 5)
		if err != nil {
			return
		}
		keepAlive <- struct{}{}

		// parse ASCII response:
		r := bytes.NewReader(rsp)

		var n int
		var addr uint32
		n, err = fmt.Fscanf(r, "READ_CORE_RAM %x", &addr)
		if err != nil {
			return
		}
		if addr != expectedAddr {
			err = fmt.Errorf("retroarch: read response for wrong request %06x != %06x", addr, expectedAddr)
			return
		}

		data := make([]byte, 0, req.Size)
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
			Address: req.Address,
			Size:    req.Size,
			Extra:   req.Extra,
			Data:    data,
		})
	}

	err = nil
	return
}

type writeCommand struct {
	Batch []snes.Write
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
	keepAlive <- struct{}{}

	for _, req := range cmd.Batch {
		var sb strings.Builder
		sb.WriteString("WRITE_CORE_RAM ")
		sb.WriteString(fmt.Sprintf("%06x ", lorom.PakAddressToBus(req.Address)))
		// emit hex data:
		lasti := len(req.Data) - 1
		for i, v := range req.Data {
			sb.WriteByte(hextable[(v>>4)&0xF])
			sb.WriteByte(hextable[v&0xF])
			if i < lasti {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
		reqStr := sb.String()

		log.Printf("retroarch: > %s", reqStr)
		err = q.c.WriteTimeout([]byte(reqStr), time.Second*5)
		if err != nil {
			q.Close()
			return
		}
		keepAlive <- struct{}{}

		completed := req.Completion
		if completed != nil {
			completed(snes.Response{
				IsWrite: true,
				Address: req.Address,
				Size:    req.Size,
				Extra:   req.Extra,
				Data:    req.Data,
			})
		}
	}

	err = nil
	return
}
