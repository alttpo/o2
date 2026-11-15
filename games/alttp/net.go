package alttp

import (
	"bytes"
	"fmt"
	"log"
	"o2/client"
	"o2/client/protocol04"
	"o2/games"
	"time"
)

type gameMessage interface {
	SendToClient(c games.Client)
}

// broadcastSector returns the server sector for a player location. It matches alttpo's `broadcast_sector()` so that o2
// and alttpo players in the same place share a sector: all underworld rooms are in the same sector regardless of the
// light/dark world bit.
func broadcastSector(location uint32) uint64 {
	if location&0x010000 != 0 {
		location &= 0x01FFFF
	}
	return uint64(location)
}

// sendGroupMessage fills in the common GroupMessage fields and sends it as a protocol 04 packet.
func (g *Game) sendGroupMessage(c games.Client, gm *protocol04.GroupMessage) {
	gm.Group = string(c.Group())
	gm.PlayerTime = time.Now().UnixNano()
	gm.ServerTime = 0
	gm.PlayerIndex = uint32(g.LocalPlayer().IndexF)
	gm.PlayerInSector = broadcastSector(g.LocalPlayer().Location)

	b, err := protocol04.MakePacket(gm)
	if err != nil {
		log.Printf("alttp: send: p4: %v\n", err)
		return
	}

	c.Write() <- b
}

// gameBroadcastMessage carries game state broadcast to all other clients in the group, both o2 and alttpo.
type gameBroadcastMessage struct {
	bytes.Buffer

	g *Game
}

func (m *gameBroadcastMessage) SendToClient(c games.Client) {
	m.g.sendGroupMessage(c, &protocol04.GroupMessage{
		Kind: protocol04.BroadcastAll,
		Data: m.Bytes(),
	})
}

type gameJoinMessage struct {
	g *Game
}

func (m *gameJoinMessage) SendToClient(c games.Client) {
	m.g.sendGroupMessage(c, &protocol04.GroupMessage{
		Kind: protocol04.JoinGroup,
	})
}

type gameEchoMessage struct {
	g *Game
}

func (m *gameEchoMessage) SendToClient(c games.Client) {
	m.g.sendGroupMessage(c, &protocol04.GroupMessage{
		Kind: protocol04.Echo,
	})
}

func (g *Game) makeBroadcastMessage() (m *gameBroadcastMessage) {
	m = &gameBroadcastMessage{g: g}

	// script protocol:
	m.WriteByte(SerializationVersion)

	// protocol starts with team number:
	m.WriteByte(g.LocalPlayer().Team)
	// frame number to correlate separate packets together:
	m.WriteByte(g.lastGameFrame)

	return
}

// makeO2BroadcastMessage creates a broadcast packet for o2-only message types (>= 0x80). alttpo stops parsing a packet
// at the first message type it does not understand, so o2-only messages must be kept in their own packets, separate
// from messages alttpo uses.
func (g *Game) makeO2BroadcastMessage() (m *gameBroadcastMessage) {
	return g.makeBroadcastMessage()
}

func (g *Game) makeJoinMessage() (m *gameJoinMessage) {
	m = &gameJoinMessage{g: g}
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

	m.SendToClient(c)
}

func (g *Game) handleNetMessage(msg []byte) (err error) {
	// never let a malformed packet from another client take down the game loop:
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("alttp: net: %v", rec)
			log.Println(err)
		}
	}()

	var protocol uint8

	if _, err = client.ParseHeader(msg, &protocol); err != nil {
		panic(fmt.Errorf("error parsing message header: %w", err))
	}

	if protocol != protocol04.Protocol {
		// ignore other server protocols:
		return
	}

	// the remainder of the message after the 2-byte header and protocol byte:
	body := msg[3:]

	gm := &protocol04.GroupMessage{}
	if err = protocol04.Unmarshal(body, gm); err != nil {
		log.Printf("alttp: net: p4: %v\n", err)
		return
	}

	// record server time; the server stamps every message it sends:
	g.lastServerTime = time.Unix(0, gm.ServerTime)
	g.lastServerRecvTime = time.Now()

	index := int(gm.PlayerIndex)

	// pre-emptively avoid panics in accessing players array out of bounds:
	if index >= MaxPlayers {
		log.Printf("alttp: player index %v received in packet beyond max player count %v!\n", gm.PlayerIndex, MaxPlayers)
		return
	}

	p := &g.players[index]
	p.IndexF = index

	// handle which kind of message it is:
	switch gm.Kind {
	case protocol04.JoinGroup:
		// track local player index:
		if (g.local.Index() < 0) || (g.local.Index() != index) {
			if p != g.local {
				// copy local player data into players array at the appropriate index:
				g.players[index] = *g.local
				// clear out old Player:
				g.local.IndexF = -1
				g.local.Ttl = 0
			}
			// repoint local into the array:
			g.local = p
			g.activePlayersClean = false
			p.IndexF = index
		}
	case protocol04.BroadcastAll, protocol04.BroadcastSector:
		err = g.Deserialize(bytes.NewReader(gm.Data), p)
	case protocol04.Echo:
		// nothing to do
	}

	if err != nil {
		log.Printf("alttp: net: deserialize: %v\n", err)
		return
	}

	// reset player Ttl:
	g.SetTTL(p, 255)

	// wait until we see a name packet to announce:
	if p.showJoinMessage && p.Name() != "" {
		log.Printf("alttp: player[%02x]: %s joined\n", uint8(p.Index()), p.Name())
		g.PushNotification(fmt.Sprintf("%s joined", p.Name()))
		p.showJoinMessage = false
		g.activePlayersClean = false
		g.shouldUpdatePlayersList = true
	}

	if g.shouldUpdatePlayersList {
		g.updatePlayersList()
	}

	return
}
