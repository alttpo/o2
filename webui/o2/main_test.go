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
	conn, err := snes.Open("fxpakpro", fxpakpro.DeviceDescriptor{})
	if err != nil {
		log.Printf("%v\n", err)
		quitSystray()
		return
	}

	if rc, ok := conn.(snes.ROMControl); ok {
		rom, err := ioutil.ReadFile("lttpj.smc")
		if err != nil {
			log.Printf("%v\n", err)
			quitSystray()
			return
		}
		path, seq := rc.MakeUploadROMCommands("lttp.smc", rom)
		conn.EnqueueMulti(seq)
		seq = rc.MakeBootROMCommands(path)
		conn.EnqueueMulti(seq)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	conn.EnqueueMulti(conn.MakeReadCommands([]snes.ReadRequest{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}))
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	conn.EnqueueMulti(conn.MakeWriteCommands([]snes.WriteRequest{
		{
			Address: 0x007FEA, // NMI vector in bank 00
			Size:    2,
			Data:    []byte{0xC9, 0x80},
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("write %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}))
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	conn.EnqueueMulti(conn.MakeReadCommands([]snes.ReadRequest{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	}))
	wg.Wait()

	conn.Enqueue(&snes.CloseCommand{})
}
