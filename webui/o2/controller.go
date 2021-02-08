package main

import (
	"fmt"
	"log"
	"o2/games"
	"o2/snes"
)

type Controller struct {
	// state:
	driverDevice snes.NamedDriverDevicePair
	dev          snes.Conn

	rom     *snes.ROM
	nextRom *snes.ROM

	factory     games.Factory
	nextFactory games.Factory

	game games.Game

	// dependency that notifies view of updated view model:
	viewNotifier ViewNotifier

	// View Models:
	viewModels    map[string]interface{}
	snesViewModel *SNESViewModel
}

func NewController() *Controller {
	c := &Controller{}

	// instantiate each view model:
	c.snesViewModel = &SNESViewModel{controller: c}
	//c.romScreen =     &ROMScreen{controller: c}
	//c.connectScreen = &ConnectScreen{controller: c}
	//c.gameScreen =    &GameScreen{controller: c}

	// assign unique names to each view for easy binding with html/js UI:
	c.viewModels = map[string]interface{}{
		"status": "Not connected",
		"snes":   c.snesViewModel,
	}

	return c
}

// initializes controller and all view models:
func (c *Controller) Init() {
	for _, model := range c.viewModels {
		if i, ok := model.(Initializable); ok {
			i.Init()
		}
	}
}

// updates all view models:
func (c *Controller) Update() {
	for _, model := range c.viewModels {
		if i, ok := model.(Updateable); ok {
			i.Update()
		}
	}
}

// notifies the view of any updated view models:
func (c *Controller) NotifyView() {
	for view, model := range c.viewModels {
		dirtyable, isDirtyable := model.(Dirtyable)
		if isDirtyable && !dirtyable.IsDirty() {
			continue
		}

		c.viewNotifier.NotifyView(view, model)

		if isDirtyable {
			dirtyable.ClearDirty()
		}
	}
}

func (c *Controller) NotifyViewTo(viewNotifier ViewNotifier) {
	// send all view models to this notifier regardless of dirty state:
	for view, model := range c.viewModels {
		viewNotifier.NotifyView(view, model)
	}
}

// Reflects over ViewModels to find a command method to execute by name. Binds argument data by name.
// Implements ViewCommandHandler
func (c *Controller) HandleCommand(view, command string, data Object) error {
	vm, ok := c.viewModels[view]
	if !ok {
		return fmt.Errorf("no view model '%s' found", view)
	}

	commandHandler, ok := vm.(CommandHandler)
	if !ok {
		return fmt.Errorf("view model '%s' does not handle commands", view)
	}

	return commandHandler.HandleCommand(command, data)
}

func (c *Controller) setStatus(msg string) {
	log.Printf("notify: %s\n", msg)
	c.viewModels["status"] = msg
	c.NotifyView()
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

	defer c.Update()

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

func (c *Controller) ROMSelected(rom *snes.ROM) {
	defer c.NotifyView()

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
		c.setStatus("ROM is not compatible with any game providers")
		return
	}
	if !oneGame {
		// more than one game type matches ROM
		c.setStatus("ROM matches more than one game provider")
		c.nextFactory = nil
		return
	}

	c.nextRom = rom
	c.tryCreateGame()
}

func (c *Controller) SNESConnected(pair snes.NamedDriverDevicePair) {
	if pair == c.driverDevice && c.dev != nil {
		// no change
		return
	}

	log.Println(pair.Device.DisplayName())

	var err error
	c.dev, err = pair.NamedDriver.Driver.Open(pair.Device)
	if err != nil {
		log.Println(err)
		c.setStatus("Could not connect to the SNES")
		c.dev = nil
		c.driverDevice = snes.NamedDriverDevicePair{}
		c.Update()
		c.NotifyView()
		return
	}

	c.driverDevice = pair
	c.tryCreateGame()
}

func (c *Controller) SNESDisconnected() {
	if c.dev == nil {
		c.driverDevice = snes.NamedDriverDevicePair{}
		c.NotifyView()
		return
	}

	if c.game != nil {
		log.Println("Stop existing game")
		c.game.Stop()
		c.game = nil
	}

	c.dev.EnqueueWithCallback(&snes.CloseCommand{}, func(err error) {
		c.NotifyView()
	})

	c.dev = nil
	c.driverDevice = snes.NamedDriverDevicePair{}
}

func (c *Controller) loadROM() {
	defer c.NotifyView()

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
	defer c.NotifyView()

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

func (c *Controller) ProvideViewNotifier(viewNotifier ViewNotifier) {
	c.viewNotifier = viewNotifier
}
