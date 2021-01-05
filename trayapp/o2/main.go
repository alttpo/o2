package main

import (
	"encoding/hex"
	"fmt"
	"github.com/getlantern/systray"
	"log"
	"o2/snes"
	_ "o2/snes/fxpakpro"
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

	respC := make(chan snes.ReadOrWriteResponse, 64)
	requests := []snes.ReadRequest{
		{
			Address: 0xF50010,
			Size:    0xF0,
			ReplyTo: respC,
		},
		{
			Address: 0x007FC0,
			Size:    0x40,
			ReplyTo: respC,
		},
		{
			Address: 0xF50010,
			Size:    0xF0,
			ReplyTo: respC,
		},
		{
			Address: 0x00FFC0,
			Size:    0x40,
			ReplyTo: respC,
		},
		{
			Address: 0xF50010,
			Size:    0xF0,
			ReplyTo: respC,
		},
		{
			Address: 0x407FC0,
			Size:    0x40,
			ReplyTo: respC,
		},
		{
			Address: 0xF50010,
			Size:    0xF0,
			ReplyTo: respC,
		},
		{
			Address: 0x40FFC0,
			Size:    0x40,
			ReplyTo: respC,
		},
		{
			Address: 0xF50010,
			Size:    0xF0,
			ReplyTo: respC,
		},
		{
			Address: 0x807FC0,
			Size:    0x40,
			ReplyTo: respC,
		},
		{
			Address: 0xF50010,
			Size:    0xF0,
			ReplyTo: respC,
		},
		{
			Address: 0x80FFC0,
			Size:    0x40,
			ReplyTo: respC,
		},
	}
	conn.SubmitRead(requests)
	conn.SubmitWrite([]snes.WriteRequest{
		{
			Address: 0x007FEA, // NMI vector in bank 00
			Size:    2,
			Data:    []byte{0x12, 0x80},
			ReplyTo: respC,
		},
	})
	//conn.SubmitRead(requests)

	for i := 0; ; i++ {
		b := <-respC
		fmt.Printf("%3d: %5v %s\n", i, b.IsWrite, hex.EncodeToString(b.Data))
	}

	conn.Close()
}
