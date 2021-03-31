package protocol02

import (
	"encoding/binary"
	"io"
)

type Header struct {
	Group [20]byte
	Kind  Kind
	Index uint16
}

func Parse(r io.Reader, header *Header) (err error) {
	_, err = r.Read(header.Group[:])
	if err != nil {
		return
	}

	var kind Kind
	if err = binary.Read(r, binary.LittleEndian, &kind); err != nil {
		return
	}

	var index uint16
	if err = binary.Read(r, binary.LittleEndian, &index); err != nil {
		return
	}

	return
}
