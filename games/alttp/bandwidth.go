package alttp

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Bandwidth
//
// Every packet o2 sends is counted by the game messages in it (see BANDWIDTH.md), and the debug page shows the rates
// of the last second. A message's share of its packet's fixed cost (the protocol 05 envelope, the 1-byte game header
// and the IPv4 and UDP headers) is in proportion to its size, so the wire rates of all rows add up to the total.
//
// The state messages that don't change every frame are sent when they change and refreshed every few frames, so that
// a lost packet or a player who just arrived catches up (see changeSender).

// udpIPv4Overhead is the size of the IPv4 and UDP headers of every packet:
const udpIPv4Overhead = 28

// bandwidthCounter counts what was sent of one kind of message.
type bandwidthCounter struct {
	msgs    int
	payload int
	wire    float64
}

// bandwidthStats counts what was sent since start.
type bandwidthStats struct {
	mu      sync.Mutex
	start   time.Time
	packets int
	sector  int
	wire    int
	kinds   map[string]*bandwidthCounter
}

// bandwidthRow is a kind of message's rates on the debug page.
type bandwidthRow struct {
	Name string `json:"name"`
	// Msgs is messages per second, Bytes is their size per second and Wire adds their share of the packets' fixed cost:
	Msgs  float64 `json:"msgs"`
	Bytes float64 `json:"bytes"`
	Wire  float64 `json:"wire"`
}

// bandwidthState is the "game/bandwidth" view model: the rates sent in the last second.
type bandwidthState struct {
	Packets       float64        `json:"packets"`
	SectorPackets float64        `json:"sectorPackets"`
	Wire          float64        `json:"wire"`
	Rows          []bandwidthRow `json:"rows"`
}

// msgTypeName names a game message type for the debug page.
func msgTypeName(t MessageType) string {
	names := map[MessageType]string{
		MsgLocation: "location", MsgLocationChanges: "location changes", MsgSfx: "sfx", MsgSprites: "sprites",
		MsgSpriteChrs: "sprite chrs", MsgSpritePalettes: "sprite palettes",
		MsgWRAM: "wram (crystal switch)", MsgSRAM: "sram", MsgAncillae: "ancillae", MsgTorches: "torches",
		MsgPvP: "pvp", MsgMap16: "map16", MsgManip: "manip", MsgStarSwitch: "star switch", MsgRoomState: "room state",
		MsgRoomDoors: "room doors", MsgOwEvents: "ow events", MsgFlyingTiles: "flying tiles",
		MsgRoomKills: "room kills", MsgEnemiesKilled: "enemies killed", MsgRupees: "rupees",
		MsgReset: "reset", MsgSmallKeys: "small keys", MsgSmallKeysChanges: "small keys changes", MsgHearts: "hearts", MsgRunTiming: "run timing",
		MsgRunRestore: "run restore", MsgRunGap: "run gap",
	}
	if n, ok := names[t]; ok {
		return fmt.Sprintf("%02x %s", uint8(t), n)
	}
	return fmt.Sprintf("%02x", uint8(t))
}

// recordSent counts a packet of wire bytes carrying game data (header included) whose messages start at segs. With
// no segs, data is taken as a single message; without data, the packet is counted as kind.
func (g *Game) recordSent(kind string, sector bool, data []byte, segs []int, wire int) {
	s := &g.bandwidth
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.kinds == nil {
		s.kinds = make(map[string]*bandwidthCounter)
		s.start = time.Now()
	}
	s.packets++
	if sector {
		s.sector++
	}
	s.wire += wire

	add := func(name string, payload int, share float64) {
		c := s.kinds[name]
		if c == nil {
			c = &bandwidthCounter{}
			s.kinds[name] = c
		}
		c.msgs++
		c.payload += payload
		c.wire += share
	}

	const header = gameHeaderSize
	if len(data) <= header {
		add(kind, 0, float64(wire))
		return
	}
	if len(segs) == 0 || segs[0] > header {
		// messages written without add or addBytes come first:
		segs = append([]int{header}, segs...)
	}
	total := len(data) - header
	for i, start := range segs {
		end := len(data)
		if i+1 < len(segs) {
			end = segs[i+1]
		}
		n := end - start
		add(msgTypeName(MessageType(data[start])), n, float64(wire)*float64(n)/float64(total))
	}
}

// notifyBandwidth publishes the rates since the last call to the debug page, at most once a second. Called from the
// game loop.
func (g *Game) notifyBandwidth() {
	s := &g.bandwidth
	s.mu.Lock()
	elapsed := time.Since(s.start)
	if s.kinds == nil || elapsed < time.Second {
		s.mu.Unlock()
		return
	}
	secs := elapsed.Seconds()
	st := bandwidthState{
		Packets:       float64(s.packets) / secs,
		SectorPackets: float64(s.sector) / secs,
		Wire:          float64(s.wire) / secs,
	}
	for name, c := range s.kinds {
		st.Rows = append(st.Rows, bandwidthRow{
			Name:  name,
			Msgs:  float64(c.msgs) / secs,
			Bytes: float64(c.payload) / secs,
			Wire:  c.wire / secs,
		})
	}
	s.kinds = nil
	s.packets, s.sector, s.wire = 0, 0, 0
	s.mu.Unlock()

	sort.Slice(st.Rows, func(i, j int) bool { return st.Rows[i].Wire > st.Rows[j].Wire })
	if g.viewModels != nil {
		g.viewModels.NotifyView("game/bandwidth", &st)
	}
}

// changeSender sends a state message when it changes, and again every `every` frames it is sent (and at once when it
// was not sent the previous frame), so that a lost packet or a player who just arrived catches up.
type changeSender struct {
	hash  uint64
	frame uint8
	age   int
	ok    bool
}

// due reports whether the message hashed to h is to be sent this frame, and if so records it as sent.
func (g *Game) changeDue(cs *changeSender, h uint64, every int) bool {
	frame := g.monotonicFrameTime
	cs.age++
	if cs.ok && cs.frame+1 == frame && cs.hash == h && cs.age < every {
		cs.frame = frame
		return false
	}
	cs.frame = frame
	cs.hash, cs.age, cs.ok = h, 0, true
	return true
}

// addOnChange writes the message f serializes to m when it changed or is due for a refresh (see changeSender), and
// reports whether it did.
func (g *Game) addOnChange(m *gameBroadcastMessage, cs *changeSender, every int, f func(w io.Writer) error) bool {
	var b bytes.Buffer
	if err := f(&b); err != nil {
		panic(err)
	}
	if b.Len() == 0 || !g.changeDue(cs, hash64(b.Bytes()), every) {
		return false
	}
	m.addBytes(b.Bytes())
	return true
}
