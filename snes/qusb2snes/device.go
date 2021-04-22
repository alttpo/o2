package qusb2snes

import "o2/snes"

type DeviceDescriptor struct {
	snes.DeviceDescriptorBase
	Name string `json:"name"`
}

func (m *DeviceDescriptor) Base() *snes.DeviceDescriptorBase {
	return &m.DeviceDescriptorBase
}

func (m *DeviceDescriptor) GetId() string {
	return m.Name
}

func (m *DeviceDescriptor) GetDisplayName() string {
	return m.Name
}
