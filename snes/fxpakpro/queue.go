package fxpakpro

import (
	"fmt"
	"go.bug.st/serial"
	"log"
	"o2/snes"
)

type Queue struct {
	snes.BaseQueue

	// must be only accessed via Command.Execute
	f serial.Port
}

func (q *Queue) MakeReadCommands(reqs []snes.Read, batchComplete func(error)) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	for len(reqs) >= 8 {
		// queue up a VGET command:
		batch := reqs[:8]
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    q.newVGET(batch),
			Completion: batchComplete,
		})

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    q.newVGET(reqs),
			Completion: batchComplete,
		})
	}

	return
}

func (q *Queue) MakeWriteCommands(reqs []snes.Write, batchComplete func(error)) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	for len(reqs) >= 8 {
		// queue up a VPUT command:
		batch := reqs[:8]
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    q.newVPUT(batch),
			Completion: batchComplete,
		})

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		cmds = append(cmds, snes.CommandWithCompletion{
			Command:    q.newVPUT(reqs),
			Completion: batchComplete,
		})
	}

	return
}

func (q *Queue) Close() (err error) {
	// Clear DTR (ignore any errors since we're closing):
	log.Println("fxpakpro: clear DTR")
	q.f.SetDTR(false)

	// Close the port:
	log.Println("fxpakpro: close port")
	err = q.f.Close()
	if err != nil {
		return fmt.Errorf("fxpakpro: could not close serial port: %w", err)
	}

	return
}
