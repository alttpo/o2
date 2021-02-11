package fxpakpro

import (
	"fmt"
	"o2/snes"
)

type DeviceDescriptor struct {
	Port string
	Baud *int
	VID  string
	PID  string
}

func (d DeviceDescriptor) Equals(other snes.DeviceDescriptor) bool {
	otherd, ok := other.(DeviceDescriptor)
	if !ok {
		return false
	}
	return d.Port == otherd.Port
}

func (d DeviceDescriptor) DisplayName() string {
	return fmt.Sprintf("%s (%s:%s)", d.Port, d.VID, d.PID)
}
