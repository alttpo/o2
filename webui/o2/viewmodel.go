package main

import (
	"fmt"
	"log"
	"o2/snes"
)

// Must be JSON serializable
type SNESViewModel struct {
	controller *Controller
	isClean    bool

	Drivers     []*DriverViewModel
	IsConnected bool
}

type DriverViewModel struct {
	namedDriver snes.NamedDriver
	devices     []snes.DeviceDescriptor

	Name string

	DisplayName        string
	DisplayDescription string
	DisplayOrder       int

	Devices        []string
	SelectedDevice int

	IsConnected bool
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
func (v *SNESViewModel) HandleCommand(name string, args Object) error {
	switch name {
	case "connect":
		return v.Connect(args)
	default:
		return fmt.Errorf("unrecognized command name '%s'", name)
	}
}

func (v *SNESViewModel) Connect(args Object) error {
	driverName, ok := args["driver"].(string)
	if !ok {
		return fmt.Errorf("expected string value for 'driver'")
	}
	i, ok := args["device"].(int)
	if !ok {
		return fmt.Errorf("expected int value for 'device'")
	}

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

	if i < 0 || i >= len(dvm.devices) {
		return fmt.Errorf("device index out of range")
	}
	dvm.SelectedDevice = i

	v.controller.SNESConnected(snes.NamedDriverDevicePair{
		NamedDriver: dvm.namedDriver,
		Device:      dvm.devices[i],
	})
	v.isClean = false

	return nil
}
