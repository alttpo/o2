package fxpakpro

import (
	"fmt"
	"o2/snes"
)

type vput struct {
	batch      []snes.WriteRequest
	onResponse func()
}

func (c *Conn) newVPUT(batch []snes.WriteRequest) *vput {
	return &vput{
		batch: batch,
		onResponse: func() {
			// make completed callbacks:
			for i := 0; i < len(batch); i++ {
				// make response callback:
				cb := batch[i].Completed
				if cb != nil {
					cb(snes.ReadOrWriteResponse{
						IsWrite: false,
						Address: batch[i].Address,
						Size:    batch[i].Size,
						Data:    batch[i].Data,
					})
				}
			}
		},
	}
}

// Command interface:
func (c *vput) Execute(conn snes.Conn) error {
	f := conn.(*Conn).f

	reqs := c.batch
	if len(reqs) > 8 {
		return fmt.Errorf("vput: cannot have more than 8 requests in batch")
	}

	sb := make([]byte, 64)
	sb[0] = byte('U')
	sb[1] = byte('S')
	sb[2] = byte('B')
	sb[3] = byte('A')
	sb[4] = byte(OpVPUT)
	sb[5] = byte(SpaceSNES)
	sb[6] = byte(FlagDATA64B | FlagNORESP)

	total := 0
	for i := 0; i < len(reqs); i++ {
		// 4-byte struct: 1 byte size, 3 byte address
		sb[32+(i*4)] = reqs[i].Size
		sb[33+(i*4)] = byte((reqs[i].Address >> 16) & 0xFF)
		sb[34+(i*4)] = byte((reqs[i].Address >> 8) & 0xFF)
		sb[35+(i*4)] = byte((reqs[i].Address >> 0) & 0xFF)
		total += int(reqs[i].Size)
	}

	err := sendSerial(f, sb)
	if err != nil {
		return err
	}

	// calculate expected number of packets:
	packets := total / 64
	remainder := total & 63
	if remainder > 0 {
		packets++
	}

	// concatenate all accompanying data together in one large slice:
	expected := packets * 64
	whole := make([]byte, expected)
	o := 0
	for i := 0; i < len(reqs); i++ {
		copy(whole[o:], reqs[i].Data)
		o += len(reqs[i].Data)
	}

	// send the expected number of 64-byte packets:
	err = sendSerial(f, whole)
	if err != nil {
		return err
	}

	// callback:
	cb := c.onResponse
	if cb != nil {
		cb()
	}

	return nil
}
