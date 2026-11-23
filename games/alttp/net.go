package alttp

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"o2/client/protocol05"
	"o2/games"
	"time"
)

// Networking: o2-server protocol 0x05 (see o2-server's PROTOCOL05.md and client/protocol05).
//
// The client joins a group with join-group (group, name and the info blob joinInfo) and resends it every netResend
// until the join-ack for the current joinSeq arrives. The server keeps the group and sector the client is in, so
// broadcasts carry neither: the local player's sector is sent with join-sector (or leave-sector when it has none),
// resent until acked. Other players' names and info come in player-info; a relay from a player whose player-info
// hasn't arrived asks for it with who-is. A player that left (player-left) keeps its slot for playerGrace, during
// which late relays from it are dropped.

const (
	// netResend is how often unacked joins and sectors are resent:
	netResend = 100 * time.Millisecond
	// whoIsResend is how often who-is is sent for the same player:
	whoIsResend = 500 * time.Millisecond
	// playerGrace is how long a slot whose player left is reserved; the server reserves it as long:
	playerGrace = 10 * time.Second
	// echoFastSamples is how many echoes are sent every 500 ms after a join, before slowing down to every 2 s:
	echoFastSamples = 30
)

type gameMessage interface {
	SendToClient(c games.Client)
}

// netState is the local client's protocol 0x05 state.
type netState struct {
	sessionID uint64

	// the join last sent, and whether it was acked:
	joinSeq   uint8
	joinSent  time.Time
	joinAcked bool
	sentGroup string
	sentName  string
	sentInfo  string

	// the sector last sent (inSector false: leave-sector), and whether it was acked:
	sectorSeq   uint8
	sectorSet   bool
	inSector    bool
	sector      uint32
	sectorSent  time.Time
	sectorAcked bool

	// echoes received since the last join, and slowbeats since the last echo sent:
	echoes     int
	echoTicks  int
	graceUntil [MaxPlayers]time.Time
	whoIsSent  [MaxPlayers]time.Time
	// what each player's player-info said; kept here rather than in Player since a group reset clears the players:
	info [MaxPlayers]netPlayerInfo
	// the group info is for:
	infoGroup string
}

// netPlayerInfo is what a player's player-info said.
type netPlayerInfo struct {
	known      bool
	compatible bool
	name       string
	team       uint8
}

func newSessionID() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b[:])
}

// broadcastSector returns the server sector for a player location. It matches alttpo's `broadcast_sector()` so that o2
// and alttpo players in the same place share a sector: all underworld rooms are in the same sector regardless of the
// light/dark world bit.
func broadcastSector(location uint32) uint32 {
	if location&0x010000 != 0 {
		location &= 0x01FFFF
	}
	return location
}

// joinInfo is the info blob of the local player's join-group: the game protocol version and the team.
func (g *Game) joinInfo() []byte {
	return []byte{SerializationVersion, g.LocalPlayer().Team}
}

// sendRaw sends a complete packet to the server.
func (g *Game) sendRaw(kind string, pkt []byte) {
	c := g.client
	if c == nil || !c.IsConnected() {
		return
	}
	g.recordSent(kind, false, nil, nil, len(pkt)+udpIPv4Overhead)
	c.Write() <- pkt
}

// tickNet resends the join and the sector until they are acked, and starts a new join when the group, name or team
// changed. Called on the fastbeat and once per frame sent.
func (g *Game) tickNet(now time.Time) {
	c := g.client
	if c == nil || !c.IsConnected() {
		return
	}
	ns := &g.net
	local := g.LocalPlayer()
	group, info := string(c.Group()), string(g.joinInfo())
	if group != ns.sentGroup || local.NameF != ns.sentName || info != ns.sentInfo {
		ns.joinSeq++
		ns.joinAcked = false
		ns.joinSent = time.Time{}
		ns.sentGroup, ns.sentName, ns.sentInfo = group, local.NameF, info
	}
	if group != ns.infoGroup {
		// the players listed as incompatible were in the other group:
		ns.infoGroup = group
		for i := range ns.info {
			if pi := &ns.info[i]; pi.known && !pi.compatible {
				*pi = netPlayerInfo{}
				g.shouldUpdatePlayersList = true
			}
		}
	}
	if !ns.joinAcked && now.Sub(ns.joinSent) >= netResend {
		ns.joinSent = now
		pkt, err := protocol05.MakeJoin(&protocol05.Join{
			JoinSeq:   ns.joinSeq,
			SessionID: ns.sessionID,
			Group:     group,
			Name:      local.NameF,
			Info:      []byte(info),
		})
		if err != nil {
			log.Printf("alttp: join: %v\n", err)
			return
		}
		g.sendRaw("join", pkt)
	}
	g.tickSector(now)
	g.tickFacts(now)
}

