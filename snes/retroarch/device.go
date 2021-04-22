package retroarch

import "o2/snes"

type DeviceDescriptor struct {
	snes.DeviceDescriptorBase
}

func (d *DeviceDescriptor) Base() *snes.DeviceDescriptorBase {
	return &d.DeviceDescriptorBase
}

func (d *DeviceDescriptor) GetId() string {
	return "retroarch"
}

func (d *DeviceDescriptor) GetDisplayName() string {
	return "RetroArch"
}
