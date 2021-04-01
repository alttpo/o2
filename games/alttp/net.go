package alttp

import (
	"log"
	"o2/client"
	"o2/client/protocol01"
	"o2/client/protocol02"
)

func (g *Game) handleNetMessage(msg []byte) (err error) {
	var protocol uint8

	r, err := client.ParseHeader(msg, &protocol)
	if err != nil {
		return
	}

	switch protocol {
	// old unused server protocol:
	case 1:
		var header protocol01.Header
		err = protocol01.Parse(r, &header)
		if err != nil {
			return
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
			return
		}

		// pre-emptively avoid panics in accessing players array out of bounds:
		if header.Index >= MaxPlayers {
			log.Printf("player index %v received in packet beyond max player count %v!\n", header.Index, MaxPlayers)
			return
		}

		// reset player TTL:
		g.players[header.Index].TTL = 255
		g.players[header.Index].Index = int(header.Index)

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
			}
			break

		case protocol02.BroadcastToSector:
			fallthrough
		case protocol02.Broadcast:
			return g.players[header.Index].Deserialize(r)
		}
		return
	default:
		return
	}
}
