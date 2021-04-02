package engine

import (
	"fmt"
	"log"
	"o2/client"
	"o2/games"
	"o2/interfaces"
	"o2/snes"
)

type ViewModel struct {
	// state:
	driverDevice snes.NamedDriverDevicePair
	dev          snes.Queue

	rom     *snes.ROM
	nextRom *snes.ROM

	factory     games.Factory
	nextFactory games.Factory

	game   games.Game
	client *client.Client

	// dependency that notifies view of updated view model:
	viewNotifier interfaces.ViewNotifier

	// View Models:
	viewModels      map[string]interface{}
	snesViewModel   *SNESViewModel
	romViewModel    *ROMViewModel
	serverViewModel *ServerViewModel
	gameViewModel   *GameViewModel
}

func NewViewModel() *ViewModel {
	vm := &ViewModel{
		client: client.NewClient(),
	}

	// instantiate each child view model:
	vm.snesViewModel = NewSNESViewModel(vm)
	vm.romViewModel = NewROMViewModel(vm)
	vm.serverViewModel = NewServerViewModel(vm)
	vm.gameViewModel = NewGameViewModel(vm)

	// assign unique names to each view for easy binding with html/js UI:
	vm.viewModels = map[string]interface{}{
		"status": "Not connected",
		"snes":   vm.snesViewModel,
		"rom":    vm.romViewModel,
		"server": vm.serverViewModel,
		"game":   vm.gameViewModel,
	}

	return vm
}

// initializes all view models:
func (vm *ViewModel) Init() {
	for _, model := range vm.viewModels {
		if i, ok := model.(interfaces.Initializable); ok {
			i.Init()
		}
	}
}

// updates all view models:
func (vm *ViewModel) Update() {
	for _, model := range vm.viewModels {
		if i, ok := model.(interfaces.Updateable); ok {
			i.Update()
		}
	}
}

// updates all view models and notifies view:
func (vm *ViewModel) UpdateAndNotifyView() {
	for view, model := range vm.viewModels {
		if i, ok := model.(interfaces.Updateable); ok {
			i.Update()
		}
		vm.NotifyViewOf(view, model)
	}
}

// notifies the view of any updated view models:
func (vm *ViewModel) NotifyView() {
	for view, model := range vm.viewModels {
		vm.NotifyViewOf(view, model)
	}
}

func notifyView(viewNotifier interfaces.ViewNotifier, view string, model interface{}) {
	viewModel := model
	if viewModeler, ok := model.(interfaces.ViewModeler); ok {
		viewModel = viewModeler.ViewModel()
	}
	viewNotifier.NotifyView(view, viewModel)
}

func (vm *ViewModel) ForceNotifyViewOf(view string, model interface{}) {
	if vm.viewNotifier == nil {
		return
	}

	dirtyable, isDirtyable := model.(interfaces.Dirtyable)
	// ignore IsDirty() check

	notifyView(vm.viewNotifier, view, model)

	if isDirtyable {
		dirtyable.ClearDirty()
	}
}

func (vm *ViewModel) NotifyViewOf(view string, model interface{}) {
	if vm.viewNotifier == nil {
		return
	}

	dirtyable, isDirtyable := model.(interfaces.Dirtyable)
	if isDirtyable && !dirtyable.IsDirty() {
		return
	}

	notifyView(vm.viewNotifier, view, model)

	if isDirtyable {
		dirtyable.ClearDirty()
	}
}

func (vm *ViewModel) NotifyViewTo(viewNotifier interfaces.ViewNotifier) {
	if viewNotifier == nil {
		return
	}

	// send all view models to this notifier regardless of dirty state:
	for view, model := range vm.viewModels {
		notifyView(viewNotifier, view, model)
	}
}

// Implements ViewCommandHandler
func (vm *ViewModel) CommandFor(view, command string) (ce interfaces.Command, err error) {
	svm, ok := vm.viewModels[view]
	if !ok {
		return nil, fmt.Errorf("view=%s,cmd=%s: no view model found to handle command", view, command)
	}

	commandHandler, ok := svm.(interfaces.ViewModelCommandHandler)
	if !ok {
		return nil, fmt.Errorf("view=%s,cmd=%s: view model does not handle commands", view, command)
	}

	ce, err = commandHandler.CommandFor(command)
	if err != nil {
		err = fmt.Errorf("view=%s,cmd=%s: error from command handler: %w", view, command, err)
	}
	return
}

func (vm *ViewModel) setStatus(msg string) {
	log.Printf("notify: %s\n", msg)
	vm.viewModels["status"] = msg
}

func (vm *ViewModel) tryCreateGame() bool {
	defer vm.UpdateAndNotifyView()

	if vm.dev == nil {
		log.Println("dev is nil")
		return false
	}
	if vm.nextRom == nil {
		log.Println("rom is nil")
		return false
	}
	if vm.game != nil {
		log.Println("game already created")
		return false
	}

	var err error
	log.Println("Create new game")
	vm.game, err = vm.nextFactory.NewGame(
		vm.dev,
		vm.nextRom,
		vm.client,
	)
	if err != nil {
		return false
	}

	vm.rom = vm.nextRom
	vm.factory = vm.nextFactory

	vm.gameViewModel.GameCreated()

	// Load the ROM:
	vm.game.Load()

	// start the game instance:
	log.Println("Start game")
	vm.game.Start()

	return true
}

