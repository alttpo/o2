package main

import (
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
	conn.Close()
}
