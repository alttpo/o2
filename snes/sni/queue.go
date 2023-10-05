package sni

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"o2/snes"
)

type Queue struct {
	snes.BaseQueue

	closed   chan struct{}
	isClosed bool

	uri              string
	memoryClient     DeviceMemoryClient
	filesystemClient DeviceFilesystemClient
}

func (q *Queue) IsTerminalError(err error) bool {
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.Unknown {
			return true
		}
		if st.Code() == codes.Internal {
			return true
		}
	}
	return false
}

func (q *Queue) Closed() <-chan struct{} {
	return q.closed
}

func (q *Queue) Close() (err error) {
	if q.isClosed {
		return
	}

	// make sure closed channel is closed:
	close(q.closed)
	q.isClosed = true

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
