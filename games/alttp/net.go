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
		uint16(g.localIndex),
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

		// pre-emptively avoid panics in accessing players array out of bounds:
		if header.Index >= MaxPlayers {
			log.Printf("player index %v received in packet beyond max player count %v!\n", header.Index, MaxPlayers)
			return
		}

		// reset player TTL:
		g.players[header.Index].SetTTL(255)
		g.players[header.Index].Index = int(header.Index)

		// handle which kind of message it is:
		switch header.Kind & 0x7F {
		case protocol02.RequestIndex:
			// track local player index:
			if (g.localIndex < 0) || (g.localIndex != g.local.Index) {
				g.localIndex = int(header.Index)
				// copy local player data into players array at the appropriate index:
				g.players[g.localIndex] = *g.local
				// clear out old Player:
				g.local.Index = -1
				g.local.TTL = 0
				// repoint local into the array:
				g.local = &g.players[g.localIndex]
				g.local.Index = g.localIndex
				log.Printf("[%02x] %s joined\n", g.localIndex, g.local.Name)
			}
			return

		case protocol02.BroadcastToSector:
			fallthrough
		case protocol02.Broadcast:
			//log.Printf("%s\n", header.Kind.String())
			return g.players[header.Index].Deserialize(r)
		default:
			panic(fmt.Errorf("unknown message kind %02x", header.Kind))
		}
		return
	default:
		return
	}
}
