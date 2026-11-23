// Package protocol05 implements the o2-server protocol 0x05 envelope; see o2-server's PROTOCOL05.md.
//
// All integers are little-endian. Every packet starts with the 2-byte magic 0x651F, the protocol byte 0x05 and a kind
// byte. The data of broadcasts, relays and fact chunks runs to the end of the datagram.
package protocol05

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const Protocol = 0x05

const magic = 25887

type Kind uint8

// client → server:
const (
	JoinGroup       = Kind(0x01)
	LeaveGroup      = Kind(0x02)
	JoinSector      = Kind(0x03)
	LeaveSector     = Kind(0x04)
	Echo            = Kind(0x05)
	WhoIs           = Kind(0x06)
	FactPut         = Kind(0x07)
	FactGet         = Kind(0x08)
	BroadcastAll    = Kind(0x09)
	BroadcastSector = Kind(0x0A)
)

// server → client:
const (
	JoinAck     = Kind(0x81)
	JoinNak     = Kind(0x82)
	SectorAck   = Kind(0x83)
	EchoReply   = Kind(0x84)
	PlayerInfo  = Kind(0x85)
	PlayerLeft  = Kind(0x86)
	FactStatus  = Kind(0x87)
	FactList    = Kind(0x88)
	FactData    = Kind(0x89)
	RelayAll    = Kind(0x8A)
	RelaySector = Kind(0x8B)
)

var kindNames = map[Kind]string{
	JoinGroup: "joinGroup", LeaveGroup: "leaveGroup", JoinSector: "joinSector", LeaveSector: "leaveSector",
	Echo: "echo", WhoIs: "whoIs", FactPut: "factPut", FactGet: "factGet", BroadcastAll: "broadcastAll",
	BroadcastSector: "broadcastSector",
	JoinAck:         "joinAck", JoinNak: "joinNak", SectorAck: "sectorAck", EchoReply: "echoReply", PlayerInfo: "playerInfo",
	PlayerLeft: "playerLeft", FactStatus: "factStatus", FactList: "factList", FactData: "factData",
	RelayAll: "relayAll", RelaySector: "relaySector",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("unknown(0x%02x)", uint8(k))
}

// join-nak reasons:
const (
	NakNotJoined = 0
	NakGroupFull = 1
)

// player-left reasons:
const (
	LeftLeft     = 0
	LeftTimedOut = 1
	LeftMoved    = 2
)

// fact-status states:
const (
	FactReceiving = 0
	FactComplete  = 1
	FactRejected  = 2
)

const (
	// FactChunkSize is the size of every chunk of a fact but the last:
	FactChunkSize = 1024
	// MaxFactSize is the largest fact; its 64 chunks fit a u64 bitmap:
	MaxFactSize = 64 * FactChunkSize
	// HashSize is the size of a fact's hash (SHA-256 truncated, computed by the server):
	HashSize = 16
	// PackLimit is the most bytes packed into one player-info, fact-list or who-is packet:
	PackLimit = 1200
)

// Hash identifies a fact's contents.
type Hash [HashSize]byte

// ChunkCount returns the number of chunks of a fact of size bytes.
func ChunkCount(size int) int {
	return (size + FactChunkSize - 1) / FactChunkSize
}

// AllChunks returns the bitmap with every chunk of a fact of size bytes set.
func AllChunks(size int) uint64 {
	n := ChunkCount(size)
	if n >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << n) - 1
}

// ChunkLen returns the length of chunk i of a fact of size bytes.
func ChunkLen(size int, i int) int {
	n := size - i*FactChunkSize
	if n > FactChunkSize {
		n = FactChunkSize
	}
	return n
}

// ExpandServerTime rebuilds a full server time from the low 16 bits of its Unix milliseconds: the latest time no later
// than estimate + 1 s whose low 16 bits match. estimate is the client's estimate of the server's current time.
func ExpandServerTime(ms uint16, estimate time.Time) time.Time {
	limit := estimate.Add(time.Second).UnixMilli()
	t := (limit &^ 0xFFFF) | int64(ms)
	if t > limit {
		t -= 0x10000
	}
	return time.UnixMilli(t)
}

