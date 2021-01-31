package main

import (
	"fyne.io/fyne"
	"fyne.io/fyne/container"
	"fyne.io/fyne/widget"
	"log"
	"o2/games"
	"o2/snes"
)

type Controller struct {
	driverDevice snes.NamedDriverDevicePair
	dev          snes.Conn

	rom     *snes.ROM
	nextRom *snes.ROM

	factory     games.Factory
	nextFactory games.Factory

	game games.Game

	// main app window
	w             fyne.Window
	snesScreen    *SNESScreen
	romScreen     *ROMScreen
	connectScreen *ConnectScreen
	gameScreen    *GameScreen
	screens       []Screen
}

func NewController() *Controller {
	c := &Controller{
		snesScreen:    &SNESScreen{},
		romScreen:     &ROMScreen{},
		connectScreen: &ConnectScreen{},
		gameScreen:    &GameScreen{},
	}

	c.screens = []Screen{
		c.snesScreen,
		c.romScreen,
		c.connectScreen,
		c.gameScreen,
	}

	return c
}

func (c *Controller) createWindow() {
	c.w = a.NewWindow("O2")

	menu := widget.NewList(
		func() int {
			return len(c.screens)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Item")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(c.screens[id].Title())
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
		screen := c.screens[id]
		lastScreen = screen
		card.SetTitle(screen.Title())
		card.SetSubTitle(screen.Description())
		v := screen.View(c.w)
		card.Content = v
		card.Refresh()
	}

	split := container.NewHSplit(
		menu,
		card)
	split.Offset = 0.2

	c.w.SetContent(split)

	menu.Select(0)
	c.w.Resize(fyne.NewSize(1024, 800))

	c.w.SetMaster()
}

func (c *Controller) showAndRun() {
	// This blocks the main goroutine:
	c.w.ShowAndRun()
}

func (c *Controller) tryCreateGame() bool {
	if c.dev == nil {
		log.Println("dev is nil")
		return false
	}
	if c.nextRom == nil {
		log.Println("rom is nil")
		return false
	}
	if c.game != nil {
		log.Println("game already created")
		return false
	}

	var err error
	log.Println("Create new game")
	c.game, err = c.nextFactory.NewGame(c.nextRom, c.dev)
	if err != nil {
		return false
	}

	c.rom = c.nextRom
	c.factory = c.nextFactory
	return true
}

func (c *Controller) IsConnected() bool {
	return c.dev != nil
}

func (c *Controller) IsConnectedToDriver(driver snes.NamedDriver) bool {
	if c.dev == nil {
		return false
	}

	return c.driverDevice.NamedDriver == driver
}

func (c *Controller) Refresh() {
	c.snesScreen.Refresh()
	c.gameScreen.Refresh()
	//c.romScreen.Refresh()
}

func (c *Controller) ROMSelected(rom *snes.ROM) {
	defer c.Refresh()

	// the user has selected a ROM file:
	log.Printf(`ROM selected
title:   '%s'
region:  %d
version: 1.%d
maker:   %02x
game:    %04x
`,
		string(rom.Header.Title[:]),
		rom.Header.DestinationCode,
		rom.Header.MaskROMVersion,
		rom.Header.MakerCode,
		rom.Header.GameCode)

	// determine if ROM is recognizable as a game we provide support for:
	oneGame := true
	c.nextFactory = nil
	for _, f := range games.Factories() {
		if !f.IsROMCompatible(rom) {
			continue
		}
		if c.nextFactory == nil {
			c.nextFactory = f
		} else {
			oneGame = false
		}
		break
	}

	if c.nextFactory == nil {
		// unrecognized ROM
		notify("ROM is not compatible with any game providers")
		return
	}
	if !oneGame {
		// more than one game type matches ROM
		notify("ROM matches more than one game provider")
		c.nextFactory = nil
		return
	}

	c.nextRom = rom
	c.tryCreateGame()
}

func (c *Controller) SNESConnected(pair snes.NamedDriverDevicePair) {
	defer c.Refresh()

	if pair == c.driverDevice && c.dev != nil {
		return
	}

	log.Println(pair.Device.DisplayName())

	var err error
	c.dev, err = pair.NamedDriver.Driver.Open(pair.Device)
	if err != nil {
		log.Println(err)
		notify("Could not connect to the SNES")
		c.dev = nil
		c.driverDevice = snes.NamedDriverDevicePair{}
		return
	}

	c.driverDevice = pair
	c.tryCreateGame()
}

func (c *Controller) SNESDisconnected() {
	if c.dev == nil {
		c.driverDevice = snes.NamedDriverDevicePair{}
		c.Refresh()
		return
	}

	if c.game != nil {
		log.Println("Stop existing game")
		c.game.Stop()
		c.game = nil
	}

	c.dev.EnqueueWithCallback(&snes.CloseCommand{}, func(err error) {
		c.Refresh()
	})

	c.dev = nil
	c.driverDevice = snes.NamedDriverDevicePair{}
}

func (c *Controller) loadROM() {
	defer c.Refresh()

	if !c.tryCreateGame() {
		return
	}

	// Load the ROM:
	c.game.Load()

	// start the game instance:
	log.Println("Start game")
	c.game.Start()

}

func (c *Controller) startGame() {
	defer c.Refresh()

	if c.game == nil {
		return
	}
	if c.game.IsRunning() {
		return
	}

	// start the game instance:
	log.Println("Start game")
	c.game.Start()
}
