package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/container"
	"fyne.io/fyne/widget"
	"log"
	"o2/game"
	_ "o2/game/alttp"
	"o2/snes"
	_ "o2/snes/fxpakpro"
	"time"
)

var (
	a fyne.App
	w fyne.Window

	romC  = make(chan *snes.ROM)
	snesC = make(chan snes.DriverDevicePair)
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)

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

func notify(content string) {
	a.SendNotification(&fyne.Notification{
		Title:   "O2",
		Content: content,
	})
}

func appMain() {
	var rom *snes.ROM = nil
	var dev snes.Conn = nil
	var factory game.Factory = nil
	var inst game.Game = nil
	ticker := time.NewTicker(100 * time.Millisecond)

	tryCreateGame := func() {
		if dev == nil {
			return
		}
		if rom == nil {
			return
		}

		var err error
		if inst != nil {
			inst.Stop()
			inst = nil
		}

		inst, err = factory.NewGame(rom, dev)
		if err != nil {
			return
		}

		// start the game instance:
		inst.Start()
	}

	for {
		select {
		case newrom := <-romC:
			// the user has selected a ROM file:
			log.Printf("title:   '%s'\n", string(newrom.Header.Title[:]))
			log.Printf("region:  %d\n", newrom.Header.DestinationCode)
			log.Printf("version: 1.%d\n", newrom.Header.MaskROMVersion)
			log.Printf("maker:   %02x\n", newrom.Header.MakerCode)
			log.Printf("game :   %04x\n", newrom.Header.GameCode)

			oneGame := true
			factory = nil
			for _, f := range game.Factories() {
				if !f.IsROMCompatible(newrom) {
					continue
				}
				if factory == nil {
					factory = f
				} else {
					oneGame = false
				}
				break
			}
			if factory == nil {
				// unrecognized ROM
				notify("ROM is not compatible with any game providers")
				break
			}
			if !oneGame {
				// more than one game type matches ROM
				notify("ROM matches more than one game provider")
				break
			}

			rom = newrom
			tryCreateGame()
			break
		case pair := <-snesC:
			log.Println(pair.Device.DisplayName())

			var err error
			dev, err = pair.Driver.Open(pair.Device)
			if err != nil {
				log.Println(err)
				notify("Could not connect to the SNES")
				break
			}

			tryCreateGame()
			break
		case <-ticker.C:
			break
		}
	}
}
