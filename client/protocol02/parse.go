package protocol02

import (
	"encoding/binary"
	"fmt"
	"io"
)

type Header struct {
	Group [20]byte
	Kind  Kind
	Index uint16
}

func Parse(r io.Reader, header *Header) (err error) {
	if _, err = r.Read(header.Group[:]); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error reading group: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.Kind); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error reading kind: %w", err))
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &header.Index); err != nil {
		// TODO: diagnostics
		panic(fmt.Errorf("error reading index: %w", err))
		return
	}

	return
}
