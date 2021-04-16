package engine

import (
	"fmt"
	"log"
	"o2/client"
	"o2/games"
	"o2/interfaces"
	"o2/snes"
	"sync"
)

type ViewModel struct {
	// state:
	driverDevice snes.NamedDriverDevicePair
	dev          snes.Queue
	devLock      sync.Mutex

	rom     *snes.ROM
	nextRom *snes.ROM

	factory     games.Factory
	nextFactory games.Factory

	game   games.Game
	client *client.Client

	// dependency that notifies view of updated view model:
	viewNotifier interfaces.ViewNotifier

	// View Models:
	viewModels     map[string]interface{}
	viewModelsLock sync.Mutex

	snesViewModel   *SNESViewModel
	romViewModel    *ROMViewModel
	serverViewModel *ServerViewModel
}

func NewViewModel() *ViewModel {
	vm := &ViewModel{
		client: client.NewClient(),
	}

	// instantiate each child view model:
	vm.snesViewModel = NewSNESViewModel(vm)
	vm.romViewModel = NewROMViewModel(vm)
	vm.serverViewModel = NewServerViewModel(vm)

	// assign unique names to each view for easy binding with html/js UI:
	vm.viewModels = map[string]interface{}{
		"status": "Not connected",
		"snes":   vm.snesViewModel,
		"rom":    vm.romViewModel,
		"server": vm.serverViewModel,
	}

	return vm
}

func (vm *ViewModel) GetViewModel(view string) (interface{}, bool) {
	defer vm.viewModelsLock.Unlock()
	vm.viewModelsLock.Lock()

	viewModel, ok := vm.viewModels[view]
	return viewModel, ok
}

func (vm *ViewModel) NotifyView(view string, model interface{}) {
	defer vm.viewModelsLock.Unlock()
	vm.viewModelsLock.Lock()

	// allow model to customize the instance to be stored as a view model:
	viewModel := model
	if viewModeler, ok := model.(interfaces.ViewModeler); ok {
		viewModel = viewModeler.ViewModel()
	}

	// cache the viewModel for new websocket connections so they get the updates on first connect:
	vm.viewModels[view] = viewModel

	// notify downstream if applicable:
	vn := vm.viewNotifier
	if vn == nil {
		return
	}
	vn.NotifyView(view, viewModel)
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

func (vm *ViewModel) NotifyViewTo(viewNotifier interfaces.ViewNotifier) {
	if viewNotifier == nil {
		return
	}

	// send all view models to this notifier regardless of dirty state:
	for view, model := range vm.viewModels {
		viewNotifier.NotifyView(view, model)
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

func (vm *ViewModel) NotifyViewOf(view string, model interface{}) {
	dirtyable, isDirtyable := model.(interfaces.Dirtyable)
	if isDirtyable && !dirtyable.IsDirty() {
		return
	}

	vm.NotifyView(view, model)

	if isDirtyable {
		dirtyable.ClearDirty()
	}
}

// Implements ViewCommandHandler
func (vm *ViewModel) CommandFor(view, command string) (ce interfaces.Command, err error) {
	var svm interface{}
	var ok bool

	svm, ok = vm.viewModels[view]
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

	if vm.nextRom == nil {
		log.Println("rom is nil")
		return false
	}
	if vm.game != nil {
		log.Println("stop game")
		vm.game.Stop()
		vm.game = nil
	}

	vm.rom = vm.nextRom
	vm.factory = vm.nextFactory

	log.Println("create new game")
	vm.game = vm.factory.NewGame(vm.rom)

	// provide the game with its deps:
	vm.game.ProvideQueue(vm.dev)
	vm.game.ProvideClient(vm.client)

	// intercept root.viewNotifier to let us cache viewModel updates from the game:
	// game will notify us of its viewModel on Start()/Reset():
	vm.game.ProvideViewModelContainer(vm)

	go func() {
		// wait until the game is stopped:
		<-vm.game.Stopped()
		vm.game = nil
		delete(vm.viewModels, "game")
		vm.UpdateAndNotifyView()
	}()

	// start the game instance:
	log.Println("start game")
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
`,
		string(rom.Header.Title[:]),
		snes.RegionNames[rom.Header.DestinationCode],
		rom.Header.DestinationCode,
		rom.Header.MaskROMVersion)

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

	if vm.game != nil {
		// inform the game of the new device:
		vm.game.ProvideQueue(vm.dev)
	}

	go func() {
		// wait for the SNES to be closed:
		<-vm.dev.Closed()
		vm.SNESDisconnected()
	}()

	vm.driverDevice = pair
	vm.setStatus("Connected to SNES")
}

func (vm *ViewModel) SNESDisconnected() {
	defer vm.devLock.Unlock()
	vm.devLock.Lock()

	if vm.dev == nil {
		vm.driverDevice = snes.NamedDriverDevicePair{}
		return
	}

	defer vm.UpdateAndNotifyView()

	// enqueue the close operation:
	snesClosed := make(chan error)
	lastDev := vm.driverDevice
	log.Printf("Closing %s\n", lastDev.Device.DisplayName())
	err := vm.dev.Enqueue(snes.CommandWithCompletion{
		Command: &snes.CloseCommand{},
		Completion: func(cmd snes.Command, err error) {
			snesClosed <- err
			close(snesClosed)
		},
	})

	vm.dev = nil
	if vm.game != nil {
		vm.game.ProvideQueue(nil)
	}
	vm.driverDevice = snes.NamedDriverDevicePair{}
	vm.setStatus("Disconnecting from SNES...")
	vm.UpdateAndNotifyView()

	if err != nil {
		log.Println(err)
		return
	}

	// wait until snes is closed:
	err = <-snesClosed
	if err != nil {
		log.Println(err)
	}
	log.Printf("Closed %s\n", lastDev.Device.DisplayName())

	lastDev = snes.NamedDriverDevicePair{}
	vm.setStatus("Disconnected from SNES")

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
