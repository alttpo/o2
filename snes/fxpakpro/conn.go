package fxpakpro

import (
	"fmt"
	"go.bug.st/serial"
	"log"
	"o2/snes"
)

type Conn struct {
	snes.BaseConn

	// must be only accessed via Command.Execute
	f serial.Port
}

func (c *Conn) MakeReadCommands(reqs []snes.ReadRequest) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	for len(reqs) >= 8 {
		// queue up a VGET command:
		batch := reqs[:8]
		cmds = append(cmds, c.newVGET(batch))

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		cmds = append(cmds, c.newVGET(reqs))
	}

	return
}

func (c *Conn) MakeWriteCommands(reqs []snes.WriteRequest) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	for len(reqs) >= 8 {
		// queue up a VPUT command:
		batch := reqs[:8]
		cmds = append(cmds, c.newVPUT(batch))

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		cmds = append(cmds, c.newVPUT(reqs))
	}

	return
}

func (c *Conn) Close() (err error) {
	// Clear DTR (ignore any errors since we're closing):
	log.Println("fxpakpro: clear DTR")
	c.f.SetDTR(false)

	// Close the port:
	log.Println("fxpakpro: close port")
	err = c.f.Close()
	if err != nil {
		return fmt.Errorf("fxpakpro: could not close serial port: %w", err)
	}

	return
}
