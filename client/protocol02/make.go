package protocol02

import (
	"bytes"
	"encoding/binary"
	"o2/client"
)

func MakePacket(group []byte, kind Kind, index uint16) (buf *bytes.Buffer) {
	// construct packet:
	buf = client.MakePacket(0x02)

	// protocol packet; group name is exactly 20 bytes, space padded:
	var groupPadded [20]byte
	n := copy(groupPadded[:], group)
	for ; n < len(groupPadded); n++ {
		groupPadded[n] = ' '
	}
	buf.Write(groupPadded[:])
	buf.WriteByte(byte(kind))
	_ = binary.Write(buf, binary.LittleEndian, &index)

	return
}
