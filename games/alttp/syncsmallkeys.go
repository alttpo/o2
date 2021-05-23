package alttp

import (
	"fmt"
	"log"
	"o2/snes/asm"
	"time"
)

const smallKeyFirst = uint16(0xF37C)
const smallKeyLast = uint16(0xF38B)

// initSmallKeysSync called from Reset():
func (g *Game) initSmallKeysSync() {
	local := g.local

	for offs := smallKeyFirst; offs <= smallKeyLast; offs++ {
		local.WRAM[offs] = &SyncableWRAM{
			Name:      fmt.Sprintf("%s small keys", dungeonNames[offs-smallKeyFirst]),
			Size:      1,
			Timestamp: 0,
			Value:     uint16(g.wram[offs]),
			ValueUsed: uint16(g.wram[offs]),
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
	nowTs := uint32(now.UnixNano() / 1e6) // convert to milliseconds

	// read in all WRAM syncables:
	for offs, w := range local.WRAM {
		var v uint16
		if w.Size == 2 {
			v = g.wramU16(uint32(offs))
		} else {
			v = uint16(g.wramU8(uint32(offs)))
		}

		// if awaiting a write, don't update our value until it matches expected:
		if w.IsWriting {
			if w.ValueExpected == v {
				w.IsWriting = false
				w.Value = v
			}
			continue
		}

		if v != w.Value {
			if g.notFirstWRAMRead {
				w.Timestamp = nowTs
			}
			w.Value = v
			w.ValueUsed = v
			log.Printf("alttp: wram[$%04x] -> %08x (%v), %04x   ; %s\n", offs, w.Timestamp, now.Format(time.RFC3339Nano), w.Value, w.Name)
		}
	}

	// Small Keys:
	if local.IsInDungeon() {
		dungeonNumber := local.Dungeon
		if dungeonNumber != 0xFF && dungeonNumber < 0x20 {
			dungeonNumber >>= 1
			dungeonOffs := smallKeyFirst + dungeonNumber
			currentKeyCount := uint16(g.wram[0xF36F])
			w := local.WRAM[dungeonOffs]
			if currentKeyCount != w.ValueUsed {
				if g.notFirstWRAMRead {
					w.Timestamp = nowTs
				}
				w.ValueUsed = currentKeyCount
				log.Printf("alttp: wram[$%04x] -> %08x (%v), %04x   ; current key counter\n", dungeonOffs, w.Timestamp, now.Format(time.RFC3339Nano), w.ValueUsed)
			}
		}
	}
}

func (g *Game) doSyncSmallKeys(a *asm.Emitter) (updated bool) {
	// update local copy of small-keys data:
	local := g.local

	// compare timestamps amongst players:
	for offs := smallKeyFirst; offs <= smallKeyLast; offs++ {
		lw := local.WRAM[offs]
		// don't process a value awaiting a write:
		if lw.IsWriting {
			continue
		}

		// find latest timestamp among players:
		winner := local
		for _, p := range g.RemotePlayers() {
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

		ww := winner.WRAM[offs]

		// Force our local timestamp equal to the remote winner to prevent the value bouncing back:
		lw.IsWriting = true
		lw.Timestamp = ww.Timestamp
		lw.ValueExpected = ww.Value
		log.Printf("alttp: keys[$%04x] <- %08x, %02x <- player '%s'\n", offs, ww.Timestamp, ww.Value, winner.Name())

		dungeonNumber := offs - smallKeyFirst
		notification := fmt.Sprintf("update %s to %d from %s", lw.Name, ww.Value, winner.Name())
		a.Comment(notification + ":")
		g.PushNotification(notification)
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
