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
	c.q <- rwop{isRead: true, read: reqs}
}

func (c *Conn) SubmitWrite(reqs []snes.WriteRequest) {
	c.q <- rwop{isRead: false, write: reqs}
}
