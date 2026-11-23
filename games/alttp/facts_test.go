package alttp

import (
	"bytes"
	"o2/client/protocol05"
	"testing"
	"time"
)

// TestFacts_UploadAndDownload: a player uploads its fact once; another downloads it only when asked for it; after a
// new join the server's copy is kept and only the first chunk is sent again.
func TestFacts_UploadAndDownload(t *testing.T) {
	h := newResetTestHarness(t)
	g0, g1 := h.gs[0].g, h.gs[1].g

	fact := make([]byte, 5000)
	for i := range fact {
		fact[i] = byte(i * 13)
	}
	g0.facts.init()
	g0.facts.built = true
	g0.facts.local[7] = &localFact{data: fact}

	puts := 0
	countPuts := func() {
		for len(h.gs[0].c.Wr) > 0 {
			b := <-h.gs[0].c.Wr
			if q, err := protocol05.DecodeRequest(b); err == nil && q.Kind == protocol05.FactPut {
				puts++
			}
			if err := h.s.HandleMessageFromClient(nil, 0, b); err != nil {
				t.Fatal(err)
			}
		}
	}
	now := time.Now()
	run := func(ticks int) {
		for i := 0; i < ticks; i++ {
			now = now.Add(20 * time.Millisecond)
			for j := range h.gs {
				h.gs[j].g.tickFacts(now)
			}
			countPuts()
			h.exchange()
		}
	}

	run(10)
	if lf := g0.facts.local[7]; !lf.complete {
		t.Fatalf("fact not uploaded: received %b", lf.received)
	}
	if puts != protocol05.ChunkCount(len(fact)) {
		t.Errorf("expected each of the %d chunks sent once; sent %d", protocol05.ChunkCount(len(fact)), puts)
	}
	idx0 := g0.local.Index()
	if _, ok := g1.facts.lists[idx0][7]; !ok {
		t.Fatalf("g1 doesn't know g0's fact")
	}
	if len(g1.facts.downloads) != 0 {
		t.Fatalf("g1 downloads facts nobody asked for")
	}
	// g0 knows its own fact by its hash:
	if e := g0.facts.lists[idx0][7]; !bytes.Equal(g0.facts.store[e.Hash], fact) {
		t.Errorf("g0 doesn't have its own fact under its hash")
	}

	if _, ok := g1.factData(idx0, 7, now); ok {
		t.Fatalf("fact available before it was downloaded")
	}
	run(10)
	data, ok := g1.factData(idx0, 7, now)
	if !ok || !bytes.Equal(data, fact) {
		t.Fatalf("g1 didn't download g0's fact")
	}

	// a new join: only the first chunk goes again, since the server has the fact:
	puts = 0
	g0.factsJoined()
	run(20)
	if puts != 1 || !g0.facts.local[7].complete {
		t.Errorf("expected 1 chunk sent after the new join; got %d (complete %v)", puts, g0.facts.local[7].complete)
	}

	// a fact a player's fact-list doesn't name is asked for with who-is:
	if _, ok := g1.factData(idx0, 9, now); ok {
		t.Errorf("unknown fact available")
	}
}
