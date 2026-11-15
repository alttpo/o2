// Package protocol04 implements o2-server protocol 0x04.
//
// Protocol 0x04 mirrors protocol 0x03's GroupMessage contract using a simple hand-rolled binary encoding instead of
// protobuf. All multi-byte integers are little-endian.
//
// Packet layout (after the common 2-byte header 0x651F and 1-byte protocol 0x04):
//
//	GroupMessage:
//	  uint8   groupLength
//	  [N]byte group            (N = groupLength, max 255)
//	  int64   playerTime
//	  int64   serverTime
//	  uint32  playerIndex
//	  uint64  playerInSector
//	  uint8   kind             (discriminates the `oneof message`; see Kind)
//	  ...     kind-specific body:
//
//	  JoinGroup       (0x01): (empty)
//	  BroadcastAll    (0x02): uint16 dataLength, [N]byte data
//	  BroadcastSector (0x03): uint64 targetSector, uint16 dataLength, [N]byte data
//	  Echo            (0x04): uint16 dataLength, [N]byte data
//
// Trailing bytes after the kind-specific body are ignored.
//
// The server overwrites playerIndex with the sender's index and serverTime with its own clock (UnixNano) before
// relaying a message. JoinGroup and Echo are answered only to the sender; BroadcastAll and BroadcastSector are relayed
// to every other client in the group (BroadcastSector only to clients whose playerInSector equals targetSector).
package protocol04

import (
	"encoding/binary"
	"errors"
	"fmt"
	"o2/client"
)

const Protocol = 0x04

type Kind uint8

const (
	JoinGroup       = Kind(0x01)
	BroadcastAll    = Kind(0x02)
	BroadcastSector = Kind(0x03)
	Echo            = Kind(0x04)
)

func (k Kind) String() string {
	switch k {
	case JoinGroup:
		return "joinGroup"
	case BroadcastAll:
		return "broadcastAll"
	case BroadcastSector:
		return "broadcastSector"
	case Echo:
		return "echo"
	}
	return fmt.Sprintf("unknown(0x%02x)", uint8(k))
}

type GroupMessage struct {
	Group          string
	PlayerTime     int64
	ServerTime     int64
	PlayerIndex    uint32
	PlayerInSector uint64

	Kind Kind

	// TargetSector is only used for BroadcastSector:
	TargetSector uint64
	// Data is used for BroadcastAll, BroadcastSector, and Echo:
	Data []byte
}

var (
	ErrTruncated    = errors.New("p4: message truncated")
	ErrUnknownKind  = errors.New("p4: unknown message kind")
	ErrGroupTooLong = errors.New("p4: group name exceeds 255 bytes")
	ErrDataTooLong  = errors.New("p4: data exceeds 65535 bytes")
)

type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if len(r.b) < n {
		r.err = ErrTruncated
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() uint8 {
	if v := r.take(1); v != nil {
		return v[0]
	}
	return 0
}

func (r *reader) u16() uint16 {
	if v := r.take(2); v != nil {
		return binary.LittleEndian.Uint16(v)
	}
	return 0
}

func (r *reader) u32() uint32 {
	if v := r.take(4); v != nil {
		return binary.LittleEndian.Uint32(v)
	}
	return 0
}

func (r *reader) u64() uint64 {
	if v := r.take(8); v != nil {
		return binary.LittleEndian.Uint64(v)
	}
	return 0
}

// Unmarshal decodes a GroupMessage body (the bytes following the protocol byte). Data aliases b.
func Unmarshal(b []byte, gm *GroupMessage) error {
	r := &reader{b: b}

	gm.Group = string(r.take(int(r.u8())))
	gm.PlayerTime = int64(r.u64())
	gm.ServerTime = int64(r.u64())
	gm.PlayerIndex = r.u32()
	gm.PlayerInSector = r.u64()
	gm.Kind = Kind(r.u8())
	gm.TargetSector = 0
	gm.Data = nil
	if r.err != nil {
		return r.err
	}

	switch gm.Kind {
	case JoinGroup:
	case BroadcastAll, Echo:
		gm.Data = r.take(int(r.u16()))
	case BroadcastSector:
		gm.TargetSector = r.u64()
		gm.Data = r.take(int(r.u16()))
	default:
		return ErrUnknownKind
	}

	return r.err
}

// Append encodes gm and appends it to dst, returning the extended slice.
func Append(dst []byte, gm *GroupMessage) ([]byte, error) {
	if len(gm.Group) > 0xFF {
		return dst, ErrGroupTooLong
	}
	if len(gm.Data) > 0xFFFF {
		return dst, ErrDataTooLong
	}

	dst = append(dst, uint8(len(gm.Group)))
	dst = append(dst, gm.Group...)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(gm.PlayerTime))
	dst = binary.LittleEndian.AppendUint64(dst, uint64(gm.ServerTime))
	dst = binary.LittleEndian.AppendUint32(dst, gm.PlayerIndex)
	dst = binary.LittleEndian.AppendUint64(dst, gm.PlayerInSector)
	dst = append(dst, uint8(gm.Kind))

	switch gm.Kind {
	case JoinGroup:
	case BroadcastAll, Echo:
		dst = binary.LittleEndian.AppendUint16(dst, uint16(len(gm.Data)))
		dst = append(dst, gm.Data...)
	case BroadcastSector:
		dst = binary.LittleEndian.AppendUint64(dst, gm.TargetSector)
		dst = binary.LittleEndian.AppendUint16(dst, uint16(len(gm.Data)))
		dst = append(dst, gm.Data...)
	default:
		return dst, ErrUnknownKind
	}

	return dst, nil
}

// MakePacket builds a complete UDP packet: header, protocol byte, and encoded GroupMessage.
func MakePacket(gm *GroupMessage) ([]byte, error) {
	return Append(client.MakePacket(Protocol).Bytes(), gm)
}
