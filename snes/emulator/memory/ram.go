package memory

type RAM struct {
	data   []byte
	offset uint32
}

func NewRAM(data []byte, offset uint32) *RAM {
	return &RAM{data, offset}
}

func (m *RAM) Read(address uint32) byte {
	return m.data[address-m.offset]
}

func (m *RAM) Write(address uint32, value byte) {
	m.data[address-m.offset] = value
}

func (m *RAM) Shutdown() {
	panic("implement me")
}

func (m *RAM) Size() uint32 {
	return uint32(len(m.data))
}

func (m *RAM) Clear() {
	for i := range m.data {
		m.data[i] = 0
	}
}

func (m *RAM) Dump(address uint32) []byte {
	panic("implement me")
}
