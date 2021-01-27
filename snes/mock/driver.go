package mock

import "o2/snes"

const driverName = "mock"

type Driver struct{}

func (d *Driver) DisplayOrder() int {
	return 1000
}

func (d *Driver) DisplayName() string {
	return "Mock Device"
}

func (d *Driver) DisplayDescription() string {
	return "Connect to a mock SNES device for testing"
}

func (d *Driver) Open(desc snes.DeviceDescriptor) (snes.Conn, error) {
	c := &Conn{}
	c.Init(driverName)
	return c, nil
}

func (d *Driver) Detect() ([]snes.DeviceDescriptor, error) {
	return []snes.DeviceDescriptor{
		&MockDeviceDescriptor{},
	}, nil
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return &MockDeviceDescriptor{}
}

func init() {
	snes.Register(driverName, &Driver{})
}
