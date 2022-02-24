package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"o2/interfaces"
	"o2/snes"
	"time"
)

// Must be JSON serializable
type SNESViewModel struct {
	commands map[string]interfaces.Command

	c       *ViewModel
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

	Devices        []snes.DeviceDescriptor `json:"devices"`
	SelectedDevice string                  `json:"selectedDevice"`

	IsConnected bool `json:"isConnected"`
}

type SNESConfiguration struct {
	Driver string `json:"driver"`
	Device string `json:"device"`
}

func (v *SNESViewModel) LoadConfiguration(config *SNESConfiguration) {
	if config == nil {
		log.Printf("snesviewmodel: loadConfiguration: no config\n")
		return
	}

	// Init() has already been called
	dvm := v.FindNamedDriver(config.Driver)
	if dvm == nil {
		log.Printf("snesviewmodel: loadConfiguration: driver '%s' not found\n", config.Driver)
		return
	}

	var device snes.DeviceDescriptor
	for _, dv := range dvm.Devices {
		if dv.GetId() == config.Device {
			device = dv
			break
		}
	}
	if device == nil {
		log.Printf("snesviewmodel: loadConfiguration: driver '%s' device '%s' not found\n", config.Driver, config.Device)
		return
	}

	// connect to driver and device:
	dvm.SelectedDevice = device.GetId()
	v.c.SNESConnected(snes.NamedDriverDevicePair{
		NamedDriver: dvm.namedDriver,
		Device:      device,
	})
}

func (v *SNESViewModel) SaveConfiguration(config *SNESConfiguration) {
	if config == nil {
		log.Printf("snesviewmodel: saveConfiguration: no config\n")
		return
	}

	var drv *DriverViewModel = nil
	for _, d := range v.Drivers {
		if d.IsConnected {
			drv = d
			break
		}
	}

	if drv == nil {
		config.Driver = ""
		config.Device = ""
		return
	}

	config.Driver = drv.Name
	config.Device = drv.SelectedDevice
}

func NewSNESViewModel(c *ViewModel) *SNESViewModel {
	v := &SNESViewModel{c: c}

	// supported commands:
	v.commands = map[string]interfaces.Command{
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
			log.Printf("snesviewmodel: detect[%s]: %v\n", dv.Name, err)
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

		dvm.Devices = make([]snes.DeviceDescriptor, len(devices))
		for i, dv := range devices {
			dvm.Devices[i] = snes.MarshalDeviceDescriptor(dv)
		}

		dvm.SelectedDevice = ""
		dvm.IsConnected = false
	}

	// background goroutine to auto-detect new devices every 2 seconds:
	go func() {
		for range time.NewTicker(time.Second * 2).C {
			// don't need to auto-detect while already connected:
			if v.IsConnected {
				continue
			}

			needUpdate := false

			for _, dvm := range v.Drivers {
				devices, err := dvm.namedDriver.Driver.Detect()
				if err != nil {
					log.Printf("snesviewmodel: detect[%s]: %v\n", dvm.namedDriver.Name, err)
					devices = make([]snes.DeviceDescriptor, 0)
				}

				replace := false
				if len(dvm.devices) != len(devices) {
					replace = true
				} else {
					// check if all devices are equivalent:
					for i := 0; i < len(devices); i++ {
						if devices[i].GetId() != dvm.devices[i].GetId() {
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
				dvm.Devices = make([]snes.DeviceDescriptor, len(devices))
				for i, dv := range devices {
					dvm.Devices[i] = snes.MarshalDeviceDescriptor(dv)
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
			dvm.SelectedDevice = ""
		}
	}

	v.isClean = false
}

// Commands:
func (v *SNESViewModel) CommandFor(command string) (ce interfaces.Command, err error) {
	var ok bool
	ce, ok = v.commands[command]
	if !ok {
		err = fmt.Errorf("no command '%s' found", command)
	}
	return
}

type ConnectCommandExecutor struct{ v *SNESViewModel }
type ConnectCommandArgs struct {
	Driver string          `json:"driver"`
	Device json.RawMessage `json:"device"`
}

func (c *ConnectCommandExecutor) CreateArgs() interfaces.CommandArgs { return &ConnectCommandArgs{} }
func (c *ConnectCommandExecutor) Execute(args interfaces.CommandArgs) error {
	return c.v.Connect(args.(*ConnectCommandArgs))
}

func (v *SNESViewModel) Connect(args *ConnectCommandArgs) error {
	driverName := args.Driver

	dvm := v.FindNamedDriver(driverName)
	if dvm == nil {
		return fmt.Errorf("snes driver not found by name '%s'", driverName)
	}

	// unmarshal the json:
	device := dvm.namedDriver.Driver.Empty()
	err := json.Unmarshal(args.Device, device)
	if err != nil {
		return fmt.Errorf("snes could not unmarshal device json: %w", err)
	}

	dvm.SelectedDevice = device.GetId()

	v.c.SNESConnected(snes.NamedDriverDevicePair{
		NamedDriver: dvm.namedDriver,
		Device:      device,
	})

	return nil
}

func (v *SNESViewModel) FindNamedDriver(driverName string) *DriverViewModel {
	var dvm *DriverViewModel = nil
	for _, dvm = range v.Drivers {
		if driverName != dvm.Name {
			continue
		}

		break
	}
	return dvm
}

type DisconnectCommandExecutor struct{ v *SNESViewModel }

func (c *DisconnectCommandExecutor) CreateArgs() interfaces.CommandArgs { return nil }
func (c *DisconnectCommandExecutor) Execute(_ interfaces.CommandArgs) error {
	return c.v.Disconnect()
}

func (v *SNESViewModel) Disconnect() error {
	v.c.SNESDisconnected()

	return nil
}