var (
	ErrTruncated  = errors.New("p5: message truncated")
	ErrBadHeader  = errors.New("p5: bad header")
	ErrTooLong    = errors.New("p5: field too long")
	ErrUnexpected = errors.New("p5: unexpected kind")
)

func start(kind Kind, capacity int) []byte {
	b := make([]byte, 0, 4+capacity)
	b = binary.LittleEndian.AppendUint16(b, magic)
	return append(b, Protocol, uint8(kind))
}

// Header checks the magic and protocol of a packet and returns its kind and the body after the kind byte.
func Header(pkt []byte) (kind Kind, body []byte, err error) {
	if len(pkt) < 4 || binary.LittleEndian.Uint16(pkt) != magic || pkt[2] != Protocol {
		return 0, nil, ErrBadHeader
	}
	return Kind(pkt[3]), pkt[4:], nil
}

type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.b) < n {
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

func (r *reader) tiny() []byte { return r.take(int(r.u8())) }

func (r *reader) rest() []byte {
	if r.err != nil {
		return nil
	}
	v := r.b
	r.b = nil
	return v
}

// Client → server packets:

// Join is a join-group.
type Join struct {
	JoinSeq   uint8
	SessionID uint64
	Group     string
	Name      string
	Info      []byte
}

func MakeJoin(j *Join) ([]byte, error) {
	if len(j.Group) > 0xFF || len(j.Name) > 0xFF || len(j.Info) > 0xFF {
		return nil, ErrTooLong
	}
	b := start(JoinGroup, 12+len(j.Group)+len(j.Name)+len(j.Info))
	b = append(b, j.JoinSeq)
	b = binary.LittleEndian.AppendUint64(b, j.SessionID)
	b = append(b, uint8(len(j.Group)))
	b = append(b, j.Group...)
	b = append(b, uint8(len(j.Name)))
	b = append(b, j.Name...)
	b = append(b, uint8(len(j.Info)))
	b = append(b, j.Info...)
	return b, nil
}

func MakeLeave() []byte { return start(LeaveGroup, 0) }

func MakeJoinSector(seq uint8, sector uint32) []byte {
	b := append(start(JoinSector, 5), seq)
	return binary.LittleEndian.AppendUint32(b, sector)
}

func MakeLeaveSector(seq uint8) []byte { return append(start(LeaveSector, 1), seq) }

func MakeEcho(clientTime int64) []byte {
	return binary.LittleEndian.AppendUint64(start(Echo, 8), uint64(clientTime))
}

func MakeWhoIs(indexes []uint16) []byte {
	b := start(WhoIs, 2*len(indexes))
	for _, i := range indexes {
		b = binary.LittleEndian.AppendUint16(b, i)
	}
	return b
}

func MakeFactPut(factNum uint8, size int, chunk int, data []byte) []byte {
	b := append(start(FactPut, 6+len(data)), factNum)
	b = binary.LittleEndian.AppendUint32(b, uint32(size))
	b = append(b, uint8(chunk))
	return append(b, data...)
}

// FactRequest asks for chunks of a player's fact.
type FactRequest struct {
	PlayerIndex uint16
	FactNum     uint8
	Chunks      uint64
}

func MakeFactGet(reqs []FactRequest) []byte {
	b := start(FactGet, 11*len(reqs))
	for _, q := range reqs {
		b = binary.LittleEndian.AppendUint16(b, q.PlayerIndex)
		b = append(b, q.FactNum)
		b = binary.LittleEndian.AppendUint64(b, q.Chunks)
	}
	return b
}

// BroadcastHeaderSize is the size of a broadcast-all packet before its data; a broadcast-sector adds 4 bytes:
const BroadcastHeaderSize = 4

func MakeBroadcastAll(data []byte) []byte {
	return append(start(BroadcastAll, len(data)), data...)
}

func MakeBroadcastSector(sector uint32, data []byte) []byte {
	b := binary.LittleEndian.AppendUint32(start(BroadcastSector, 4+len(data)), sector)
	return append(b, data...)
}

// Server → client packets:

// PlayerInfoEntry is one player in a player-info.
type PlayerInfoEntry struct {
	PlayerIndex uint16
	Name        string
	Info        []byte
}

// FactEntry is one fact in a fact-list.
type FactEntry struct {
	FactNum uint8
	Size    int
	Hash    Hash
}

// Message is a decoded server → client packet. Only the fields of its Kind are set; byte slices alias the packet.
type Message struct {
	Kind Kind

	JoinSeq     uint8     // JoinAck
	PlayerIndex uint16    // JoinAck, PlayerLeft, FactList, FactData, RelayAll, RelaySector
	ServerTime  time.Time // JoinAck, EchoReply
	Reason      uint8     // JoinNak, PlayerLeft
	SectorSeq   uint8     // SectorAck
	ClientTime  int64     // EchoReply

	Players []PlayerInfoEntry // PlayerInfo
	Facts   []FactEntry       // FactList

	FactNum   uint8  // FactStatus, FactData
	FactState uint8  // FactStatus
	Received  uint64 // FactStatus
	Chunk     uint8  // FactData

	ServerTimeMs uint16 // RelayAll, RelaySector
	Sector       uint32 // RelaySector
	Data         []byte // FactData, RelayAll, RelaySector
}

// Decode decodes a server → client packet.
func Decode(pkt []byte) (m Message, err error) {
	var body []byte
	if m.Kind, body, err = Header(pkt); err != nil {
		return
	}
	r := &reader{b: body}
	switch m.Kind {
	case JoinAck:
		m.JoinSeq = r.u8()
		m.PlayerIndex = r.u16()
		m.ServerTime = time.Unix(0, int64(r.u64()))
	case JoinNak:
		m.Reason = r.u8()
	case SectorAck:
		m.SectorSeq = r.u8()
	case EchoReply:
		m.ClientTime = int64(r.u64())
		m.ServerTime = time.Unix(0, int64(r.u64()))
	case PlayerInfo:
		for r.err == nil && len(r.b) > 0 {
			e := PlayerInfoEntry{PlayerIndex: r.u16()}
			e.Name = string(r.tiny())
			e.Info = r.tiny()
			if r.err == nil {
				m.Players = append(m.Players, e)
			}
		}
	case PlayerLeft:
		m.PlayerIndex = r.u16()
		m.Reason = r.u8()
	case FactStatus:
		m.FactNum = r.u8()
		m.FactState = r.u8()
		m.Received = r.u64()
	case FactList:
		m.PlayerIndex = r.u16()
		for r.err == nil && len(r.b) > 0 {
			e := FactEntry{FactNum: r.u8(), Size: int(r.u32())}
			copy(e.Hash[:], r.take(HashSize))
			if r.err == nil {
				m.Facts = append(m.Facts, e)
			}
		}
	case FactData:
		m.PlayerIndex = r.u16()
		m.FactNum = r.u8()
		m.Chunk = r.u8()
		m.Data = r.rest()
	case RelayAll, RelaySector:
		m.PlayerIndex = r.u16()
		m.ServerTimeMs = r.u16()
		if m.Kind == RelaySector {
			m.Sector = r.u32()
		}
		m.Data = r.rest()
	default:
		return m, ErrUnexpected
	}
	err = r.err
	return
}

// Encoders of server → client packets, for test servers:

func MakeJoinAck(seq uint8, index uint16, serverTime time.Time) []byte {
	b := append(start(JoinAck, 11), seq)
	b = binary.LittleEndian.AppendUint16(b, index)
	return binary.LittleEndian.AppendUint64(b, uint64(serverTime.UnixNano()))
}

func MakeJoinNak(reason uint8) []byte { return append(start(JoinNak, 1), reason) }

func MakeSectorAck(seq uint8) []byte { return append(start(SectorAck, 1), seq) }

func MakeEchoReply(clientTime int64, serverTime time.Time) []byte {
	b := binary.LittleEndian.AppendUint64(start(EchoReply, 16), uint64(clientTime))
	return binary.LittleEndian.AppendUint64(b, uint64(serverTime.UnixNano()))
}

func MakePlayerInfo(es []PlayerInfoEntry) []byte {
	b := start(PlayerInfo, 64)
	for _, e := range es {
		b = binary.LittleEndian.AppendUint16(b, e.PlayerIndex)
		b = append(b, uint8(len(e.Name)))
		b = append(b, e.Name...)
		b = append(b, uint8(len(e.Info)))
		b = append(b, e.Info...)
	}
	return b
}

func MakePlayerLeft(index uint16, reason uint8) []byte {
	return append(binary.LittleEndian.AppendUint16(start(PlayerLeft, 3), index), reason)
}

func MakeFactStatus(factNum, state uint8, received uint64) []byte {
	return binary.LittleEndian.AppendUint64(append(start(FactStatus, 10), factNum, state), received)
}

func MakeFactList(index uint16, es []FactEntry) []byte {
	b := binary.LittleEndian.AppendUint16(start(FactList, 2+21*len(es)), index)
	for _, e := range es {
		b = append(b, e.FactNum)
		b = binary.LittleEndian.AppendUint32(b, uint32(e.Size))
		b = append(b, e.Hash[:]...)
	}
	return b
}

func MakeFactData(index uint16, factNum uint8, chunk int, data []byte) []byte {
	b := binary.LittleEndian.AppendUint16(start(FactData, 4+len(data)), index)
	return append(append(b, factNum, uint8(chunk)), data...)
}

func MakeRelay(index uint16, serverTime time.Time, sector bool, target uint32, data []byte) []byte {
	kind := RelayAll
	if sector {
		kind = RelaySector
	}
	b := binary.LittleEndian.AppendUint16(start(kind, 8+len(data)), index)
	b = binary.LittleEndian.AppendUint16(b, uint16(serverTime.UnixMilli()))
	if sector {
		b = binary.LittleEndian.AppendUint32(b, target)
	}
	return append(b, data...)
}

// Request is a decoded client → server packet, for test servers.
type Request struct {
	Kind Kind

	Join       Join          // JoinGroup
	SectorSeq  uint8         // JoinSector, LeaveSector
	Sector     uint32        // JoinSector, BroadcastSector (target)
	ClientTime int64         // Echo
	Indexes    []uint16      // WhoIs
	FactNum    uint8         // FactPut
	FactSize   int           // FactPut
	Chunk      int           // FactPut
	Gets       []FactRequest // FactGet
	Data       []byte        // FactPut, BroadcastAll, BroadcastSector
}

// DecodeRequest decodes a client → server packet.
func DecodeRequest(pkt []byte) (q Request, err error) {
	var body []byte
	if q.Kind, body, err = Header(pkt); err != nil {
		return
	}
	r := &reader{b: body}
	switch q.Kind {
	case JoinGroup:
		q.Join.JoinSeq = r.u8()
		q.Join.SessionID = r.u64()
		q.Join.Group = string(r.tiny())
		q.Join.Name = string(r.tiny())
		q.Join.Info = r.tiny()
	case LeaveGroup:
	case JoinSector:
		q.SectorSeq = r.u8()
		q.Sector = r.u32()
	case LeaveSector:
		q.SectorSeq = r.u8()
	case Echo:
		q.ClientTime = int64(r.u64())
	case WhoIs:
		for len(r.b) >= 2 {
			q.Indexes = append(q.Indexes, r.u16())
		}
	case FactPut:
		q.FactNum = r.u8()
		q.FactSize = int(r.u32())
		q.Chunk = int(r.u8())
		q.Data = r.rest()
	case FactGet:
		for len(r.b) >= 11 {
			q.Gets = append(q.Gets, FactRequest{PlayerIndex: r.u16(), FactNum: r.u8(), Chunks: r.u64()})
		}
	case BroadcastAll:
		q.Data = r.rest()
	case BroadcastSector:
		q.Sector = r.u32()
		q.Data = r.rest()
	default:
		return q, ErrUnexpected
	}
	err = r.err
	return
}
