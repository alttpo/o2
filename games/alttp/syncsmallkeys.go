package alttp

import (
	"fmt"
	"log"
	"o2/snes/asm"
	"time"
)

const smallKeyFirst = uint16(0xF37C)

// initSyncableWRAM called from Reset():
func (g *Game) initSyncableWRAM() {
	local := g.local
	local.WRAM = make(map[uint16]*SyncableWRAM)

	for offs := smallKeyFirst; offs < smallKeyFirst+0x10; offs++ {
		local.WRAM[offs] = &SyncableWRAM{
			Timestamp: 0,
			Value:     uint16(g.wram[offs]),
		}
	}

	// don't set timestamp on first read:
	g.firstKeysRead = true
}

// handleReadWRAM called when WRAM is read from SNES:
func (g *Game) handleReadWRAM() {
	local := g.local

	// don't sample updates when not in game:
	if !local.Module.IsInGame() {
		return
	}

	// TODO: replace this with server timestamp once that's implemented on the server
	now := time.Now()
	nowTs := uint32(now.UnixNano() / 1e6)

	for offs, w := range local.WRAM {
		v := uint16(g.wram[offs])
		if v != w.Value {
			if !g.firstKeysRead {
				w.Timestamp = nowTs
			}
			w.Value = v
			w.ValueUsed = v
			log.Printf("alttp: keys[$%04x] -> %08x, %02x\n", offs, w.Timestamp, w.Value)
		}
	}

	if local.IsInDungeon() {
		dungeonNumber := local.Dungeon
		if dungeonNumber != 0xFF && dungeonNumber < 0x20 {
			dungeonNumber >>= 1
			dungeonOffs := smallKeyFirst + dungeonNumber
			currentKeyCount := uint16(g.wram[0xF36F])
			w := local.WRAM[dungeonOffs]
			if currentKeyCount != w.ValueUsed {
				if !g.firstKeysRead {
					w.Timestamp = nowTs
				}
				w.ValueUsed = currentKeyCount
				log.Printf("alttp: keys[$%04x] -> %08x, %02x ** current key counter\n", dungeonOffs, w.Timestamp, w.ValueUsed)
			}
		}
	}

	g.firstKeysRead = false
}

func (g *Game) doSyncSmallKeys(a *asm.Emitter) (updated bool) {
	// update local copy of small-keys data:
	local := g.local

	// compare timestamps amongst players:
	for offs := range local.WRAM {
		// find latest timestamp among players:
		winner := local
		for _, p := range g.ActivePlayers() {
			rw, ok := p.WRAM[offs]
			if !ok {
				continue
			}

			ww := winner.WRAM[offs]
			if rw.Timestamp <= ww.Timestamp {
				continue
			}

			winner = p
		}

		if winner == local {
			continue
		}

		lw := local.WRAM[offs]
		ww := winner.WRAM[offs]

		// Force our local timestamp equal to the remote winner to prevent the value bouncing back:
		lw.Timestamp = ww.Timestamp
		log.Printf("alttp: keys[$%04x] <- %08x, %02x <- '%s'\n", offs, lw.Timestamp, ww.Value, winner.Name)

		dungeonNumber := offs - smallKeyFirst
		a.Comment(fmt.Sprintf("update %s small keys from %s", dungeonNammes[dungeonNumber], winner.Name))
		a.LDA_imm8_b(uint8(ww.Value))
		a.STA_long(0x7E0000 + uint32(offs))

		a.Comment("update current dungeon small keys")
		a.LDY_abs(0x040C)
		a.CPY_imm8_b(uint8(dungeonNumber << 1))
		a.BNE(0x04)
		a.STA_long(0x7EF36F)

		updated = true
	}

	return
}
