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
	Unused1 [4]byte
	COP     uint16
	BRK     uint16
	ABORT   uint16
	NMI     uint16
	Unused2 uint16
	IRQ     uint16
}

type EmulatedVectors struct {
	Unused1 [4]byte
	COP     uint16
	Unused2 uint16
	ABORT   uint16
	NMI     uint16
	RESET   uint16
	IRQBRK  uint16
}

func NewROM(contents []byte) (r *ROM, err error) {
	if len(contents) < 0x8000 {
		return nil, fmt.Errorf("ROM file not big enough to contain SNES header")
	}

	headerOffset := uint32(0x007FB0)

	r = &ROM{
		Contents:     contents,
		HeaderOffset: headerOffset,
	}

	// Read SNES header:
	b := bytes.NewReader(contents[headerOffset : headerOffset+0x50])
	err = readBinaryStruct(b, &r.Header)
	if err != nil {
		return
	}
	err = readBinaryStruct(b, &r.NativeVectors)
	if err != nil {
		return
	}
	err = readBinaryStruct(b, &r.EmulatedVectors)
	if err != nil {
		return
	}

	return
}

func readBinaryStruct(b *bytes.Reader, into interface{}) (err error) {
	hv := reflect.ValueOf(into).Elem()
	for i := 0; i < hv.NumField(); i++ {
		f := hv.Field(i)
		var p interface{}

		if !f.CanAddr() {
			panic(fmt.Errorf("error handling struct field %s of type %s; cannot take address of field", hv.Type().Field(i).Name, hv.Type().Name()))
			//p = f.Interface()
			//_, err = b.Read(p.([]byte))
			//if err != nil {
			//	return fmt.Errorf("error reading header field %s: %w", hv.Type().Field(i).Name, err)
			//}
		}

		p = f.Addr().Interface()
		err = binary.Read(b, binary.LittleEndian, p)
		if err != nil {
			return fmt.Errorf("error reading struct field %s of type %s: %w", hv.Type().Field(i).Name, hv.Type().Name(), err)
		}
		//fmt.Printf("%s: %v\n", reflect.TypeOf(r.Header).Field(i).Name, f.Interface())
	}
	return
}

func (r *ROM) ROMSize() uint32 {
	return 1024 << r.Header.ROMSize
}

func (r *ROM) RAMSize() uint32 {
	return 1024 << r.Header.RAMSize
}
