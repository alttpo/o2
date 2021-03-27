package snes

type ReadOrWriteResponse struct {
	IsWrite bool // was the request a read or write?
	Address uint32
	Size    uint8
	Data    []byte // the data that was read or written
}

type ReadRequest struct {
	// E00000-EFFFFF = SRAM
	// F50000-F6FFFF = WRAM
	// F70000-F8FFFF = VRAM
	// F90000-F901FF = CGRAM
	// F90200-F904FF = OAM
	Address   uint32
	Size      uint8
	Completed chan<- ReadOrWriteResponse
}

type WriteRequest struct {
	// E00000-EFFFFF = SRAM
	// F50000-F6FFFF = WRAM
	// F70000-F8FFFF = VRAM
	// F90000-F901FF = CGRAM
	// F90200-F904FF = OAM
	Address   uint32
	Size      uint8
	Data      []byte
	Completed chan<- ReadOrWriteResponse
}
