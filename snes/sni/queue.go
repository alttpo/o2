package sni

import (
	"o2/snes"
)

type Queue struct {
	snes.BaseQueue

	closed chan struct{}

	uri              string
	memoryClient     DeviceMemoryClient
	filesystemClient DeviceFilesystemClient
}

func (q *Queue) IsTerminalError(err error) bool {
	return false
}

func (q *Queue) Closed() <-chan struct{} {
	return q.closed
}

func (q *Queue) Close() (err error) {
	// make sure closed channel is closed:
	defer close(q.closed)

	return
}

func (q *Queue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, 1)

	// queue up a MultiRead command:
	cmds = append(cmds, snes.CommandWithCompletion{
		Command:    &multiReadCommand{reqs: reqs},
		Completion: batchComplete,
	})

	return
}

func (q *Queue) MakeWriteCommands(reqs []snes.Write, batchComplete snes.Completion) (cmds snes.CommandSequence) {
	cmds = make(snes.CommandSequence, 0, len(reqs)/8+1)

	// queue up a MultiWrite command:
	cmds = append(cmds, snes.CommandWithCompletion{
		Command:    &multiWriteCommand{reqs: reqs},
		Completion: batchComplete,
	})

	return
}
