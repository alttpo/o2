package mock

import (
	"o2/snes"
	"time"
)

type Conn struct {
	snes.BaseConn
}

func (c *Conn) MakeReadCommands(reqs []snes.ReadRequest) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	for _, req := range reqs {
		seq = append(seq, &readCommand{req})
	}
	return seq
}

func (c *Conn) MakeWriteCommands(reqs []snes.WriteRequest) snes.CommandSequence {
	seq := make(snes.CommandSequence, 0, len(reqs))
	for _, req := range reqs {
		seq = append(seq, &writeCommand{req})
	}
	return seq
}

type readCommand struct {
	Request snes.ReadRequest
}

func (r *readCommand) Execute(conn snes.Conn) error {
	<-time.After(time.Millisecond*2)
	completed := r.Request.Completed
	if completed != nil {
		completed(snes.ReadOrWriteResponse{
			IsWrite: false,
			Address: r.Request.Address,
			Size:    r.Request.Size,
			Data:    make([]byte, r.Request.Size),
		})
	}
	return nil
}

type writeCommand struct {
	Request snes.WriteRequest
}

func (r *writeCommand) Execute(conn snes.Conn) error {
	<-time.After(time.Millisecond*2)
	completed := r.Request.Completed
	if completed != nil {
		completed(snes.ReadOrWriteResponse{
			IsWrite: true,
			Address: r.Request.Address,
			Size:    r.Request.Size,
			Data:    r.Request.Data,
		})
	}
	return nil
}
