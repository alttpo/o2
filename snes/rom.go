package snes

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
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
		Contents:     contents,
		HeaderOffset: 0x7FB0,
	}

	// Read header:
	b := bytes.NewReader(contents[r.HeaderOffset : r.HeaderOffset+0x40])

	// reflection version of below code:
	hv := reflect.ValueOf(&r.Header).Elem()
	for i := 0; i < hv.NumField(); i++ {
		f := hv.Field(i)
		var p interface{}
		if f.CanAddr() {
			p = f.Addr().Interface()
			err = binary.Read(b, binary.LittleEndian, p)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", hv.Type().Field(i).Name, err)
			}
			//fmt.Printf("%s: %v\n", reflect.TypeOf(r.Header).Field(i).Name, f.Interface())
		} else {
			p = f.Interface()
			_, err = b.Read(p.([]byte))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", hv.Type().Field(i).Name, err)
			}
		}
	}

	return
}

func (r *ROM) ROMSize() uint32 {
	return 1024 << r.Header.ROMSize
}

func (r *ROM) RAMSize() uint32 {
	return 1024 << r.Header.RAMSize
}
