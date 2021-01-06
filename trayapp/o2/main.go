package main

import (
	"encoding/hex"
	"fmt"
	"github.com/getlantern/systray"
	"log"
	"o2/snes"
	_ "o2/snes/fxpakpro"
	"sync"
)

func main() {
	systray.Run(trayStart, trayExit)
}

func trayExit() {
	fmt.Println("Finished quitting")
}

func trayStart() {
	//systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("O2")
	systray.SetTooltip("O2 - SNES Online 2.0")
	mQuitOrig := systray.AddMenuItem("Quit", "Quit")
	go func() {
		<-mQuitOrig.ClickedCh
		fmt.Println("Requesting quit")
		systray.Quit()
	}()

	conn, err := snes.Open("fxpakpro", "")
	if err != nil {
		log.Printf("%v\n", err)
		systray.Quit()
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	conn.SubmitRead([]snes.ReadRequest{
		{
			Address: 0x007FC0,
			Size:    0x40,
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("read  %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	})
	wg.Wait()

	wg = sync.WaitGroup{}
	wg.Add(1)
	conn.SubmitWrite([]snes.WriteRequest{
		{
			Address: 0x007FEA, // NMI vector in bank 00
			Size:    2,
			Data:    []byte{0xC9, 0x80},
			Completed: func(b snes.ReadOrWriteResponse) {
				fmt.Printf("write %06x %02x\n%s\n", b.Address, b.Size, hex.Dump(b.Data))
				wg.Done()
			},
		},
	})
	wg.Wait()

	conn.Close()
}