func (vm *ViewModel) IsConnected() bool {
	return vm.dev != nil
}

func (vm *ViewModel) IsConnectedToDriver(driver snes.NamedDriver) bool {
	if vm.dev == nil {
		return false
	}

	return vm.driverDevice.NamedDriver == driver
}

func (vm *ViewModel) ROMSelected(rom *snes.ROM) error {
	defer vm.UpdateAndNotifyView()

	// the user has selected a ROM file:
	log.Printf(`ROM selected
title:   '%s'
region:  %s (code %02X)
version: 1.%d
maker:   %02x
game:    %04x
`,
		string(rom.Header.Title[:]),
		regions[rom.Header.DestinationCode],
		rom.Header.DestinationCode,
		rom.Header.MaskROMVersion,
		rom.Header.MakerCode,
		rom.Header.GameCode)

	// determine if ROM is recognizable as a game we provide support for:
	vm.nextFactory = nil

	allFactories := games.Factories()
	factories := make([]games.Factory, 0, len(allFactories))
	for _, f := range allFactories {
		if !f.IsROMSupported(rom) {
			continue
		}
		factories = append(factories, f)
		break
	}

	if len(factories) == 0 {
		// unrecognized ROM
		vm.setStatus("ROM is not compatible with any game providers")
		return nil
	} else if len(factories) > 1 {
		// more than one game type matches ROM
		// TODO: could loop through factories and filter by CanPlay
		vm.setStatus("ROM matches more than one game provider")
		vm.nextFactory = nil
		return nil
	}

	vm.nextFactory = factories[0]

	// check if the ROM is supported:
	ok, reason := vm.nextFactory.CanPlay(rom)
	if !ok {
		vm.setStatus(fmt.Sprintf("ROM not supported: %s", reason))
		return nil
	}

	// attempt to patch the ROM file:
	patcher := vm.nextFactory.Patcher(rom)
	if err := patcher.Patch(); err != nil {
		err = fmt.Errorf("error patching ROM: %w", err)
		log.Println(err)
		vm.setStatus(err.Error())
		return nil
	}

	vm.nextRom = rom
	vm.tryCreateGame()

	return nil
}

func (vm *ViewModel) SNESConnected(pair snes.NamedDriverDevicePair) {
	defer vm.UpdateAndNotifyView()

	if pair == vm.driverDevice && vm.dev != nil {
		// no change
		return
	}

	log.Println(pair.Device.DisplayName())

	var err error
	vm.dev, err = pair.NamedDriver.Driver.Open(pair.Device)
	if err != nil {
		log.Println(err)
		vm.setStatus("Could not connect to the SNES")
		vm.dev = nil
		vm.driverDevice = snes.NamedDriverDevicePair{}
		return
	}

	vm.driverDevice = pair
	vm.setStatus("Connected to SNES")

	vm.tryCreateGame()
}

func (vm *ViewModel) SNESDisconnected() {
	defer vm.UpdateAndNotifyView()

	if vm.dev == nil {
		vm.driverDevice = snes.NamedDriverDevicePair{}
		return
	}

	if vm.game != nil {
		log.Println("Stop existing game")
		vm.game.Stop()
		vm.game = nil
	}

	// enqueue the close operation:
	snesClosed := make(chan error)
	lastDev := vm.driverDevice
	log.Printf("Closing %s\n", lastDev.Device.DisplayName())
	vm.dev.EnqueueWithCompletion(&snes.CloseCommand{}, snesClosed)

	vm.dev = nil
	vm.driverDevice = snes.NamedDriverDevicePair{}
	vm.setStatus("Disconnecting from SNES...")
	vm.UpdateAndNotifyView()

	// wait until snes is closed:
	err := <-snesClosed
	if err != nil {
		log.Println(err)
	}
	log.Printf("Closed %s\n", lastDev.Device.DisplayName())
	vm.setStatus("Disconnected from SNES")
	lastDev = snes.NamedDriverDevicePair{}
	vm.UpdateAndNotifyView()
}

func (vm *ViewModel) ProvideViewNotifier(viewNotifier interfaces.ViewNotifier) {
	vm.viewNotifier = viewNotifier
}

func (vm *ViewModel) ConnectServer() {
	defer vm.serverViewModel.MarkDirty()

	err := vm.client.Connect(vm.serverViewModel.HostName, vm.serverViewModel.GroupName)
	vm.serverViewModel.IsConnected = vm.client.IsConnected()
	if err != nil {
		log.Print(err)
		return
	}
}

func (vm *ViewModel) DisconnectServer() {
	defer vm.serverViewModel.MarkDirty()

	vm.client.Disconnect()
	vm.serverViewModel.IsConnected = vm.client.IsConnected()
}
