package snes

import (
	"bytes"
	"encoding/binary"
)

type ROM struct {
	Contents []byte

	HeaderOffset    uint32
	Header          Header
	NativeVectors   NativeVectors
	EmulatedVectors EmulatedVectors
}

// $FFB0
type Header struct {
	MakerCode          uint16
	GameCode           uint32
	Fixed1             [7]byte
	ExpansionRAMSize   byte
	SpecialVersion     byte
	CartridgeSubType   byte
	Title              [21]byte
	MapMode            byte
	CartridgeType      byte
	ROMSize            byte
	RAMSize            byte
	DestinationCode    byte
	Fixed2             byte
	MaskROMVersion     byte
	ComplementCheckSum uint16
	CheckSum           uint16
}

type NativeVectors struct {
	unused1 [4]byte
	COP     uint16
	BRK     uint16
	ABORT   uint16
	NMI     uint16
	unused2 uint16
	IRQ     uint16
}

type EmulatedVectors struct {
	unused1 [4]byte
	COP     uint16
	unused2 uint16
	ABORT   uint16
	NMI     uint16
	RESET   uint16
	IRQBRK  uint16
}

func NewROM(contents []byte) (r *ROM, err error) {
	r = &ROM{
		Contents: contents,
		HeaderOffset: 0x7FC0,
	}

	// Read header:
	b := bytes.NewReader(contents[r.HeaderOffset:r.HeaderOffset+0x20])

	// reflection version of below code:
	//hv := reflect.ValueOf(r.Header)
	//for i := 0; i < hv.NumField(); i++ {
	//	p := hv.Field(i).Interface()
	//	err = binary.Read(b, binary.LittleEndian, p)
	//	if err != nil {
	//		return nil, err
	//	}
	//}

	err = binary.Read(b, binary.LittleEndian, &r.Header.MakerCode)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.GameCode)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.Fixed1)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.ExpansionRAMSize)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.SpecialVersion)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.CartridgeSubType)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.Title)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.MapMode)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.CartridgeType)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.ROMSize)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.RAMSize)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.DestinationCode)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.Fixed2)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.MaskROMVersion)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.ComplementCheckSum)
	if err != nil {
		return nil, err
	}
	err = binary.Read(b, binary.LittleEndian, &r.Header.CheckSum)
	if err != nil {
		return nil, err
	}

	return
}

func (r *ROM) ROMSize() uint32 {
	return 1024 << r.Header.ROMSize
}

func (r *ROM) RAMSize() uint32 {
	return 1024 << r.Header.RAMSize
}
