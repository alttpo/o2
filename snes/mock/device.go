package mock

type MockDeviceDescriptor struct {
}

func (m *MockDeviceDescriptor) DisplayName() string {
	return "Mock Device"
}
