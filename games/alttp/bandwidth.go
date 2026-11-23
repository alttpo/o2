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

// msgTypeNames names every game message type for the debug page, in the order of their numbers (see serde.go). The
// debug page lists them all, those not sent showing zeros, so that its table keeps its layout.
var msgTypeNames = []struct {
	t    MessageType
	name string
}{
	{MsgLocation, "location"},
	{MsgLocationChanges, "location changes"},
	{MsgSfx, "sfx"},
	{MsgSprites, "sprites"},
	{MsgSpriteChrs, "sprite chrs"},
	{MsgSpritePalettes, "sprite palettes"},
	{MsgWRAM, "wram (crystal switch)"},
	{MsgSRAM, "sram"},
	{MsgAncillae, "ancillae"},
	{MsgTorches, "torches"},
	{MsgPvP, "pvp"},
	{MsgMap16, "map16"},
	{MsgManip, "manip"},
	{MsgStarSwitch, "star switch"},
	{MsgRoomState, "room state"},
	{MsgRoomDoors, "room doors"},
	{MsgOwEvents, "ow events"},
	{MsgFlyingTiles, "flying tiles"},
	{MsgRoomKills, "room kills"},
	{MsgEnemiesKilled, "enemies killed"},
	{MsgRupees, "rupees"},
	{MsgEnemyData, "enemy data (alttpo)"},
	{MsgEnemySegmentData, "enemy segment data (alttpo)"},
	{MsgOverlordData, "overlord data (alttpo)"},
	{MsgSmallKeys, "small keys"},
	{MsgSmallKeysChanges, "small keys changes"},
	{MsgHearts, "hearts"},
	{MsgReset, "reset"},
	{MsgRunTiming, "run timing"},
	{MsgRunRestore, "run restore"},
	{MsgRunGap, "run gap"},
	{MsgSMLocation, "sm location (alttpo)"},
	{MsgSMEvents, "sm events (alttpo)"},
	{MsgSMSRAM, "sm sram (alttpo)"},
	{MsgSMEnemies, "sm enemies (alttpo)"},
}

// packetKinds are the protocol 05 packets o2 sends without game messages (see sendRaw), listed after the game
// messages; "all" and "sector" are broadcasts that carried no game message.
var packetKinds = []string{"join", "sector", "echo", "who-is", "fact", "all"}

// bandwidthOrder is the debug page's row order: every game message type, then the packet kinds.
var bandwidthOrder = func() []string {
	var order []string
	for _, n := range msgTypeNames {
		order = append(order, msgTypeName(n.t))
	}
	return append(order, packetKinds...)
}()

// msgTypeName names a game message type for the debug page.
func msgTypeName(t MessageType) string {
	for _, n := range msgTypeNames {
		if n.t == t {
			return fmt.Sprintf("%02x %s", uint8(t), n.name)
		}
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
		// nothing sent yet, or less than a second since the last call:
		s.mu.Unlock()
		return
	}
	secs := elapsed.Seconds()
	st := bandwidthState{
		Packets:       float64(s.packets) / secs,
		SectorPackets: float64(s.sector) / secs,
		Wire:          float64(s.wire) / secs,
	}
	row := func(name string, c *bandwidthCounter) bandwidthRow {
		r := bandwidthRow{Name: name}
		if c != nil {
			r.Msgs, r.Bytes, r.Wire = float64(c.msgs)/secs, float64(c.payload)/secs, c.wire/secs
		}
		return r
	}
	// every row in a fixed order, so the table keeps its layout; kinds not in the order (unknown message types) last:
	listed := make(map[string]bool, len(bandwidthOrder))
	for _, name := range bandwidthOrder {
		listed[name] = true
		st.Rows = append(st.Rows, row(name, s.kinds[name]))
	}
	var extra []string
	for name := range s.kinds {
		if !listed[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		st.Rows = append(st.Rows, row(name, s.kinds[name]))
	}
	// keep publishing every second, zeros included, once anything was sent:
	s.kinds = make(map[string]*bandwidthCounter)
	s.start = time.Now()
	s.packets, s.sector, s.wire = 0, 0, 0
	s.mu.Unlock()

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
