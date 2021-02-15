package engine

import (
	"fmt"
	"log"
	"o2/games"
	"o2/snes"
)

type ViewModel struct {
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

func NewViewModel() *ViewModel {
	c := &ViewModel{}

	// instantiate each child view model:
	c.snesViewModel = NewSNESViewModel(c)
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

// initializes all view models:
func (c *ViewModel) Init() {
	for _, model := range c.viewModels {
		if i, ok := model.(Initializable); ok {
			i.Init()
		}
	}
}

// updates all view models:
func (c *ViewModel) Update() {
	for _, model := range c.viewModels {
		if i, ok := model.(Updateable); ok {
			i.Update()
		}
	}
}

// updates all view models and notifies view:
func (c *ViewModel) UpdateAndNotifyView() {
	for view, model := range c.viewModels {
		if i, ok := model.(Updateable); ok {
			i.Update()
		}
		c.NotifyViewOf(view, model)
	}
}

// notifies the view of any updated view models:
func (c *ViewModel) NotifyView() {
	for view, model := range c.viewModels {
		c.NotifyViewOf(view, model)
	}
}

func (c *ViewModel) ForceNotifyViewOf(view string, model interface{}) {
	if c.viewNotifier == nil {
		return
	}

	dirtyable, isDirtyable := model.(Dirtyable)
	// ignore IsDirty() check

	c.viewNotifier.NotifyView(view, model)

	if isDirtyable {
		dirtyable.ClearDirty()
	}
}

func (c *ViewModel) NotifyViewOf(view string, model interface{}) {
	if c.viewNotifier == nil {
		return
	}

	dirtyable, isDirtyable := model.(Dirtyable)
	if isDirtyable && !dirtyable.IsDirty() {
		return
	}

	c.viewNotifier.NotifyView(view, model)

	if isDirtyable {
		dirtyable.ClearDirty()
	}
}

func (c *ViewModel) NotifyViewTo(viewNotifier ViewNotifier) {
	if viewNotifier == nil {
		return
	}

	// send all view models to this notifier regardless of dirty state:
	for view, model := range c.viewModels {
		viewNotifier.NotifyView(view, model)
	}
}

// Implements ViewCommandHandler
func (c *ViewModel) CommandExecutor(view, command string) (ce CommandExecutor, err error) {
	vm, ok := c.viewModels[view]
	if !ok {
		return nil, fmt.Errorf("view=%s,cmd=%s: no view model found to handle command", view, command)
	}

	commandHandler, ok := vm.(ViewModelCommandHandler)
	if !ok {
		return nil, fmt.Errorf("view=%s,cmd=%s: view model does not handle commands", view, command)
	}

	ce, err = commandHandler.CommandExecutor(command)
	if err != nil {
		err = fmt.Errorf("view=%s,cmd=%s: error from command handler: %w", view, command, err)
	}
	return
}

func (c *ViewModel) setStatus(msg string) {
	log.Printf("notify: %s\n", msg)
	c.viewModels["status"] = msg
}

func (c *ViewModel) tryCreateGame() bool {
	defer c.UpdateAndNotifyView()

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

func (c *ViewModel) IsConnected() bool {
	return c.dev != nil
}

func (c *ViewModel) IsConnectedToDriver(driver snes.NamedDriver) bool {
	if c.dev == nil {
		return false
	}

	return c.driverDevice.NamedDriver == driver
}

func (c *ViewModel) ROMSelected(rom *snes.ROM) {
	defer c.UpdateAndNotifyView()

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

func (c *ViewModel) SNESConnected(pair snes.NamedDriverDevicePair) {
	defer c.UpdateAndNotifyView()

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
		return
	}

	c.driverDevice = pair
	c.setStatus("Connected to SNES")

	c.tryCreateGame()
}

func (c *ViewModel) SNESDisconnected() {
	defer c.UpdateAndNotifyView()

	if c.dev == nil {
		c.driverDevice = snes.NamedDriverDevicePair{}
		return
	}

	if c.game != nil {
		log.Println("Stop existing game")
		c.game.Stop()
		c.game = nil
	}

	lastDev := c.driverDevice
	log.Printf("Closing %s\n", lastDev.Device.DisplayName())
	c.dev.EnqueueWithCallback(&snes.CloseCommand{}, func(err error) {
		log.Printf("Closed %s\n", lastDev.Device.DisplayName())
		c.setStatus("Disconnected from SNES")
		lastDev = snes.NamedDriverDevicePair{}
		c.UpdateAndNotifyView()
	})

	c.dev = nil
	c.driverDevice = snes.NamedDriverDevicePair{}
	c.setStatus("Disconnecting from SNES...")
}

func (c *ViewModel) loadROM() {
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

func (c *ViewModel) startGame() {
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

func (c *ViewModel) ProvideViewNotifier(viewNotifier ViewNotifier) {
	c.viewNotifier = viewNotifier
}
