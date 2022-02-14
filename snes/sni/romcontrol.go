package sni

import (
	"context"
	"o2/snes"
	"path/filepath"
)

type uploadROM struct {
	path string
	rom  []byte
}

func (c *uploadROM) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
	q := queue.(*Queue)
	ctx := context.TODO()

	var rsp *PutFileResponse
	rsp, err = q.filesystemClient.PutFile(ctx, &PutFileRequest{
		Uri:  q.uri,
		Path: c.path,
		Data: c.rom,
	})
	if err != nil {
		return
	}
	_ = rsp

	return
}

func (q *Queue) MakeUploadROMCommands(folder string, filename string, rom []byte) (path string, cmds snes.CommandSequence) {
	path = filepath.Join(folder, filename)
	cmds = snes.CommandSequence{
		snes.CommandWithCompletion{
			Command:    &uploadROM{path: path, rom: rom},
			Completion: nil,
		},
	}
	return
}

type bootROM struct {
	path string
}

func (c *bootROM) Execute(queue snes.Queue, keepAlive snes.KeepAlive) (err error) {
	q := queue.(*Queue)
	ctx := context.TODO()

	var rsp *BootFileResponse
	rsp, err = q.filesystemClient.BootFile(ctx, &BootFileRequest{
		Uri:  q.uri,
		Path: c.path,
	})
	if err != nil {
		return
	}
	_ = rsp

	return
}

func (q *Queue) MakeBootROMCommands(path string) snes.CommandSequence {
	return snes.CommandSequence{
		snes.CommandWithCompletion{
			Command:    &bootROM{path: path},
			Completion: nil,
		},
	}
}
