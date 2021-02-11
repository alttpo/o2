package main

import (
	"fmt"
	"log"
	"o2/snes"
	"time"
)

// Must be JSON serializable
type SNESViewModel struct {
	commands map[string]CommandExecutor

	c       *Controller
	isClean bool

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
	v := &SNESViewModel{c: c}

	// supported commands:
	v.commands = map[string]CommandExecutor{
		"connect":    &ConnectCommandExecutor{v},
		"disconnect": &DisconnectCommandExecutor{v},
	}

	return v
}

func (v *SNESViewModel) IsDirty() bool {
	return !v.isClean
}

func (v *SNESViewModel) ClearDirty() {
	v.isClean = true
}

func (v *SNESViewModel) MarkDirty() {
	v.isClean = false
	v.c.NotifyViewOf("snes", v)
}

func (v *SNESViewModel) Init() {
	dvs := snes.Drivers()
	v.Drivers = make([]*DriverViewModel, len(dvs))
	for i, dv := range dvs {
		devices, err := dv.Driver.Detect()
		if err != nil {
			log.Println(err)
			devices = make([]snes.DeviceDescriptor, 0)
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

	// background goroutine to auto-detect new devices every 5 seconds:
	go func() {
		for range time.NewTicker(time.Second * 5).C {
			needUpdate := false

			for _, dvm := range v.Drivers {
				devices, err := dvm.namedDriver.Driver.Detect()
				if err != nil {
					log.Println(err)
					devices = make([]snes.DeviceDescriptor, 0)
				}

				replace := false
				if len(dvm.devices) != len(devices) {
					replace = true
				} else {
					// check if all devices are equivalent:
					for i := 0; i < len(devices); i++ {
						if !devices[i].Equals(dvm.devices[i]) {
							replace = true
							break
						}
					}
				}

				if !replace {
					continue
				}

				// swap out the array and recreate the view models:
				dvm.devices = devices
				dvm.Devices = make([]string, len(devices))
				for j := 0; j < len(devices); j++ {
					dvm.Devices[j] = devices[j].DisplayName()
				}
				needUpdate = true
			}

			if needUpdate {
				v.Update()
				v.MarkDirty()
			}
		}
	}()
}

func (v *SNESViewModel) Update() {
	v.IsConnected = v.c.IsConnected()
	for _, dvm := range v.Drivers {
		dvm.IsConnected = v.c.IsConnectedToDriver(dvm.namedDriver)
		if !dvm.IsConnected {
			dvm.SelectedDevice = 0
		}
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
	deviceIndex := args.Device - 1

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
	dvm.SelectedDevice = deviceIndex + 1

	v.c.SNESConnected(snes.NamedDriverDevicePair{
		NamedDriver: dvm.namedDriver,
		Device:      dvm.devices[deviceIndex],
	})

	return nil
}

type NullArgs struct{}
type DisconnectCommandExecutor struct {
	v *SNESViewModel
}

func (c *DisconnectCommandExecutor) CreateArgs() CommandArgs {
	return &NullArgs{}
}
func (c *DisconnectCommandExecutor) Execute(args CommandArgs) error {
	return c.v.Disconnect(args.(*NullArgs))
}

func (v *SNESViewModel) Disconnect(args *NullArgs) error {
	v.c.SNESDisconnected()

	return nil
}