// localSector returns the sector the local player is in, if any: none before a game is loaded and while resetting.
func (g *Game) localSector() (uint32, bool) {
	local := g.LocalPlayer()
	if local.Index() < 0 || g.IsResetting() || preGameModule(local.Module) {
		return 0, false
	}
	return broadcastSector(local.Location), true
}

// tickSector sends join-sector or leave-sector when the local player's sector changed, and resends it until acked.
func (g *Game) tickSector(now time.Time) {
	ns := &g.net
	if g.LocalPlayer().Index() < 0 {
		return
	}
	sector, in := g.localSector()
	if !ns.sectorSet || in != ns.inSector || (in && sector != ns.sector) {
		ns.sectorSeq++
		ns.sectorSet, ns.inSector, ns.sector = true, in, sector
		ns.sectorAcked = false
		ns.sectorSent = time.Time{}
	}
	if ns.sectorAcked || now.Sub(ns.sectorSent) < netResend {
		return
	}
	ns.sectorSent = now
	if ns.inSector {
		g.sendRaw("sector", protocol05.MakeJoinSector(ns.sectorSeq, ns.sector))
	} else {
		g.sendRaw("sector", protocol05.MakeLeaveSector(ns.sectorSeq))
	}
}

// netDisconnected forgets the join and the sector, so that they are sent again on the next connection.
func (g *Game) netDisconnected() {
	ns := &g.net
	ns.joinAcked, ns.joinSent = false, time.Time{}
	ns.sentGroup, ns.sentName, ns.sentInfo = "", "", ""
	ns.sectorSet, ns.sectorAcked = false, false
}

// sendEcho sends an echo to measure the round trip and the server's clock: every 500 ms (slowbeat) for the first
// echoFastSamples after a join, then every 2 s.
func (g *Game) sendEcho() {
	ns := &g.net
	ns.echoTicks++
	if ns.echoes >= echoFastSamples && ns.echoTicks < 4 {
		return
	}
	ns.echoTicks = 0
	g.lastServerSentTime = time.Now()
	g.sendRaw("echo", protocol05.MakeEcho(time.Now().UnixNano()))
}

// sendBroadcast sends a game packet to all players, or to those in sector. segs are where the game messages in data
// start, for the bandwidth counters (see bandwidth.go).
func (g *Game) sendBroadcast(c games.Client, sector bool, target uint32, data []byte, kind string, segs []int) {
	if g.LocalPlayer().Index() < 0 {
		// the server drops broadcasts from clients it hasn't joined:
		return
	}
	var pkt []byte
	if sector {
		pkt = protocol05.MakeBroadcastSector(target, data)
	} else {
		pkt = protocol05.MakeBroadcastAll(data)
	}
	g.recordSent(kind, sector, data, segs, len(pkt)+udpIPv4Overhead)
	c.Write() <- pkt
}

// gameBroadcastMessage carries game state broadcast to all other clients in the group, both o2 and alttpo.
type gameBroadcastMessage struct {
	bytes.Buffer

	g *Game
	// segs are where the messages added with add or addBytes start:
	segs []int
}

// gameHeaderSize is the size of the game packet header: the frame number.
const gameHeaderSize = 1

func (m *gameBroadcastMessage) SendToClient(c games.Client) {
	m.g.sendBroadcast(c, false, 0, m.Bytes(), "all", m.segs)
}

