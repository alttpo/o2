package retroarch

import (
	"o2/interfaces"
	"o2/snes"
	"os"
)

const driverName = "retroarch"

type Driver struct{}

func (d *Driver) DisplayOrder() int {
	return 2
}

func (d *Driver) DisplayName() string {
	return "RetroArch"
}

func (d *Driver) DisplayDescription() string {
	return "Connect to a RetroArch emulator"
}

func (d *Driver) Open(desc snes.DeviceDescriptor) (snes.Queue, error) {
	c := &Queue{}
	c.BaseInit(driverName, c)
	c.Init()
	return c, nil
}

func (d *Driver) Detect() ([]snes.DeviceDescriptor, error) {
	return []snes.DeviceDescriptor{
		&DeviceDescriptor{},
	}, nil
}

func (d *Driver) Empty() snes.DeviceDescriptor {
	return &DeviceDescriptor{}
}

func init() {
	if interfaces.IsTruthy(os.Getenv("O2_RETROARCH_DISABLE")) {
		return
	}
	snes.Register(driverName, &Driver{})
}
