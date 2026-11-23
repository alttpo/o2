package protocol05

import (
	"bytes"
	"testing"
	"time"
)

func TestJoinRoundTrip(t *testing.T) {
	j := Join{JoinSeq: 3, SessionID: 0xAABBCCDD11223344, Group: "group", Name: "name", Info: []byte{2, 1}}
	b, err := MakeJoin(&j)
	if err != nil {
		t.Fatal(err)
	}
	q, err := DecodeRequest(b)
	if err != nil || q.Kind != JoinGroup || q.Join.JoinSeq != 3 || q.Join.SessionID != j.SessionID ||
		q.Join.Group != "group" || q.Join.Name != "name" || !bytes.Equal(q.Join.Info, j.Info) {
		t.Fatalf("%+v %v", q, err)
	}
	if _, err := DecodeRequest(b[:len(b)-1]); err == nil {
		t.Fatal("truncated join decoded")
	}
}

func TestServerMessages(t *testing.T) {
	now := time.Unix(1700000000, 123000000)
	m, err := Decode(MakeJoinAck(5, 300, now))
	if err != nil || m.JoinSeq != 5 || m.PlayerIndex != 300 || !m.ServerTime.Equal(now) {
		t.Fatalf("join-ack %+v %v", m, err)
	}
	m, err = Decode(MakePlayerInfo([]PlayerInfoEntry{{1, "a", []byte{2}}, {7, "bob", nil}}))
	if err != nil || len(m.Players) != 2 || m.Players[1].PlayerIndex != 7 || m.Players[1].Name != "bob" {
		t.Fatalf("player-info %+v %v", m, err)
	}
	m, err = Decode(MakeFactList(2, []FactEntry{{0, 38528, Hash{1, 2}}}))
	if err != nil || m.PlayerIndex != 2 || len(m.Facts) != 1 || m.Facts[0].Size != 38528 || m.Facts[0].Hash[1] != 2 {
		t.Fatalf("fact-list %+v %v", m, err)
	}
	m, err = Decode(MakeRelay(4, now, true, 0x1234, []byte{9, 8}))
	if err != nil || m.Kind != RelaySector || m.PlayerIndex != 4 || m.Sector != 0x1234 ||
		m.ServerTimeMs != uint16(now.UnixMilli()) || !bytes.Equal(m.Data, []byte{9, 8}) {
		t.Fatalf("relay %+v %v", m, err)
	}
}

func TestExpandServerTime(t *testing.T) {
	now := time.UnixMilli(0x12345678_9ABC)
	for _, d := range []time.Duration{0, -time.Second, -30 * time.Second, 900 * time.Millisecond} {
		at := now.Add(d)
		got := ExpandServerTime(uint16(at.UnixMilli()), now)
		if !got.Equal(time.UnixMilli(at.UnixMilli())) {
			t.Fatalf("%v: got %v want %v", d, got, at)
		}
	}
}

func TestChunks(t *testing.T) {
	if ChunkCount(1) != 1 || ChunkCount(1024) != 1 || ChunkCount(1025) != 2 || ChunkCount(MaxFactSize) != 64 {
		t.Fatal("ChunkCount")
	}
	if AllChunks(MaxFactSize) != ^uint64(0) || AllChunks(2500) != 7 || ChunkLen(2500, 2) != 452 {
		t.Fatal("AllChunks/ChunkLen")
	}
}

func TestHubFacts(t *testing.T) {
	now := time.Unix(1, 0)
	h := NewHub(func() time.Time { return now })
	join := func(conn int, session uint64) {
		b, _ := MakeJoin(&Join{JoinSeq: 1, SessionID: session, Group: "g", Name: "p"})
		h.Handle(conn, b)
	}
	join(10, 1)
	join(20, 2)
	fact := bytes.Repeat([]byte{7}, 1500)
	h.Handle(10, MakeFactPut(0, 1500, 0, fact[:1024]))
	out := h.Handle(10, MakeFactPut(0, 1500, 1, fact[1024:]))
	var list Message
	for _, d := range out {
		if m, _ := Decode(d.Pkt); m.Kind == FactList && d.To == 20 {
			list = m
		}
	}
	if len(list.Facts) != 1 || list.Facts[0].Size != 1500 {
		t.Fatalf("fact-list %+v", list)
	}
	out = h.Handle(20, MakeFactGet([]FactRequest{{0, 0, 3}}))
	if len(out) != 2 {
		t.Fatalf("fact-data %d", len(out))
	}
	m, _ := Decode(out[1].Pkt)
	if m.Chunk != 1 || !bytes.Equal(m.Data, fact[1024:]) {
		t.Fatalf("chunk %+v", m)
	}
}
