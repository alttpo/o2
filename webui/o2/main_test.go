package main

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"o2/snes"
	"o2/snes/fxpakpro"
	"sync"
)

func test() {
	queue, err := snes.Open("fxpakpro", fxpakpro.DeviceDescriptor{})
	if err != nil {
		log.Printf("%v\n", err)
		quitSystray()
		return
	}

	if rc, ok := queue.(snes.ROMControl); ok {
		rom, err := ioutil.ReadFile("lttpj.smc")
		if err != nil {
			log.Printf("%v\n", err)
			quitSystray()
			return
		}
		path, seq := rc.MakeUploadROMCommands("lttp.smc", rom)
		queue.EnqueueMulti(seq)
		seq = rc.MakeBootROMCommands(path)
		queue.EnqueueMulti(seq)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	queue.EnqueueMulti(queue.MakeReadCommands([]snes.Read{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completion: func(b snes.Response) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}, nil))
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	queue.EnqueueMulti(queue.MakeWriteCommands([]snes.Write{
		{
			Address: 0x007FEA, // NMI vector in bank 00
			Size:    2,
			Data:    []byte{0xC9, 0x80},
			Completion: func(b snes.Response) {
				fmt.Printf("write %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}, nil))
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	queue.EnqueueMulti(queue.MakeReadCommands([]snes.Read{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completion: func(b snes.Response) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}, nil))
	wg.Wait()

	queue.Enqueue(snes.CommandWithCompletion{Command: &snes.CloseCommand{}})
}
