package mock

import "o2/snes"

type DeviceDescriptor struct {
}

func (m DeviceDescriptor) Equals(other snes.DeviceDescriptor) bool {
	_, ok := other.(DeviceDescriptor)
	if !ok {
		return false
	}
	return true
}

func (m DeviceDescriptor) DisplayName() string {
	return "Mock"
}
