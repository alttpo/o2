package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

func ParseHeader(msg []byte, protocol *uint8) (r io.Reader, err error) {
	var hdr uint16

	r = bytes.NewReader(msg)
	err = binary.Read(r, binary.LittleEndian, &hdr)
	if err != nil {
		return
	}
	if hdr != 25887 {
		err = fmt.Errorf("bad message header")
		return
	}

	err = binary.Read(r, binary.LittleEndian, protocol)
	if err != nil {
		return
	}

	return
}
