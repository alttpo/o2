package main

import (
	"fmt"
	"log"
	"o2/snes"
)

// Must be JSON serializable
type SNESViewModel struct {
	commands map[string]CommandExecutor

	controller *Controller
	isClean    bool

	Drivers     []*DriverViewModel `json:"drivers"`
	IsConnected bool               `json:"isConnected"`
}

type DriverViewModel struct {
	namedDriver snes.NamedDriver
	devices     []snes.DeviceDescriptor

	Name string `json:"name"`

	DisplayName        string `json:"displayName"`
	DisplayDescription string `json:"displayDescription"`
	DisplayOrder       int    `json:"displayOrder"`

	Devices        []string `json:"devices"`
	SelectedDevice int      `json:"selectedDevice"`

	IsConnected bool `json:"isConnected"`
}

func NewSNESViewModel(c *Controller) *SNESViewModel {
	v := &SNESViewModel{controller: c}
	v.commands = map[string]CommandExecutor{
		"connect": &ConnectCommandExecutor{v},
	}
	return v
}

func (v *SNESViewModel) IsDirty() bool {
	return !v.isClean
}

func (v *SNESViewModel) ClearDirty() {
	v.isClean = true
}

func (v *SNESViewModel) Init() {
	dvs := snes.Drivers()
	v.Drivers = make([]*DriverViewModel, len(dvs))
	for i, dv := range dvs {
		devices, err := dv.Driver.Detect()
		if err != nil {
			log.Println(err)
		}

		dvm := &DriverViewModel{
			namedDriver: dv,
			devices:     devices,
		}
		v.Drivers[i] = dvm

		dvm.Name = dv.Name
		if descriptor, ok := dv.Driver.(snes.DriverDescriptor); ok {
			dvm.DisplayOrder = descriptor.DisplayOrder()
			dvm.DisplayName = descriptor.DisplayName()
			dvm.DisplayDescription = descriptor.DisplayDescription()
		} else {
			dvm.DisplayOrder = 0
			dvm.DisplayName = dv.Name
			dvm.DisplayDescription = dv.Name + " driver"
		}

		dvm.Devices = make([]string, len(devices))
		for j := 0; j < len(devices); j++ {
			dvm.Devices[j] = devices[j].DisplayName()
		}

		dvm.SelectedDevice = 0
		dvm.IsConnected = false
	}
}

func (v *SNESViewModel) Update() {
	v.IsConnected = v.controller.IsConnected()
	for _, d := range v.Drivers {
		d.IsConnected = v.controller.IsConnectedToDriver(d.namedDriver)
	}
	v.isClean = false
}

// Commands:
func (v *SNESViewModel) CommandExecutor(command string) (ce CommandExecutor, err error) {
	var ok bool
	ce, ok = v.commands[command]
	if !ok {
		err = fmt.Errorf("no command '%s' found", command)
	}
	return ce, err
}

type ConnectCommandArgs struct {
	Driver string `json:"driver"`
	Device int    `json:"device"`
}
type ConnectCommandExecutor struct {
	v *SNESViewModel
}

func (c *ConnectCommandExecutor) CreateArgs() CommandArgs {
	return &ConnectCommandArgs{}
}

func (c *ConnectCommandExecutor) Execute(args CommandArgs) error {
	return c.v.Connect(args.(*ConnectCommandArgs))
}

func (v *SNESViewModel) Connect(args *ConnectCommandArgs) error {
	driverName := args.Driver
	deviceIndex := args.Device

	var dvm *DriverViewModel = nil
	for _, dvm = range v.Drivers {
		if driverName != dvm.Name {
			continue
		}

		break
	}
	if dvm == nil {
		return fmt.Errorf("driver not found by name")
	}

	if deviceIndex < 0 || deviceIndex >= len(dvm.devices) {
		return fmt.Errorf("device index out of range")
	}
	dvm.SelectedDevice = deviceIndex

	v.controller.SNESConnected(snes.NamedDriverDevicePair{
		NamedDriver: dvm.namedDriver,
		Device:      dvm.devices[deviceIndex],
	})
	v.isClean = false

	return nil
}
