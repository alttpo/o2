package alttp

import (
	"log"
	"o2/games"
)

func (g *Game) localChecks() {
	// generate update ASM code for any 8-bit values:
	for offs := g.syncableItemsMin; offs <= g.syncableItemsMax; offs++ {
		var s games.SyncStrategy
		var ok bool
		s, ok = g.syncableItems[offs]
		if !ok {
			continue
		}

		notifications := s.LocalCheck(g.wram[:], g.wramLastFrame[:])
		if notifications == nil {
			continue
		}

		for _, notification := range notifications {
			n := notification.String()
			log.Printf("alttp: %s\n", n)
			g.PushNotification(n)
		}
	}
}
