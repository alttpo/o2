package alttp

import (
	"crypto/sha256"
	"log"
	"o2/client/protocol05"
	"time"
)

// Facts (see o2-server's PROTOCOL05.md, "Facts")
//
// A fact is immutable data a player uploads once; the server keeps it and hands it to the others. o2 uploads the Link
// graphics fact (fact 0x00, see linkfact.go) after every new join: the first chunk alone, then, unless the server
// answers that it has the fact already, the rest at factPutPerTick chunks per tick, resending what fact-status
// doesn't mark received after factResend. The server's fact-lists say which facts each player has (number, size and
// hash); o2 keeps fact contents by hash, its own included, and downloads a remote player's fact only when something
// asks for it (factData).

const (
	// factPutPerTick is the most chunks sent per tick (once per frame):
	factPutPerTick = 4
	// factResend is how long a chunk sent or asked for waits for its answer before it is sent or asked for again:
	factResend = 250 * time.Millisecond
	// factGetInFlight is the most chunks asked for at once per download:
	factGetInFlight = 16
)

type factsState struct {
	// the local player's facts, by fact number:
	local map[uint8]*localFact
	built bool
	// fact contents by hash, the local player's and downloaded ones:
	store map[protocol05.Hash][]byte
	// each player's fact-list:
	lists [MaxPlayers]map[uint8]protocol05.FactEntry
	// downloads in progress:
	downloads map[factKey]*factDownload
}

type localFact struct {
	data     []byte
	received uint64
	status   bool // a fact-status arrived since the last join
	complete bool
	rejected bool
	sent     [64]time.Time
}

type factKey struct {
	index   int
	factNum uint8
}

type factDownload struct {
	entry protocol05.FactEntry
	data  []byte
	have  uint64
	asked [64]time.Time
}

func (f *factsState) init() {
	if f.store == nil {
		f.store = make(map[protocol05.Hash][]byte)
		f.downloads = make(map[factKey]*factDownload)
		f.local = make(map[uint8]*localFact)
	}
}

// buildLocalFacts builds the local player's facts from the ROM, once.
func (g *Game) buildLocalFacts() {
	f := &g.facts
	f.init()
	if f.built {
		return
	}
	f.built = true
	if fact, err := g.buildLinkFact(); err != nil {
		log.Printf("alttp: facts: no Link graphics fact: %v\n", err)
	} else {
		f.local[factLinkGraphics] = &localFact{data: fact}
	}
}

// factsJoined starts the uploads again after a new join: the server may have lost our facts.
func (g *Game) factsJoined() {
	f := &g.facts
	f.init()
	for _, lf := range f.local {
		*lf = localFact{data: lf.data}
	}
}

// tickFacts sends the chunks of the local facts the server lacks and asks for the chunks of the downloads in progress.
func (g *Game) tickFacts(now time.Time) {
	if g.LocalPlayer().Index() < 0 || !g.net.joinAcked {
		return
	}
	g.buildLocalFacts()
	f := &g.facts

	budget := factPutPerTick
	for n := 0; n < 256 && budget > 0; n++ {
		lf := f.local[uint8(n)]
		if lf == nil || lf.complete || lf.rejected {
			continue
		}
		count := protocol05.ChunkCount(len(lf.data))
		for i := 0; i < count && budget > 0; i++ {
			if lf.received&(1<<i) != 0 || now.Sub(lf.sent[i]) < factResend {
				continue
			}
			if i > 0 && !lf.status {
				// the first chunk alone until the server says whether it has the fact:
				break
			}
			lf.sent[i] = now
			o := i * protocol05.FactChunkSize
			g.sendRaw("fact", protocol05.MakeFactPut(uint8(n), len(lf.data), i, lf.data[o:o+protocol05.ChunkLen(len(lf.data), i)]))
			budget--
		}
	}

	var reqs []protocol05.FactRequest
	for k, d := range f.downloads {
		count := protocol05.ChunkCount(d.entry.Size)
		var ask uint64
		inFlight := 0
		for i := 0; i < count; i++ {
			if d.have&(1<<i) == 0 && now.Sub(d.asked[i]) < factResend {
				inFlight++
			}
		}
		for i := 0; i < count && inFlight < factGetInFlight; i++ {
			if d.have&(1<<i) != 0 || now.Sub(d.asked[i]) < factResend {
				continue
			}
			d.asked[i] = now
			ask |= 1 << i
			inFlight++
		}
		if ask != 0 {
			reqs = append(reqs, protocol05.FactRequest{PlayerIndex: uint16(k.index), FactNum: k.factNum, Chunks: ask})
		}
	}
	for len(reqs) > 0 {
		n := min(len(reqs), protocol05.PackLimit/11)
		g.sendRaw("fact", protocol05.MakeFactGet(reqs[:n]))
		reqs = reqs[n:]
	}
}

