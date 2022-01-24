package alttp

import (
	"fmt"
	"o2/games"
	"strings"
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

		verb, items := s.LocalCheck(g.wram[:], g.wramLastFrame[:])
		if verb != "" {
			g.PushNotification(fmt.Sprintf("%s %s", verb, strings.Join(items, ", ")))
		}
	}

}
