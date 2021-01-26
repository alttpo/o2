package mock

import "o2/snes"

type Conn struct {
}

func (c *Conn) Enqueue(cmd snes.Command) {
	panic("implement me")
}

func (c *Conn) EnqueueWithCallback(cmd snes.Command, onComplete func(err error)) {
	panic("implement me")
}

func (c *Conn) EnqueueMulti(cmds snes.CommandSequence) {
	panic("implement me")
}

func (c *Conn) EnqueueMultiWithCallback(cmds snes.CommandSequence, onComplete func(err error)) {
	panic("implement me")
}

func (c *Conn) MakeReadCommands(reqs []snes.ReadRequest) snes.CommandSequence {
	panic("implement me")
}

func (c *Conn) MakeWriteCommands(reqs []snes.WriteRequest) snes.CommandSequence {
	panic("implement me")
}
