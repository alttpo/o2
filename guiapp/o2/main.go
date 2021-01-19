package main

import (
	"fmt"
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/container"
	"fyne.io/fyne/widget"
	"o2/snes"
	_ "o2/snes/fxpakpro"
	"time"
)

var (
	a fyne.App
	w fyne.Window

	romC            = make(chan *snes.ROM)
	snesC           = make(chan snes.DeviceDescriptor)
	rom   *snes.ROM = nil
)

func main() {
	a = app.NewWithID("o2")

	go appMain()

	w = a.NewWindow("O2")
	setContent(w)
	w.SetMaster()
	w.ShowAndRun()
}

func setContent(w fyne.Window) {
	menu := widget.NewList(
		func() int {
			return len(Screens)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Item")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(Screens[id].Title())
		},
	)
	lastScreen := Screen(nil)

	card := widget.NewCard("", "", nil)
	menu.OnSelected = func(id widget.ListItemID) {
		if lastScreen != nil {
			// Destroy last screen:
			if sd, ok := lastScreen.(ScreenDestroy); ok {
				sd.Destroy(card.Content)
			}
		}

		// set up new screen:
		screen := Screens[id]
		lastScreen = screen
		card.SetTitle(screen.Title())
		card.SetSubTitle(screen.Description())
		v := screen.View(w)
		card.Content = v
		card.Refresh()
	}

	split := container.NewHSplit(
		menu,
		card)
	split.Offset = 0.2

	w.SetContent(split)

	menu.Select(0)
	w.Resize(fyne.NewSize(1024, 800))
}

func appMain() {
	ticker := time.NewTicker(100 * time.Millisecond)

	for {
		select {
		case rom = <-romC:

			break
		case dev := <-snesC:
			fmt.Println(dev.DisplayName())
			break
		case <-ticker.C:
			break
		}
	}
}
