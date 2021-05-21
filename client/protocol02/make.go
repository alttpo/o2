package protocol02

import (
	"bytes"
	"encoding/binary"
	"o2/client"
)

func MakePacket(group []byte, kind Kind, index uint16) (buf *bytes.Buffer) {
	// construct packet:
	buf = client.MakePacket(0x02)

	// protocol packet:
	buf.Write(group[:])
	buf.WriteByte(byte(kind))
	_ = binary.Write(buf, binary.LittleEndian, &index)

	return
}
