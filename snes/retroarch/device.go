package retroarch

import (
	"fmt"
	"net"
	"o2/snes"
)

type DeviceDescriptor struct {
	snes.DeviceDescriptorBase

	addr *net.UDPAddr
}

func (d *DeviceDescriptor) Base() *snes.DeviceDescriptorBase {
	return &d.DeviceDescriptorBase
}

func (d *DeviceDescriptor) GetId() string {
	return fmt.Sprintf("retroarch-%s", d.addr)
}

func (d *DeviceDescriptor) GetDisplayName() string {
	return fmt.Sprintf("RetroArch at %s", d.addr)
}
