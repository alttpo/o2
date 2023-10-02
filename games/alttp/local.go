package alttp

import (
	"log"
	"o2/games"
)

func (g *Game) localChecks() {
	if !g.notFirstFrame {
		return
	}

	// generate update ASM code for any 8-bit values:
	for offs := g.syncableOffsMin; offs <= g.syncableOffsMax; offs++ {
		var s games.SyncStrategy
		var ok bool
		s, ok = g.syncable[offs]
		if !ok {
			continue
		}

		notifications := s.LocalCheck(g.wram[:], g.wramLastFrame[:])
		if notifications == nil {
			continue
		}

		for _, notification := range notifications {
			n := notification.String()
			log.Printf("alttp: local: %s\n", n)
			g.PushNotification(n)
		}
	}

	for room := uint16(0); room < 0x128; room++ {
		s := &g.underworld[room]

		notifications := s.LocalCheck(g.wram[:], g.wramLastFrame[:])
		if notifications == nil {
			continue
		}

		for _, notification := range notifications {
			n := notification.String()
			log.Printf("alttp: local: %s\n", n)
			g.PushNotification(n)
		}
	}

	for area := uint16(0); area < 0xC0; area++ {
		s := &g.overworld[area]

		notifications := s.LocalCheck(g.wram[:], g.wramLastFrame[:])
		if notifications == nil {
			continue
		}

		for _, notification := range notifications {
			n := notification.String()
			log.Printf("alttp: local: %s\n", n)
			g.PushNotification(n)
		}
	}
}
