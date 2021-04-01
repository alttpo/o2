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
	if _, err = r.Read(header.Group[:]); err != nil {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.Kind); err != nil {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.Index); err != nil {
		return
	}

	return
}
