package protocol05

import (
	"crypto/sha256"
	"time"
)

// Hub is an in-memory protocol 0x05 server for tests. It routes packets between connections the way o2-server does,
// for one group, without timeouts, grace periods or quotas. Connections are numbered by the test; a connection's
// player index is the order it first joined in.
type Hub struct {
	Now func() time.Time

	byConn  map[int]int
	players []hubPlayer

	// the group's password token, set by the first join with one; created is true once anyone joined:
	created bool
	token   []byte
}

type hubPlayer struct {
	conn      int
	live      bool
	sessionID uint64
	name      string
	info      []byte
	inSector  bool
	sector    uint32
	sectorSeq uint8
	seqSet    bool
	facts     map[uint8]*hubFact
}

type hubFact struct {
	size     int
	received uint64
	data     []byte
	complete bool
	hash     Hash
}

// Delivery is a packet the hub sends to a connection.
type Delivery struct {
	To  int
	Pkt []byte
}

func NewHub(now func() time.Time) *Hub {
	return &Hub{Now: now, byConn: make(map[int]int)}
}

// PlayerIndex returns the player index of a connection, or -1.
func (h *Hub) PlayerIndex(conn int) int {
	if i, ok := h.byConn[conn]; ok && h.players[i].live {
		return i
	}
	return -1
}

// Sector returns the sector a connection's player is in.
func (h *Hub) Sector(conn int) (sector uint32, ok bool) {
	i := h.PlayerIndex(conn)
	if i < 0 {
		return 0, false
	}
	return h.players[i].sector, h.players[i].inSector
}

