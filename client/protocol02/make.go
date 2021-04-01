package protocol02

import (
	"bytes"
	"encoding/binary"
)

func MakePacket(groupBuf []byte, kind Kind) (buf *bytes.Buffer) {
	// construct message:
	buf = &bytes.Buffer{}
	header := uint16(25887)
	binary.Write(buf, binary.LittleEndian, &header)
	protocol := byte(0x02)
	buf.WriteByte(protocol)

	// protocol packet:
	buf.Write(groupBuf)
	responseKind := kind | 0x80
	buf.WriteByte(byte(responseKind))

	return
}

