package snes

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
)

type ROM struct {
	Name     string
	Path     string
	Contents []byte

	HeaderOffset    uint32
	Header          Header
	NativeVectors   NativeVectors
	EmulatedVectors EmulatedVectors
}

// $FFB0
type Header struct {
	MakerCode          uint16   `rom:"FFB0"`
	GameCode           uint32   `rom:"FFB2"`
	Fixed1             [7]byte  //`rom:"FFB6"`
	ExpansionRAMSize   byte     `rom:"FFBD"`
	SpecialVersion     byte     `rom:"FFBE"`
	CartridgeSubType   byte     `rom:"FFBF"`
	Title              [21]byte `rom:"FFC0"`
	MapMode            byte     `rom:"FFD5"`
	CartridgeType      byte     `rom:"FFD6"`
	ROMSize            byte     `rom:"FFD7"`
	RAMSize            byte     `rom:"FFD8"`
	DestinationCode    byte     `rom:"FFD9"`
	Fixed2             byte     //`rom:"FFDA"`
	MaskROMVersion     byte     `rom:"FFDB"`
	ComplementCheckSum uint16   `rom:"FFDC"`
	CheckSum           uint16   `rom:"FFDE"`
}

type NativeVectors struct {
	Unused1 [4]byte //`rom:"FFE0"`
	COP     uint16  `rom:"FFE4"`
	BRK     uint16  `rom:"FFE6"`
	ABORT   uint16  `rom:"FFE8"`
	NMI     uint16  `rom:"FFEA"`
	Unused2 uint16  //`rom:"FFEC"`
	IRQ     uint16  `rom:"FFEE"`
}

type EmulatedVectors struct {
	Unused1 [4]byte //`rom:"FFF0"`
	COP     uint16  `rom:"FFF4"`
	Unused2 uint16  //`rom:"FFF6"`
	ABORT   uint16  `rom:"FFF8"`
	NMI     uint16  `rom:"FFFA"`
	RESET   uint16  `rom:"FFFC"`
	IRQBRK  uint16  `rom:"FFFE"`
}

func NewROM(name string, path string, contents []byte) (r *ROM, err error) {
	if len(contents) < 0x8000 {
		return nil, fmt.Errorf("ROM file not big enough to contain SNES header")
	}

	headerOffset := uint32(0x007FB0)

	r = &ROM{
		Name:         name,
		Path:         path,
		Contents:     contents,
		HeaderOffset: headerOffset,
	}

	err = r.ReadHeader()
	return
}

func (r *ROM) ReadHeader() (err error) {
	// Read SNES header:
	b := bytes.NewReader(r.Contents[r.HeaderOffset : r.HeaderOffset+0x50])
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

type alwaysError struct{}

func (alwaysError) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (alwaysError) Write(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

var alwaysErrorInstance = &alwaysError{}

func (r *ROM) BusReader(busAddr uint32) io.Reader {
	page := busAddr & 0xFFFF
	if page < 0x8000 {
		return alwaysErrorInstance
	}

	// Return a reader over the ROM contents up to the next bank to prevent accidental overflow:
	bank := busAddr >> 16
	pcStart := (bank << 15) | (page - 0x8000)
	pcEnd := (bank << 15) | 0x7FFF
	return bytes.NewReader(r.Contents[pcStart:pcEnd])
}

type busWriter struct {
	r       *ROM
	busAddr uint32
	start   uint32
	end     uint32
	o       uint32
}

func (w busWriter) Write(p []byte) (n int, err error) {
	if uint32(len(p)) >= w.o+w.end {
		err = io.ErrUnexpectedEOF
		return
	}

	n = copy(w.r.Contents[w.o+w.start:w.end], p)
	w.o += uint32(n)

	return
}

func (r *ROM) BusWriter(busAddr uint32) io.Writer {
	page := busAddr & 0xFFFF
	if page < 0x8000 {
		return alwaysErrorInstance
	}

	// Return a reader over the ROM contents up to the next bank to prevent accidental overflow:
	bank := busAddr >> 16
	pcStart := (bank << 15) | (page - 0x8000)
	pcEnd := (bank << 15) | 0x7FFF
	return &busWriter{r, busAddr, pcStart, pcEnd, 0}
}
