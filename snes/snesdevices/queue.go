package snesdevices

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/alttpo/snes-devices/devices"
	"github.com/alttpo/snes-devices/protos/sni"

	"o2/snes"
)

// commandTimeout bounds each device request; the drivers have their own I/O timeouts too.
const commandTimeout = 10 * time.Second

// romCommandTimeout bounds ROM uploads, which can take a while over USB.
const romCommandTimeout = 2 * time.Minute

// Queue adapts a devices.AutoCloseableDevice to snes.Queue.
type Queue struct {
	snes.BaseQueue

	name string
	dev  devices.AutoCloseableDevice

	// o2 addresses memory in the FX Pak Pro address space; the memory mapping lets non-FX Pak devices translate it.
	// All of o2's games are LoROM.
	mapping sni.MemoryMapping

	closeOnce sync.Once
	closed    chan struct{}
}

func newQueue(name string, dev devices.AutoCloseableDevice) *Queue {
	return &Queue{
		name:    name,
		dev:     dev,
		mapping: sni.MemoryMapping_LoROM,
		closed:  make(chan struct{}),
	}
}

func (q *Queue) Closed() <-chan struct{} { return q.closed }

func (q *Queue) Close() (err error) {
	q.closeOnce.Do(func() {
		err = q.dev.Close()
		close(q.closed)
	})
	return
}

// IsTerminalError reports fatal device errors so that o2 disconnects and resets the game state.
func (q *Queue) IsTerminalError(err error) bool {
	return devices.IsFatal(err)
}

func (q *Queue) address(addr uint32) devices.AddressTuple {
	return devices.AddressTuple{Address: addr, AddressSpace: sni.AddressSpace_FxPakPro, MemoryMapping: q.mapping}
}

func (q *Queue) MakeReadCommands(reqs []snes.Read, batchComplete snes.Completion) snes.CommandSequence {
	// the drivers batch reads themselves (e.g. FX Pak Pro VGETs of up to 8 reads), so one command suffices:
	return snes.CommandSequence{{Command: &readCommand{reqs: reqs}, Completion: batchComplete}}
}

func (q *Queue) MakeWriteCommands(reqs []snes.Write, batchComplete snes.Completion) snes.CommandSequence {
	return snes.CommandSequence{{Command: &writeCommand{reqs: reqs}, Completion: batchComplete}}
}

func queueOf(queue snes.Queue) (*Queue, error) {
	switch q := queue.(type) {
	case *Queue:
		return q, nil
	case *ROMControlQueue:
		return q.Queue, nil
	}
	return nil, fmt.Errorf("snesdevices: unexpected queue type %T", queue)
}

type readCommand struct {
	reqs []snes.Read
}

func (c *readCommand) Execute(queue snes.Queue, _ snes.KeepAlive) error {
	q, err := queueOf(queue)
	if err != nil {
		return err
	}

	reads := make([]devices.MemoryReadRequest, len(c.reqs))
	for i, r := range c.reqs {
		reads[i] = devices.MemoryReadRequest{RequestAddress: q.address(r.Address), Size: int(r.Size)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	rsps, err := q.dev.MultiReadMemory(ctx, reads...)
	if err != nil {
		return err
	}
	if len(rsps) != len(c.reqs) {
		return fmt.Errorf("%s: expected %d read responses; got %d", q.name, len(c.reqs), len(rsps))
	}

	for i, r := range c.reqs {
		if r.Completion == nil {
			continue
		}
		r.Completion(snes.Response{
			IsWrite: false,
			Address: r.Address,
			Size:    r.Size,
			Data:    rsps[i].Data,
			Extra:   r.Extra,
		})
	}
	return nil
}

type writeCommand struct {
	reqs []snes.Write
}

func (c *writeCommand) Execute(queue snes.Queue, _ snes.KeepAlive) error {
	q, err := queueOf(queue)
	if err != nil {
		return err
	}

	writes := make([]devices.MemoryWriteRequest, len(c.reqs))
	for i, w := range c.reqs {
		writes[i] = devices.MemoryWriteRequest{RequestAddress: q.address(w.Address), Data: w.Data}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	if _, err = q.dev.MultiWriteMemory(ctx, writes...); err != nil {
		return err
	}

	for _, w := range c.reqs {
		if w.Completion == nil {
			continue
		}
		w.Completion(snes.Response{
			IsWrite: true,
			Address: w.Address,
			Size:    w.Size,
			Data:    w.Data,
			Extra:   w.Extra,
		})
	}
	return nil
}

// ROMControlQueue is a Queue for devices that can upload and boot ROMs.
type ROMControlQueue struct {
	*Queue
}

func (q *ROMControlQueue) MakeUploadROMCommands(folder string, filename string, rom []byte) (path string, cmds snes.CommandSequence) {
	folder = strings.TrimRight(folder, "/")
	filename = strings.ToLower(strings.TrimLeft(filename, "/"))
	path = folder + "/" + filename

	cmds = snes.CommandSequence{
		{Command: &mkdirCommand{path: folder}},
		{Command: &putFileCommand{path: path, data: rom}},
	}
	return
}

func (q *ROMControlQueue) MakeBootROMCommands(path string) snes.CommandSequence {
	return snes.CommandSequence{{Command: &bootCommand{path: path}}}
}

type mkdirCommand struct{ path string }

func (c *mkdirCommand) Execute(queue snes.Queue, _ snes.KeepAlive) error {
	q, err := queueOf(queue)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err = q.dev.MakeDirectory(ctx, c.path); err != nil && !devices.IsFatal(err) {
		// most likely the folder already exists:
		log.Printf("%s: mkdir '%s': %v\n", q.name, c.path, err)
		return nil
	}
	return err
}

type putFileCommand struct {
	path string
	data []byte
}

func (c *putFileCommand) Execute(queue snes.Queue, keepAlive snes.KeepAlive) error {
	q, err := queueOf(queue)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), romCommandTimeout)
	defer cancel()
	_, err = q.dev.PutFile(ctx, c.path, uint32(len(c.data)), bytes.NewReader(c.data), func(sent, total uint32) {
		// keep BaseQueue from timing out the upload:
		select {
		case keepAlive <- struct{}{}:
		default:
		}
		log.Printf("%s: upload '%s': %#06x of %#06x\n", q.name, c.path, sent, total)
	})
	return err
}

type bootCommand struct{ path string }

func (c *bootCommand) Execute(queue snes.Queue, _ snes.KeepAlive) error {
	q, err := queueOf(queue)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return q.dev.BootFile(ctx, c.path)
}