// factStatus records the server's answer to our fact-put.
func (g *Game) factStatus(m *protocol05.Message) {
	f := &g.facts
	f.init()
	lf := f.local[m.FactNum]
	if lf == nil {
		return
	}
	lf.status = true
	switch m.FactState {
	case protocol05.FactComplete:
		if !lf.complete {
			log.Printf("alttp: facts: fact %d uploaded\n", m.FactNum)
		}
		lf.complete, lf.received = true, protocol05.AllChunks(len(lf.data))
	case protocol05.FactRejected:
		if !lf.rejected {
			log.Printf("alttp: facts: server rejected fact %d\n", m.FactNum)
		}
		lf.rejected = true
	default:
		lf.received |= m.Received
	}
}

// factList records a player's facts.
func (g *Game) factList(m *protocol05.Message) {
	index := int(m.PlayerIndex)
	if index >= MaxPlayers {
		return
	}
	f := &g.facts
	f.init()
	if f.lists[index] == nil {
		f.lists[index] = make(map[uint8]protocol05.FactEntry)
	}
	for _, e := range m.Facts {
		f.lists[index][e.FactNum] = e
		if index == g.local.Index() {
			// our own fact, now known by its hash:
			if lf := f.local[e.FactNum]; lf != nil && len(lf.data) == e.Size {
				f.store[e.Hash] = lf.data
			}
		}
	}
}

// factChunk adds a chunk of a fact being downloaded.
func (g *Game) factChunk(m *protocol05.Message) {
	f := &g.facts
	f.init()
	k := factKey{int(m.PlayerIndex), m.FactNum}
	d := f.downloads[k]
	if d == nil || int(m.Chunk) >= protocol05.ChunkCount(d.entry.Size) ||
		len(m.Data) != protocol05.ChunkLen(d.entry.Size, int(m.Chunk)) {
		return
	}
	copy(d.data[int(m.Chunk)*protocol05.FactChunkSize:], m.Data)
	d.have |= 1 << m.Chunk
	if d.have != protocol05.AllChunks(d.entry.Size) {
		return
	}
	delete(f.downloads, k)
	sum := sha256.Sum256(d.data)
	if protocol05.Hash(sum[:protocol05.HashSize]) != d.entry.Hash {
		log.Printf("alttp: facts: player[%02x] fact %d doesn't match its hash; dropped\n", uint8(k.index), k.factNum)
		return
	}
	f.store[d.entry.Hash] = d.data
	log.Printf("alttp: facts: player[%02x] fact %d downloaded, %d bytes\n", uint8(k.index), k.factNum, d.entry.Size)
}

// factData returns a remote player's fact if it is here (downloaded, or the same as one we have), else starts
// downloading it; ok is false until then. A fact the player's fact-list doesn't name is asked for with who-is.
func (g *Game) factData(index int, factNum uint8, now time.Time) (data []byte, ok bool) {
	if index < 0 || index >= MaxPlayers {
		return nil, false
	}
	f := &g.facts
	f.init()
	e, listed := f.lists[index][factNum]
	if !listed {
		g.whoIs(index, now)
		return nil, false
	}
	if data, ok = f.store[e.Hash]; ok {
		return
	}
	k := factKey{index, factNum}
	if d := f.downloads[k]; d == nil || d.entry != e {
		f.downloads[k] = &factDownload{entry: e, data: make([]byte, e.Size)}
	}
	return nil, false
}

// factsPlayerLeft forgets a player's facts, and the contents no one else has.
func (g *Game) factsPlayerLeft(index int) {
	f := &g.facts
	f.init()
	f.lists[index] = nil
	for k := range f.downloads {
		if k.index == index {
			delete(f.downloads, k)
		}
	}
	used := map[protocol05.Hash]bool{}
	for i := range f.lists {
		for _, e := range f.lists[i] {
			used[e.Hash] = true
		}
	}
	for h, data := range f.store {
		local := false
		for _, lf := range f.local {
			if len(lf.data) == len(data) && &lf.data[0] == &data[0] {
				local = true
			}
		}
		if !used[h] && !local {
			delete(f.store, h)
		}
	}
}