// Handle processes a packet from a connection and returns what the hub sends in reply.
func (h *Hub) Handle(conn int, pkt []byte) (out []Delivery) {
	q, err := DecodeRequest(pkt)
	if err != nil {
		return nil
	}
	send := func(to int, b []byte) { out = append(out, Delivery{To: to, Pkt: b}) }
	others := func(except int, b []byte) {
		for i := range h.players {
			if p := &h.players[i]; p.live && i != except {
				send(p.conn, b)
			}
		}
	}

	if q.Kind == JoinGroup {
		idx, ok := h.byConn[conn]
		if !ok || !h.players[idx].live {
			// group passwords, as in o2-server's p5auth.go:
			a := q.Join.Auth
			switch {
			case len(a) != 0 && len(a) != sha256.Size, h.created && h.token == nil && len(a) != 0:
				send(conn, MakeJoinNak(NakAuthUnexpected))
				return
			case h.created && h.token != nil && len(a) == 0:
				send(conn, MakeJoinNak(NakAuthRequired))
				return
			case h.created && h.token != nil && string(a) != string(h.token):
				send(conn, MakeJoinNak(NakAuthFailed))
				return
			case !h.created:
				h.token = append([]byte(nil), a...)
				if len(a) == 0 {
					h.token = nil
				}
			}
			h.created = true
		}
		fresh := !ok || !h.players[idx].live
		if !ok {
			// a session that left keeps its index:
			idx = -1
			for i := range h.players {
				if !h.players[i].live && h.players[i].sessionID == q.Join.SessionID {
					idx = i
					break
				}
			}
			if idx < 0 {
				h.players = append(h.players, hubPlayer{facts: make(map[uint8]*hubFact)})
				idx = len(h.players) - 1
			}
			h.byConn[conn] = idx
		}
		p := &h.players[idx]
		changed := p.name != q.Join.Name || string(p.info) != string(q.Join.Info)
		p.conn, p.live, p.sessionID = conn, true, q.Join.SessionID
		p.name, p.info = q.Join.Name, append([]byte(nil), q.Join.Info...)
		send(conn, MakeJoinAck(q.Join.JoinSeq, uint16(idx), h.Now()))
		if fresh || changed {
			others(idx, MakePlayerInfo([]PlayerInfoEntry{{uint16(idx), p.name, p.info}}))
		}
		if fresh {
			if fl := h.factList(idx); fl != nil {
				others(idx, fl)
			}
			out = append(out, h.roster(conn, nil)...)
		}
		return
	}

	idx, ok := h.byConn[conn]
	if !ok || !h.players[idx].live {
		if q.Kind != LeaveGroup {
			send(conn, MakeJoinNak(NakNotJoined))
		}
		return
	}
	p := &h.players[idx]

	switch q.Kind {
	case LeaveGroup:
		p.live, p.inSector, p.seqSet = false, false, false
		delete(h.byConn, conn)
		others(-1, MakePlayerLeft(uint16(idx), LeftLeft))
	case JoinSector, LeaveSector:
		if !p.seqSet || int8(q.SectorSeq-p.sectorSeq) > 0 {
			p.sectorSeq, p.seqSet = q.SectorSeq, true
			p.inSector, p.sector = q.Kind == JoinSector, q.Sector
		}
		send(conn, MakeSectorAck(q.SectorSeq))
	case Echo:
		send(conn, MakeEchoReply(q.ClientTime, h.Now()))
	case WhoIs:
		out = append(out, h.roster(conn, q.Indexes)...)
	case FactPut:
		f := p.facts[q.FactNum]
		if f != nil && f.complete {
			send(conn, MakeFactStatus(q.FactNum, FactComplete, f.received))
			return
		}
		if q.FactSize < 1 || q.FactSize > MaxFactSize || q.Chunk >= ChunkCount(q.FactSize) ||
			len(q.Data) != ChunkLen(q.FactSize, q.Chunk) || (f != nil && f.size != q.FactSize) {
			send(conn, MakeFactStatus(q.FactNum, FactRejected, 0))
			return
		}
		if f == nil {
			f = &hubFact{size: q.FactSize, data: make([]byte, q.FactSize)}
			p.facts[q.FactNum] = f
		}
		copy(f.data[q.Chunk*FactChunkSize:], q.Data)
		f.received |= 1 << q.Chunk
		if f.received != AllChunks(f.size) {
			send(conn, MakeFactStatus(q.FactNum, FactReceiving, f.received))
			return
		}
		sum := sha256.Sum256(f.data)
		copy(f.hash[:], sum[:])
		f.complete = true
		send(conn, MakeFactStatus(q.FactNum, FactComplete, f.received))
		others(-1, MakeFactList(uint16(idx), []FactEntry{{q.FactNum, f.size, f.hash}}))
	case FactGet:
		for _, g := range q.Gets {
			if int(g.PlayerIndex) >= len(h.players) || !h.players[g.PlayerIndex].live {
				continue
			}
			f := h.players[g.PlayerIndex].facts[g.FactNum]
			if f == nil || !f.complete {
				continue
			}
			for i := 0; i < ChunkCount(f.size); i++ {
				if g.Chunks&(1<<i) != 0 {
					o := i * FactChunkSize
					send(conn, MakeFactData(g.PlayerIndex, g.FactNum, i, f.data[o:o+ChunkLen(f.size, i)]))
				}
			}
		}
	case BroadcastAll, BroadcastSector:
		sector := q.Kind == BroadcastSector
		relay := MakeRelay(uint16(idx), h.Now(), sector, q.Sector, q.Data)
		for i := range h.players {
			o := &h.players[i]
			if !o.live || i == idx || (sector && !(o.inSector && o.sector == q.Sector)) {
				continue
			}
			send(o.conn, relay)
		}
	}
	return
}

func (h *Hub) factList(idx int) []byte {
	var es []FactEntry
	for n := 0; n < 256; n++ {
		if f := h.players[idx].facts[uint8(n)]; f != nil && f.complete {
			es = append(es, FactEntry{uint8(n), f.size, f.hash})
		}
	}
	if es == nil {
		return nil
	}
	return MakeFactList(uint16(idx), es)
}

// roster returns player-info and fact-lists of the requested (nil = all live) players for conn.
func (h *Hub) roster(conn int, indexes []uint16) (out []Delivery) {
	if indexes == nil {
		for i := range h.players {
			if h.players[i].live {
				indexes = append(indexes, uint16(i))
			}
		}
	}
	var es []PlayerInfoEntry
	var lists [][]byte
	for _, i := range indexes {
		if int(i) >= len(h.players) || !h.players[i].live {
			continue
		}
		es = append(es, PlayerInfoEntry{i, h.players[i].name, h.players[i].info})
		if fl := h.factList(int(i)); fl != nil {
			lists = append(lists, fl)
		}
	}
	if es != nil {
		out = append(out, Delivery{conn, MakePlayerInfo(es)})
	}
	for _, l := range lists {
		out = append(out, Delivery{conn, l})
	}
	return
}
