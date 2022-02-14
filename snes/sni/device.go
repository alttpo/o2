package sni

import (
	"fmt"
	"o2/snes"
)

type DeviceDescriptor struct {
	snes.DeviceDescriptorBase
	Uri         string `json:"uri"`
	DisplayName string `json:"name"`
}

func (d *DeviceDescriptor) Base() *snes.DeviceDescriptorBase {
	return &d.DeviceDescriptorBase
}

func (d *DeviceDescriptor) GetId() string { return d.Uri }

func (d *DeviceDescriptor) GetDisplayName() string {
	return fmt.Sprintf("%s", d.Uri)
}
