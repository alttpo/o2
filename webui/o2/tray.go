// +build !linux

package main

import (
	"fmt"
	"github.com/getlantern/systray"
	"github.com/skratchdot/open-golang/open"
	"log"
)

func createSystray() {
	// Start up a systray:
	systray.Run(trayStart, trayExit)
}

func quitSystray() {
	systray.Quit()
}

func trayExit() {
	fmt.Println("Finished quitting")
}

func trayStart() {
	// Set up the systray:
	//systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("O2")
	systray.SetTooltip("O2 - SNES Online 2.0")
	mOpenWeb := systray.AddMenuItem("Web UI", "Opens the web UI in the default browser")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit")

	// Menu item click handler:
	go func() {
		for {
			select {
			case <-mOpenWeb.ClickedCh:
				err := open.Start(fmt.Sprintf("http://127.0.0.1:%d/", listenPort))
				if err != nil {
					log.Println(err)
				}
				break
			case <-mQuit.ClickedCh:
				fmt.Println("Requesting quit")
				systray.Quit()
				break
			}
		}
	}()
}
