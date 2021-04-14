package qusb2snes

import (
	"fmt"
	"github.com/gobwas/ws/wsutil"
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
		Opcode:   "Attach",
		Space:    "SNES",
		Operands: []string{q.deviceName},
	})
	if err != nil {
		return
	}

	err = q.ws.SendCommand(qusbCommand{
		Opcode:   "Info",
		Space:    "SNES",
		Operands: []string{},
	})
	if err != nil {
		return
	}

	var rsp qusbResult
	err = q.ws.ReadCommandResponse("Info", &rsp)
	if err != nil {
		return
	}

	log.Printf("qusb2snes: [%s] Info = %v\n", q.ws.appName, rsp.Results)

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
	seq = append(seq, snes.CommandWithCompletion{
		Command:    &writeCommand{reqs},
		Completion: batchComplete,
	})
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

	// TODO: make sure device is ready and a game is loaded!

	operands := make([]string, 0, 2*len(r.Requests))
	sumExpected := 0
	for _, req := range r.Requests {
		operands = append(operands, fmt.Sprintf("%x", req.Address), fmt.Sprintf("%x", req.Size))
		sumExpected += int(req.Size)
	}

	//log.Printf("qusb2snes: readCommand: GetAddress %d requests\n", len(r.Requests))
	err = q.ws.SendCommand(qusbCommand{
		Opcode:   "GetAddress",
		Space:    "SNES",
		Operands: operands,
	})
	if err != nil {
		return
	}

	var dataReceived []byte
	dataReceived, err = q.ws.ReadBinaryResponse(sumExpected)
	if err != nil {
		return
	}

	n := 0
	for _, req := range r.Requests {
		size := int(req.Size)

		data := dataReceived[n : n+size]
		n += size

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
	Requests []snes.Write
}

func (r *writeCommand) Execute(queue snes.Queue) (err error) {
	q, ok := queue.(*Queue)
	if !ok {
		return fmt.Errorf("qusb2snes: writeCommand: queue is not of expected internal type")
	}

	operands := make([]string, 0, 2*len(r.Requests))
	for _, req := range r.Requests {
		operands = append(operands, fmt.Sprintf("%x", req.Address), fmt.Sprintf("%x", req.Size))
	}

	//log.Printf("qusb2snes: writeCommand: PutAddress %d requests\n", len(r.Requests))
	err = q.ws.SendCommand(qusbCommand{
		Opcode:   "PutAddress",
		Space:    "SNES",
		Operands: operands,
	})
	if err != nil {
		return
	}

	for _, req := range r.Requests {
		err = wsutil.WriteClientBinary(q.ws.ws, req.Data)
		if err != nil {
			err = fmt.Errorf("qusb2snes: writeCommand: writeClientBinary: %w", err)
			return
		}

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

	return
}
