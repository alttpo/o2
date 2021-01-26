package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"log"
	_ "o2/game/alttp"
	_ "o2/snes/fxpakpro"
	_ "o2/snes/mock"
)

var (
	a          fyne.App
	controller *Controller
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)

	a = app.NewWithID("o2")

	controller = NewController()
	controller.createWindow()
	controller.showAndRun()
}

func notify(content string) {
	a.SendNotification(&fyne.Notification{
		Title:   "O2",
		Content: content,
	})
}
