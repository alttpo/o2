package alttp

import (
	"fmt"
	"github.com/alttpo/snes/asm"
	"log"
	"time"
)

const smallKeyFirst = uint16(0xF37C)
const smallKeyLast = uint16(0xF38B)

// initSmallKeysSync called from Reset():
func (g *Game) initSmallKeysSync() {
	local := g.local

	for offs := smallKeyFirst; offs <= smallKeyLast; offs++ {
		local.WRAM[offs] = &SyncableWRAM{
			g:         g,
			Offset:    uint32(offs),
			Name:      fmt.Sprintf("%s small keys", dungeonNames[offs-smallKeyFirst]),
			Size:      1,
			Timestamp: 0,
			Value:     uint16(g.wram[offs]),
			Fresh:     &g.wramFresh[offs],
		}
	}
}

func timestampFromTime(t time.Time) uint32 {
	// convert to milliseconds
	return uint32(t.UnixNano() / 1e6)
}

// FixSmallKeys is only invoked from the view model by the player:
func (g *Game) FixSmallKeys() {
	// forget all the small key sync state:
	local := g.local

	g.stateLock.Lock()
	defer g.stateLock.Unlock()

	g.wramFresh[0xF36F] = false
	for offs, lw := range local.WRAM {
		lw.Value = 0
		lw.PreviousValue = 0
		lw.Timestamp = 0
		lw.PreviousTimestamp = 0
		lw.ValueExpected = 0
		lw.PendingTimestamp = 0
		lw.IsWriting = false

		g.wramFresh[offs] = false
	}

	for _, p := range g.RemotePlayers() {
		for _, rw := range p.WRAM {
			rw.Value = 0
			rw.PreviousValue = 0
			rw.Timestamp = 0
			rw.PreviousTimestamp = 0
			rw.ValueExpected = 0
			rw.PendingTimestamp = 0
			rw.IsWriting = false
		}
	}
}

// readWRAM called when WRAM is read from SNES:
func (g *Game) readWRAM() {
	local := g.local

	// don't sample updates when not in game:
	if !local.IsInGame() {
		return
	}

	now := g.ServerSNESTimestamp()
	nowTs := timestampFromTime(now)

	// copy current dungeon small key counter to specific dungeon:
	if g.wramFresh[0xF36F] && local.IsInDungeon() {
		dungeonNumber := local.Dungeon
		if dungeonNumber != 0xFF && dungeonNumber < 0x20 {
			currentKeyCount := g.wram[0xF36F]

			dungeonNumber >>= 1
			dungeonOffs := smallKeyFirst + dungeonNumber

			if g.wram[dungeonOffs] != currentKeyCount {
				log.Printf("alttp: local: wram[$%04x] -> %04x -> wram[$%04x]   ; %s\n", 0xf36f, currentKeyCount, dungeonOffs, dungeonNames[dungeonNumber])

				// copy current dungeon small key counter into the dungeon's small key SRAM shadow location:
				g.wram[dungeonOffs] = currentKeyCount
				g.wramFresh[dungeonOffs] = true

				// sync Sewers and HC dungeons:
				if dungeonOffs == smallKeyFirst {
					g.wram[smallKeyFirst+1] = currentKeyCount
					g.wramFresh[smallKeyFirst+1] = true
				} else if dungeonOffs == smallKeyFirst+1 {
					g.wram[smallKeyFirst] = currentKeyCount
					g.wramFresh[smallKeyFirst] = true
				}
			}
		}
	}

	// read in all WRAM syncables:
	for offs, w := range local.WRAM {
		if w.IsWriting {
			continue
		}
		if !*w.Fresh {
			continue
		}

		var v uint16
		if w.Size == 2 {
			v = g.wramU16(uint32(offs))
		} else {
			v = uint16(g.wramU8(uint32(offs)))
		}

		if v != w.Value {
			w.PreviousValue = w.Value
			w.PreviousTimestamp = w.Timestamp
			if g.notFirstWRAMRead {
				w.Timestamp = nowTs
			}
			w.Value = v
			w.UpdatedFromPlayer = local
			log.Printf("alttp: local: wram[$%04x] -> %04x @ ts=%08x (%v)   ; %s\n", offs, w.Value, w.Timestamp, now.UTC().Format("15:04:05.999999Z"), w.Name)
		}
	}
}

