package alttp

import (
	"bytes"
	"fmt"
	"log"
	"o2/client"
	"o2/client/protocol01"
	"o2/client/protocol02"
)

func (g *Game) send(m *bytes.Buffer) {
	c := g.client
	if c == nil {
		return
	}
	if !c.IsConnected() {
		return
	}

	c.Write() <- m.Bytes()
}

func (g *Game) makeGamePacket(kind protocol02.Kind) (m *bytes.Buffer) {
	c := g.client
	if c == nil {
		return
	}

	m = protocol02.MakePacket(
		c.Group(),
		kind,
		uint16(g.local.Index),
	)

	// script protocol:
	m.WriteByte(SerializationVersion)

	// protocol starts with team number:
	m.WriteByte(g.local.Team)
	// frame number to correlate separate packets together:
	m.WriteByte(g.lastGameFrame)

	return
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
		return g.players[header.Index].Deserialize(r)

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

		// reset player TTL:
		p := &g.players[index]
		p.Index = index

		// handle which kind of message it is:
		switch header.Kind & 0x7F {
		case protocol02.RequestIndex:
			// track local player index:
			if (g.local.Index < 0) || (g.local.Index != index) {
				if p != g.local {
					// copy local player data into players array at the appropriate index:
					g.players[index] = *g.local
					// clear out old Player:
					g.local.Index = -1
					g.local.TTL = 0
				}
				// repoint local into the array:
				g.local = p
				g.activePlayersClean = false
				p.Index = index
			}
			break

		case protocol02.BroadcastToSector:
			fallthrough
		case protocol02.Broadcast:
			//log.Printf("%s\n", header.Kind.String())
			err = p.Deserialize(r)
			break
		default:
			panic(fmt.Errorf("unknown message kind %02x", header.Kind))
		}

		if err != nil {
			log.Printf("alttp: net: deserialize: %v\n", err)
			return
		}

		p.SetTTL(255)

		// wait until we see a name packet to announce:
		if p.showJoinMessage && p.Name != "" {
			log.Printf("alttp: player[%02x]: %s joined\n", uint8(p.Index), p.Name)
			p.g.pushNotification(fmt.Sprintf("%s joined", p.Name))
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
