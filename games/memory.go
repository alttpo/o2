package games

type MemoryKind int

const (
	SRAM MemoryKind = iota
	WRAM
	// TODO: other kinds of RAM
)

type ReadableMemory interface {
	IsFresh(offs uint32) bool

	BusAddress(offs uint32) uint32

	ReadU8(offs uint32) uint8
	ReadU16(offs uint32) uint16
	//ReadU24(offs uint32) uint32
}
