package fxpakpro

import (
	"fmt"
	"go.bug.st/serial"
	"log"
	"o2/snes"
)

type Conn struct {
	// must be only accessed via Command.Execute
	f serial.Port

	// command execution queue:
	cq chan snes.CommandWithCallback
}

func (c *Conn) Enqueue(cmd snes.Command) {
	c.cq <- snes.CommandWithCallback{
		Command:    cmd,
		OnComplete: nil,
	}
}

func (c *Conn) EnqueueWithCallback(cmd snes.Command, onComplete func(err error)) {
	c.cq <- snes.CommandWithCallback{
		Command:    cmd,
		OnComplete: onComplete,
	}
}

func (c *Conn) EnqueueMulti(cmds snes.CommandSequence) {
	for _, cmd := range cmds {
		c.cq <- snes.CommandWithCallback{
			Command:    cmd,
			OnComplete: nil,
		}
	}
}

func (c *Conn) EnqueueMultiWithCallback(cmds snes.CommandSequence, onComplete func(err error)) {
	// enqueue all commands except the last without a callback:
	last := len(cmds)-1
	if last > 0 {
		for _, cmd := range cmds[0 : last-1] {
			c.cq <- snes.CommandWithCallback{
				Command:    cmd,
				OnComplete: nil,
			}
		}
	}

	// only supply a callback to the last command in the sequence:
	c.cq <- snes.CommandWithCallback{
		Command:    cmds[last],
		OnComplete: onComplete,
	}
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

func (c *Conn) handleQueue() {
	var err error
	defer func() {
		if err != nil {
			log.Printf("fxpakpro: %v\n", err)
		}

		err = c.Close()
		if err != nil {
			log.Printf("fxpakpro: %v\n", err)
		}

		close(c.cq)
	}()

	for {
		pair := <-c.cq
		cmd := pair.Command
		if cmd == nil {
			break
		}
		if _, ok := cmd.(*snes.CloseCommand); ok {
			break
		}
		if _, ok := cmd.(*snes.DrainQueueCommand); ok {
			// close and recreate queue:
			close(c.cq)
			c.cq = make(chan snes.CommandWithCallback, 64)
		}

		err = cmd.Execute(c)
		if pair.OnComplete != nil {
			pair.OnComplete(err)
		} else if err != nil {
			log.Println(err)
		}
	}
}

func (c *Conn) Close() (err error) {
	// Clear DTR (ignore any errors since we're closing):
	c.f.SetDTR(false)

	// Close the port:
	err = c.f.Close()
	if err != nil {
		return fmt.Errorf("fxpakpro: could not close serial port: %w", err)
	}

	return
}
