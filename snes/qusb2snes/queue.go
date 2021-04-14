package qusb2snes

import (
	"fmt"
	"github.com/gobwas/ws"
	"io/ioutil"
	"log"
	"o2/snes"
)

type Queue struct {
	snes.BaseQueue

	deviceName string
	ws         WebSocketClient
}

func (q *Queue) Close() error {
	return q.ws.Close()
}

func (q *Queue) Init() (err error) {
	// attach to this device:
	err = q.ws.SendCommand(qusbCommand{
		Opcode:   "Name",
		Space:    "SNES",
		Operands: []string{q.deviceName},
	})
	return
}

func (q *Queue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	seq = append(seq, snes.CommandWithCompletion{
		Command:    &readCommand{reqs},
		Completion: batchComplete,
	})
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
	Requests []snes.Read
}

func (r *readCommand) Execute(queue snes.Queue) (err error) {
	q, ok := queue.(*Queue)
	if !ok {
		return fmt.Errorf("qusb2snes: readCommand: queue is not of expected internal type")
	}

	operands := make([]string, 0, 2*len(r.Requests))
	for _, req := range r.Requests {
		operands = append(operands, fmt.Sprintf("%x", req.Address), fmt.Sprintf("%x", req.Size))
	}

	log.Printf("qusb2snes: readCommand: GetAddress %d requests\n", len(r.Requests))
	err = q.ws.SendCommand(qusbCommand{
		Opcode:   "GetAddress",
		Space:    "SNES",
		Operands: operands,
	})
	if err != nil {
		return
	}

	for i, req := range r.Requests {
		log.Printf("qusb2snes: readCommand: expecting binary message, %x bytes for requests[%d]\n", req.Size, i)
		var hdr ws.Header
		hdr, err = q.ws.r.NextFrame()
		if err != nil {
			err = fmt.Errorf("qusb2snes: readCommand: NextFrame: %w", err)
			return
		}
		if hdr.OpCode == ws.OpClose {
			q.ws.Close()
			return
		}
		if hdr.OpCode != ws.OpBinary {
			log.Printf("qusb2snes: readCommand: unexpected opcode %#x (expecting %#x)\n", hdr.OpCode, ws.OpBinary)
			return
		}

		var data []byte
		data, err = ioutil.ReadAll(q.ws.r)
		if err != nil {
			err = fmt.Errorf("qusb2snes: readCommand: error reading binary response: %w", err)
			return
		}
		log.Printf("qusb2snes: readCommand: %x binary bytes received\n", len(data))

		completed := req.Completion
		if completed != nil {
			completed(snes.Response{
				IsWrite: false,
				Address: req.Address,
				Size:    req.Size,
				Extra:   req.Extra,
				Data:    data,
			})
		}
	}

	return
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