// add writes a game message with f (its type byte, then its body), inserting its length after the type and recording
// where it starts for the bandwidth counters.
func (m *gameBroadcastMessage) add(f func(w io.Writer) error) error {
	start := m.Len()
	if err := f(m); err != nil {
		m.Truncate(start)
		return err
	}
	if m.Len() == start {
		return nil
	}
	msg := append([]byte(nil), m.Bytes()[start:]...)
	if len(msg)-1 > maxMsgLen {
		m.Truncate(start)
		return fmt.Errorf("alttp: message %#02x is %d bytes, more than %d", msg[0], len(msg)-1, maxMsgLen)
	}
	m.Truncate(start)
	m.Write(frameMessage(msg))
	m.segs = append(m.segs, start)
	return nil
}

// addBytes writes a serialized game message (its type byte, then its body), inserting its length after the type and
// recording where it starts for the bandwidth counters.
func (m *gameBroadcastMessage) addBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	_ = m.add(func(w io.Writer) error {
		_, err := w.Write(b)
		return err
	})
}

// empty reports whether no game message was written after the header.
func (m *gameBroadcastMessage) empty() bool {
	return m.Len() <= gameHeaderSize
}

// gameSectorMessage carries game state broadcast only to the clients in the same sector as the local player (see
// broadcastSector), both o2 and alttpo.
type gameSectorMessage struct {
	gameBroadcastMessage
	// sector is the sector to send to, if not the local player's:
	sector    uint32
	hasSector bool
}

func (m *gameSectorMessage) SendToClient(c games.Client) {
	m.g.sendBroadcast(c, true, m.targetSector(), m.Bytes(), "sector", m.segs)
}

func (m *gameSectorMessage) targetSector() uint32 {
	if m.hasSector {
		return m.sector
	}
	return broadcastSector(m.g.LocalPlayer().Location)
}

func (g *Game) makeBroadcastMessage() (m *gameBroadcastMessage) {
	m = &gameBroadcastMessage{g: g}
	// frame number to correlate separate packets together:
	m.WriteByte(g.lastGameFrame)
	return
}

// makeSectorMessage creates a packet sent only to the clients in the local player's sector (e.g. the same room).
func (g *Game) makeSectorMessage() (m *gameSectorMessage) {
	m = &gameSectorMessage{}
	m.g = g
	m.WriteByte(g.lastGameFrame)
	return
}

// makeSectorMessageTo creates a packet sent to the clients in the sector of location, e.g. to the players of a room
// the local player left.
func (g *Game) makeSectorMessageTo(location uint32) (m *gameSectorMessage) {
	m = g.makeSectorMessage()
	m.sector, m.hasSector = broadcastSector(location), true
	return
}

func (g *Game) send(m gameMessage) {
	c := g.client
	if c == nil {
		return
	}
	if !c.IsConnected() {
		return
	}

	if g.batch != nil && g.batch.add(g, m) {
		return
	}
	m.SendToClient(c)
}

// maxGamePacket is the most game data merged into one packet; MaxPacketSize less the p5 envelope, with room to spare:
const maxGamePacket = 1400

// frameBatch merges the messages a frame sends to all players, and to the local player's own sector, into as few
// packets as fit maxGamePacket. Messages to another sector (makeSectorMessageTo) go in packets of their own.
type frameBatch struct {
	all    *gameBroadcastMessage
	sector *gameSectorMessage
}

// add merges m into the batch, sending the batch's packet first if m doesn't fit; false if m can't be merged.
func (b *frameBatch) add(g *Game, m gameMessage) bool {
	switch m := m.(type) {
	case *gameBroadcastMessage:
		if m.Len() > maxGamePacket {
			return false
		}
		if b.all != nil && b.all.Len()+m.Len()-gameHeaderSize > maxGamePacket {
			b.all.SendToClient(g.client)
			b.all = nil
		}
		if b.all == nil {
			b.all = g.makeBroadcastMessage()
		}
		b.all.merge(m)
		return true
	case *gameSectorMessage:
		if m.hasSector || m.Len() > maxGamePacket {
			return false
		}
		if b.sector != nil && b.sector.Len()+m.Len()-gameHeaderSize > maxGamePacket {
			b.sector.SendToClient(g.client)
			b.sector = nil
		}
		if b.sector == nil {
			b.sector = g.makeSectorMessage()
		}
		b.sector.merge(&m.gameBroadcastMessage)
		return true
	}
	return false
}

