// +build !linux

package main

import (
	"fmt"
	"github.com/getlantern/systray"
	"o2/webui/o2/icon"
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
	systray.SetTemplateIcon(icon.Data, icon.Data)
	//systray.SetTitle("O2")
	systray.SetTooltip("O2 - SNES Online 2.0")
	mOpenWeb := systray.AddMenuItem("Web UI", "Opens the web UI in the default browser")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit")

	// Menu item click handler:
	go func() {
		for {
			select {
			case <-mOpenWeb.ClickedCh:
				openWebUI()
				break
			case <-mQuit.ClickedCh:
				fmt.Println("Requesting quit")
				systray.Quit()
				break
			}
		}
	}()

	// Open web UI by default:
	openWebUI()
}
