package alttp

import (
	"bytes"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"o2/client"
	"o2/client/protocol01"
	"o2/client/protocol02"
	"o2/client/protocol03"
	"time"
)

// server protocol to use:
const protocol = 0x03

type gameMessage interface {
	SendToClient(c *client.Client)
}

type gameBroadcastMessage struct {
	bytes.Buffer

	g *Game
}

func (m *gameBroadcastMessage) SendToClient(c *client.Client) {
	g := m.g

	if protocol == 0x03 {
		p3msg := &protocol03.GroupMessage{
			Group:          string(c.Group()),
			PlayerTime:     time.Now().UnixNano(),
			ServerTime:     0,
			PlayerIndex:    uint32(g.LocalPlayer().IndexF),
			PlayerInSector: uint64(g.LocalPlayer().Location),
		}
		//case protocol02.Broadcast:
		p3msg.BroadcastAll = &protocol03.BroadcastAll{Data: m.Bytes()}
		//case protocol02.BroadcastToSector:
		//	p3msg.BroadcastSector = &protocol03.BroadcastSector{TargetSector: uint64(g.LocalPlayer().Location), Data: m.Bytes()}

		// construct packet:
		pkt := client.MakePacket(0x03)
		b, err := proto.MarshalOptions{}.MarshalAppend(pkt.Bytes(), p3msg)
		if err != nil {
			log.Printf("alttp: send: proto.Marshal: %v\n", err)
			return
		}

		c.Write() <- b
	} else {
		buf := protocol02.MakePacket(
			c.Group(),
			protocol02.Broadcast,
			uint16(g.LocalPlayer().IndexF),
		)
		m.WriteTo(buf)
		c.Write() <- buf.Bytes()
	}
}

type gameJoinMessage struct {
	bytes.Buffer

	g *Game
}

func (m *gameJoinMessage) SendToClient(c *client.Client) {
	g := m.g

	if protocol == 0x03 {
		p3msg := &protocol03.GroupMessage{
			Group:          string(c.Group()),
			PlayerTime:     time.Now().UnixNano(),
			ServerTime:     0,
			PlayerIndex:    uint32(g.LocalPlayer().IndexF),
			PlayerInSector: uint64(g.LocalPlayer().Location),
		}
		p3msg.JoinGroup = &protocol03.JoinGroup{}

		// construct packet:
		pkt := client.MakePacket(0x03)
		b, err := proto.MarshalOptions{}.MarshalAppend(pkt.Bytes(), p3msg)
		if err != nil {
			log.Printf("alttp: send: proto.Marshal: %v\n", err)
			return
		}

		c.Write() <- b
	} else {
		buf := protocol02.MakePacket(
			c.Group(),
			protocol02.RequestIndex,
			uint16(g.LocalPlayer().IndexF),
		)
		_, err := m.WriteTo(buf)
		if err != nil {
			log.Printf("alttp: send: writeTo: %v\n", err)
			return
		}
		c.Write() <- buf.Bytes()
	}
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

func (g *Game) makeJoinMessage() (m *gameJoinMessage) {
	m = &gameJoinMessage{g: g}
	return
}

type gameEchoMessage struct {
	bytes.Buffer

	g *Game
}

func (m *gameEchoMessage) SendToClient(c *client.Client) {
	g := m.g

	if protocol == 0x03 {
		p3msg := &protocol03.GroupMessage{
			Group:          string(c.Group()),
			PlayerTime:     time.Now().UnixNano(),
			ServerTime:     0,
			PlayerIndex:    uint32(g.LocalPlayer().IndexF),
			PlayerInSector: uint64(g.LocalPlayer().Location),
		}
		p3msg.Echo = &protocol03.Echo{Data: m.Bytes()}

		// construct packet:
		pkt := client.MakePacket(0x03)
		b, err := proto.MarshalOptions{}.MarshalAppend(pkt.Bytes(), p3msg)
		if err != nil {
			log.Printf("alttp: send: proto.Marshal: %v\n", err)
			return
		}

		c.Write() <- b
	}
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
	var protocol uint8

	r, err := client.ParseHeader(msg, &protocol)
	if err != nil {
		panic(fmt.Errorf("error parsing message header: %w", err))
	}

	switch protocol {
	// old unused server protocol:
	case 1:
		var header protocol01.Header
		err = protocol01.Parse(r, &header)
		if err != nil {
			panic(fmt.Errorf("error parsing protocol 01 header: %w", err))
		}
		if header.ClientType != 1 {
			return
		}
		return g.Deserialize(r, &g.players[header.Index])

	// current production server protocol:
	case 2:
		var header protocol02.Header
		err = protocol02.Parse(r, &header)
		if err != nil {
			panic(fmt.Errorf("error parsing protocol 02 header: %w", err))
		}

		index := int(header.Index)

		// pre-emptively avoid panics in accessing players array out of bounds:
		if index >= MaxPlayers {
			log.Printf("alttp: player index %v received in packet beyond max player count %v!\n", header.Index, MaxPlayers)
			return
		}

		// reset player Ttl:
		p := &g.players[index]
		p.IndexF = index

		// handle which kind of message it is:
		switch header.Kind & 0x7F {
		case protocol02.RequestIndex:
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
			break

		case protocol02.BroadcastToSector:
			fallthrough
		case protocol02.Broadcast:
			//log.Printf("%s\n", header.Kind.String())
			err = g.Deserialize(r, p)
			break
		default:
			panic(fmt.Errorf("unknown message kind %02x", header.Kind))
		}

		if err != nil {
			log.Printf("alttp: net: deserialize: %v\n", err)
			return
		}

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
	// next production server protocol:
	case 3:
		gm := &protocol03.GroupMessage{}
		var b []byte
		b, err = io.ReadAll(r)
		if err != nil {
			log.Printf("alttp: net: p3: readall: %v\n", err)
			return
		}
		err = proto.Unmarshal(b, gm)
		if err != nil {
			log.Printf("alttp: net: p3: unmarshal: %v\n", err)
			return
		}

		// record server time:
		newServerTime := time.Unix((gm.GetServerTime())/1e9, int64(gm.GetServerTime()%1e9))
		g.lastServerTime = newServerTime
		g.lastServerRecvTime = time.Now()
		//log.Printf("server now(): %v\n", newServerTime.Add(time.Now().Sub(g.lastServerRecvTime)))

		index := int(gm.PlayerIndex)

		// pre-emptively avoid panics in accessing players array out of bounds:
		if index >= MaxPlayers {
			log.Printf("alttp: player index %v received in packet beyond max player count %v!\n", gm.PlayerIndex, MaxPlayers)
			return
		}

		// reset player Ttl:
		p := &g.players[index]
		p.IndexF = index

		// handle which kind of message it is:
		if gm.GetJoinGroup() != nil {
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
		} else if ba := gm.GetBroadcastAll(); ba != nil {
			err = g.Deserialize(bytes.NewReader(ba.Data), p)
		} else if bs := gm.GetBroadcastSector(); bs != nil {
			err = g.Deserialize(bytes.NewReader(bs.Data), p)
		} else if ec := gm.GetEcho(); ec != nil {
			// nothing to do
		}

		if err != nil {
			log.Printf("alttp: net: deserialize: %v\n", err)
			return
		}

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
	default:
		return
	}
}