// flushBatch sends the frame's packets and ends the batch.
func (g *Game) flushBatch() {
	b := g.batch
	g.batch = nil
	if b == nil || g.client == nil || !g.client.IsConnected() {
		return
	}
	if b.all != nil && !b.all.empty() {
		b.all.SendToClient(g.client)
	}
	if b.sector != nil && !b.sector.empty() {
		b.sector.SendToClient(g.client)
	}
}

// merge appends the messages of o (without its header) to m.
func (m *gameBroadcastMessage) merge(o *gameBroadcastMessage) {
	base := m.Len() - gameHeaderSize
	for _, s := range o.segs {
		m.segs = append(m.segs, s+base)
	}
	m.Write(o.Bytes()[gameHeaderSize:])
}

// assignLocalIndex moves the local player to the index the server gave it.
func (g *Game) assignLocalIndex(index int) {
	p := &g.players[index]
	if g.local.Index() == index && g.local == p {
		return
	}
	if p != g.local {
		// copy local player data into players array at the appropriate index:
		g.players[index] = *g.local
		// clear out old Player:
		g.local.IndexF = -1
		g.local.Ttl = 0
		// the run moved with the local player; don't count it twice:
		g.local.GameStartTime, g.local.GameFinishTime = time.Time{}, time.Time{}
		g.local.RunTiming = runTimingRemote{}
	}
	// repoint local into the array:
	g.local = p
	g.activePlayersClean = false
	p.IndexF = index
}

func (g *Game) handleNetMessage(msg []byte) (err error) {
	// never let a malformed packet from another client take down the game loop:
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("alttp: net: %v", rec)
			log.Println(err)
		}
	}()

	m, err := protocol05.Decode(msg)
	if err != nil {
		// other protocols and malformed packets are ignored:
		return nil
	}
	now := time.Now()
	ns := &g.net

	switch m.Kind {
	case protocol05.JoinAck:
		if m.JoinSeq != ns.joinSeq {
			return
		}
		if int(m.PlayerIndex) >= MaxPlayers {
			log.Printf("alttp: server gave us index %d; this o2 version holds %d players\n", m.PlayerIndex, MaxPlayers)
			g.PushNotification(fmt.Sprintf("group too large for this o2 version (index %d)", m.PlayerIndex))
			return
		}
		wasJoined := g.local.Index() >= 0 && ns.joinAcked
		ns.joinAcked = true
		g.lastServerTime, g.lastServerRecvTime = m.ServerTime, now
		if !wasJoined {
			ns.echoes = 0
			ns.sectorSet, ns.sectorAcked = false, false
			g.factsJoined()
		}
		g.assignLocalIndex(int(m.PlayerIndex))
		// back in the group after losing the server:
		g.noteServerBack()
		g.tickSector(now)
	case protocol05.JoinNak:
		if m.Reason == protocol05.NakGroupFull {
			log.Println("alttp: server: group full")
		}
		if ns.joinAcked {
			// the server lost us (it restarted, or we were away too long); join again:
			log.Println("alttp: server: not in a group; joining again")
			g.noteServerLost(true)
			ns.joinAcked, ns.joinSent = false, time.Time{}
		}
	case protocol05.SectorAck:
		if m.SectorSeq == ns.sectorSeq {
			ns.sectorAcked = true
		}
	case protocol05.EchoReply:
		ns.echoes++
		g.lastServerTime, g.lastServerRecvTime = m.ServerTime, now
		g.clock.echo(m.ClientTime, m.ServerTime.UnixNano(), now)
	case protocol05.PlayerInfo:
		for _, e := range m.Players {
			g.playerInfo(e, now)
		}
	case protocol05.PlayerLeft:
		g.playerLeft(int(m.PlayerIndex), now)
	case protocol05.FactStatus:
		g.factStatus(&m)
	case protocol05.FactList:
		g.factList(&m)
	case protocol05.FactData:
		g.factChunk(&m)
	case protocol05.RelayAll, protocol05.RelaySector:
		index := int(m.PlayerIndex)
		if index >= MaxPlayers || index == g.local.Index() || now.Before(ns.graceUntil[index]) {
			return
		}
		if m.Kind == protocol05.RelaySector {
			// the server routed it by the sector it last heard us join; drop it if we moved since:
			if sector, in := g.localSector(); !in || sector != m.Sector {
				return
			}
		}
		pi := &ns.info[index]
		if !pi.known {
			g.whoIs(index, now)
			return
		}
		if !pi.compatible {
			return
		}
		p := &g.players[index]
		p.IndexF = index
		g.applyPlayerInfo(p, pi)
		stamp := protocol05.ExpandServerTime(m.ServerTimeMs, g.ServerNow())
		p.lastServerStamp = stamp
		g.lastServerTime, g.lastServerRecvTime = stamp, now
		if err = g.Deserialize(bytes.NewReader(m.Data), p); err != nil {
			log.Printf("alttp: net: deserialize: %v\n", err)
		}
		// reset player Ttl:
		g.SetTTL(p, 255)
		g.announceJoined(p)
	}

	if g.shouldUpdatePlayersList {
		g.updatePlayersList()
	}

	return
}

