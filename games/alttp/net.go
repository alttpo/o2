package alttp

import (
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
	case 1:
		var header protocol01.Header
		err = protocol01.Parse(r, &header)
		if err != nil {
			return
		}
		return
	case 2:
		var header protocol02.Header
		err = protocol02.Parse(r, &header)
		if err != nil {
			return
		}

		// track local player index:
		//g.

		kind := header.Kind & 0x7F
		switch kind {
		case protocol02.RequestIndex:
			break

		case protocol02.BroadcastToSector:
			fallthrough
		case protocol02.Broadcast:
			break
		}
		return
	default:
		return
	}
}
