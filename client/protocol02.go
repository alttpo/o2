package client

import (
	"bytes"
	"encoding/binary"
)

type P02Kind byte

const (
	RequestIndex      = P02Kind(0x00)
	Broadcast         = P02Kind(0x01)
	BroadcastToSector = P02Kind(0x02)
)

func (k P02Kind) String() string {
	switch k {
	case RequestIndex:
		return "request_index"
	case Broadcast:
		return "broadcast"
	case BroadcastToSector:
		return "broadcast_to_sector"
	}
	return "unknown"
}

func make02Packet(groupBuf []byte, kind P02Kind) (buf *bytes.Buffer) {
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

