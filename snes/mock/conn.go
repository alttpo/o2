package mock

import "o2/snes"

type Conn struct {
	snes.BaseConn
}

func (c *Conn) MakeReadCommands(reqs []snes.ReadRequest) snes.CommandSequence {
	panic("implement me")
}

func (c *Conn) MakeWriteCommands(reqs []snes.WriteRequest) snes.CommandSequence {
	panic("implement me")
}
