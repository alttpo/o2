package protocol02

import (
	"bytes"
	"encoding/binary"
)

func MakePacket(group []byte, kind Kind, index uint16) (buf *bytes.Buffer) {
	// construct message:
	buf = &bytes.Buffer{}
	header := uint16(25887)
	binary.Write(buf, binary.LittleEndian, &header)
	protocol := byte(0x02)
	buf.WriteByte(protocol)

	// protocol packet:
	buf.Write(group[:])
	buf.WriteByte(byte(kind))
	_ = binary.Write(buf, binary.LittleEndian, &index)

	return
}