// whoIs asks the server for a player's player-info, at most every whoIsResend.
func (g *Game) whoIs(index int, now time.Time) {
	ns := &g.net
	if now.Sub(ns.whoIsSent[index]) < whoIsResend {
		return
	}
	ns.whoIsSent[index] = now
	g.sendRaw("who-is", protocol05.MakeWhoIs([]uint16{uint16(index)}))
}

// playerInfo records a player's name, team and game protocol version.
func (g *Game) playerInfo(e protocol05.PlayerInfoEntry, now time.Time) {
	index := int(e.PlayerIndex)
	if index >= MaxPlayers || index == g.local.Index() {
		return
	}
	ns := &g.net
	// a player-info during the grace period is the same player coming back:
	ns.graceUntil[index] = time.Time{}

	pi := &ns.info[index]
	compatible := len(e.Info) >= 1 && e.Info[0] == SerializationVersion
	if compatible != pi.compatible && !compatible {
		log.Printf("alttp: player[%02x]: %s runs an incompatible version\n", uint8(index), e.Name)
		g.PushNotification(fmt.Sprintf("%s runs an incompatible version", e.Name))
	}
	if !compatible && (!pi.known || pi.compatible || pi.name != e.Name) {
		// listed as incompatible:
		g.shouldUpdatePlayersList = true
	}
	pi.known, pi.compatible, pi.name = true, compatible, e.Name
	if len(e.Info) >= 2 {
		pi.team = e.Info[1]
	}

	p := &g.players[index]
	p.IndexF = index
	g.applyPlayerInfo(p, pi)
	if compatible {
		g.SetTTL(p, 255)
		g.announceJoined(p)
	}
}

// applyPlayerInfo sets a player's name and team from its player-info.
func (g *Game) applyPlayerInfo(p *Player, pi *netPlayerInfo) {
	if p.NameF != pi.name {
		p.NameF = pi.name
		p.showJoinMessage = true
		g.shouldUpdatePlayersList = true
	}
	if p.Team != pi.team {
		p.Team = pi.team
		g.shouldUpdatePlayersList = true
	}
}

// announceJoined announces a player once its name is known.
func (g *Game) announceJoined(p *Player) {
	if p.showJoinMessage && p.Name() != "" && p.Ttl > 0 {
		log.Printf("alttp: player[%02x]: %s joined\n", uint8(p.Index()), p.Name())
		g.PushNotification(fmt.Sprintf("%s joined", p.Name()))
		p.showJoinMessage = false
		g.activePlayersClean = false
		g.shouldUpdatePlayersList = true
	}
}

// playerLeft clears a player that left the group and reserves its slot for the grace period.
func (g *Game) playerLeft(index int, now time.Time) {
	if index >= MaxPlayers || index == g.local.Index() {
		return
	}
	g.net.graceUntil[index] = now.Add(playerGrace)
	if pi := &g.net.info[index]; pi.known && !pi.compatible {
		// listed as incompatible:
		g.shouldUpdatePlayersList = true
	}
	g.net.info[index] = netPlayerInfo{}
	g.factsPlayerLeft(index)
	p := &g.players[index]
	if p.Ttl > 0 {
		g.PlayerLeft(p)
	}
}
