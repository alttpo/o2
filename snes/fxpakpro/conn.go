package fxpakpro

import (
	"fmt"
	"o2/snes"
)

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

func (c *Conn) SubmitRead(reqs []snes.ReadRequest) {
	for len(reqs) >= 8 {
		// queue up a VGET command:
		batch := reqs[:8]
		c.cq <- c.newVGET(batch)

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		c.cq <- c.newVGET(reqs)
	}
}

func (c *Conn) SubmitWrite(reqs []snes.WriteRequest) {
	for len(reqs) >= 8 {
		// queue up a VPUT command:
		batch := reqs[:8]
		c.cq <- c.newVPUT(batch)

		// move to next batch:
		reqs = reqs[8:]
	}

	if len(reqs) > 0 && len(reqs) <= 8 {
		c.cq <- c.newVPUT(reqs)
	}
}
