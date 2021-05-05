package memory

type FakeHW struct {
	state [0x6000]byte
}

func (f *FakeHW) Read(address uint32) (value byte) {
	offs := address & 0xFFFF
	switch offs {
	default:
		value = f.state[offs-0x2000]
	}

	//log.Printf("hwio[$%06x] -> $%02x\n", address, value)
	return
}

func (f *FakeHW) Write(address uint32, value byte) {
	offs := address & 0xFFFF

	//log.Printf("hwio[$%06x] <- $%02x\n", address, value)
	f.state[offs-0x2000] = value
}

func (f *FakeHW) Shutdown() {
}

func (f *FakeHW) Size() uint32 {
	return 0x6000
}

func (f *FakeHW) Clear() {
}

func (f *FakeHW) Dump(address uint32) []byte {
	return nil
}
