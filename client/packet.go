package client

import (
	"bytes"
	"encoding/binary"
)

func MakePacket(protocol byte) (buf *bytes.Buffer) {
	// construct message:
	buf = &bytes.Buffer{}
	header := uint16(25887)
	binary.Write(buf, binary.LittleEndian, &header)
	buf.WriteByte(protocol)
	return
}