func (g *Game) GenerateSmallKeyUpdate(
	offs uint16,
	newEmitter func() *asm.Emitter,
	index uint32,
) (updated bool, lw *SyncableWRAM, a *asm.Emitter) {
	// update local copy of small-keys data:
	local := g.local

	lw = local.WRAM[offs]

	// don't process a value awaiting a write:
	if lw.IsWriting {
		return
	}

	// don't operate on stale local data:
	if !*lw.Fresh {
		return
	}

	// check for any remote players:
	remotePlayers := g.RemotePlayers()
	if len(remotePlayers) == 0 {
		return
	}

	// find latest timestamp among players:
	noTimestamps := lw.Timestamp == 0
	winnerTs := uint32(0)
	winner := (*Player)(nil)
	for _, p := range remotePlayers {
		rw, ok := p.WRAM[offs]
		if !ok {
			continue
		}

		if rw.Timestamp != 0 {
			noTimestamps = false
		}

		// check if this player has latest timestamp:
		if rw.Timestamp <= winnerTs {
			continue
		}

		winnerTs = rw.Timestamp
		winner = p
	}

	updatingByValue := false

	if noTimestamps {
		// everyone has a 0 timestamp so find who has the max value:
		maxValue := lw.Value
		for _, p := range remotePlayers {
			rw, ok := p.WRAM[offs]
			if !ok {
				continue
			}

			// check if this player has the max value:
			if rw.Value <= maxValue {
				continue
			}

			maxValue = rw.Value
			winner = p
			updatingByValue = true
		}
	}

	// no winner?
	if winner == nil {
		return
	}

	ww := winner.WRAM[offs]

	if updatingByValue {
		// no update needed if values match:
		if lw.Value == ww.Value {
			return
		}
	} else {
		// detect write conflict:
		if lw.PreviousTimestamp < winnerTs && winnerTs < lw.Timestamp {
			// this WOULD have made a change if local hadn't changed first:
			notification := fmt.Sprintf("conflict with %s detected for %s", winner.Name(), lw.Name)
			g.PushNotification(notification)
			log.Printf("alttp: wram[$%04x] %s\n", offs, notification)

			// change local timestamp to match remote winner's so we don't unnecessarily update:
			log.Printf("alttp: wram[$%04x] reverting from ts=%08x to ts=%08x", offs, lw.Timestamp, ww.Timestamp)
			lw.Timestamp = ww.Timestamp
		}

		// didn't write after local:
		if winnerTs <= lw.Timestamp {
			return
		}
	}

	// Force our local timestamp equal to the remote winner to prevent the value bouncing back:
	lw.IsWriting = true
	lw.PendingTimestamp = ww.Timestamp
	lw.ValueExpected = ww.Value
	lw.UpdatedFromPlayer = winner
	log.Printf("alttp: wram[$%04x] <- %02x @ ts=%08x from player '%s'\n", offs, ww.Value, ww.Timestamp, winner.Name())

	dungeonNumber := offs - smallKeyFirst
	a = newEmitter()
	a.Comment(fmt.Sprintf("update %s to %d from %s:", lw.Name, ww.Value, winner.Name()))

	a.LDA_imm8_b(uint8(ww.Value))
	a.LDY_abs(0x040C)
	if offs < smallKeyFirst+2 {
		a.Comment(fmt.Sprintf("check if current dungeon is %02x %s or %02x %s", 0, dungeonNames[0], 2, dungeonNames[1]))
		a.CPY_imm8_b(0x04)
		a.BCS(fmt.Sprintf("cmp%04x", offs))
	} else {
		a.Comment(fmt.Sprintf("check if current dungeon is %02x %s", dungeonNumber<<1, dungeonNames[dungeonNumber]))
		a.CPY_imm8_b(uint8(dungeonNumber << 1))
		a.BNE(fmt.Sprintf("cmp%04x", offs))
	}

	a.STA_long(0x7EF36F)

	a.Label(fmt.Sprintf("cmp%04x", offs))
	a.STA_long(0x7E0000 + uint32(offs))

	a.Comment("sync sewer keys with HC keys:")
	if offs == smallKeyFirst {
		// got new sewer key, update HC:
		a.STA_long(0x7E0000 + uint32(smallKeyFirst+1))
	} else if offs == smallKeyFirst+1 {
		// got new HC key, update sewer:
		a.STA_long(0x7E0000 + uint32(smallKeyFirst))
	}
	a.Label(fmt.Sprintf("end%04x", offs))

	// write confirmation:
	a.LDA_imm8_b(0x01)
	a.STA_long(a.GetBase() + 0x02 + index)

	updated = true

	return
}
