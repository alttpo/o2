package main

import (
	"fmt"
	"o2/snes"
)

type Object map[string]interface{}

type ViewModel interface {
	HandleCommand(name string, args Object) error
}

// Must be JSON serializable
type SNESViewModel struct {
	controller *Controller

	Drivers     []*DriverViewModel
	IsConnected bool
}

type DriverViewModel struct {
	namedDriver snes.NamedDriver
	devices     []snes.DeviceDescriptor

	Name         string
	DisplayOrder int

	Devices        []string
	SelectedDevice int

	IsConnected bool
}

func (v *SNESViewModel) refresh() {

	v.IsConnected = v.controller.IsConnected()
	for _, d := range v.Drivers {
		d.IsConnected = v.controller.IsConnectedToDriver(d.namedDriver)
	}
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
	di, ok := args["driver"].(int)
	if !ok {
		return fmt.Errorf("expected int type for 'driver'")
	}
	i, ok := args["device"].(int)
	if !ok {
		return fmt.Errorf("expected int type for 'device'")
	}

	if di < 0 || di >= len(v.Drivers) {
		return fmt.Errorf("driver index out of range")
	}
	dvm := v.Drivers[di]

	if i < 0 || i >= len(dvm.devices) {
		return fmt.Errorf("device index out of range")
	}
	dvm.SelectedDevice = i

	v.controller.SNESConnected(snes.NamedDriverDevicePair{
		NamedDriver: dvm.namedDriver,
		Device:      dvm.devices[i],
	})

	return nil
}
