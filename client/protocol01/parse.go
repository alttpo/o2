package protocol01

import (
	"encoding/binary"
	"io"
)

type Header struct {
	Group      string
	Name       string
	Index      uint16
	ClientType uint8
}

func readTinyString(r io.Reader) (value string, err error) {
	var valueLength uint8
	if err = binary.Read(r, binary.LittleEndian, &valueLength); err != nil {
		return
	}

	valueBytes := make([]byte, valueLength)
	var n int
	n, err = r.Read(valueBytes)
	if err != nil {
		return
	}
	if n < int(valueLength) {
		return
	}

	value = string(valueBytes)
	return
}

func Parse(r io.Reader, header *Header) (err error) {
	if header.Group, err = readTinyString(r); err != nil {
		return
	}

	if header.Name, err = readTinyString(r); err != nil {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.Index); err != nil {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.ClientType); err != nil {
		return
	}

	return
}
