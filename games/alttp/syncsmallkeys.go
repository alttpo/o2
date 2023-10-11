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
			Name:      fmt.Sprintf("%s small keys", dungeonNames[offs-smallKeyFirst]),
			Size:      1,
			Timestamp: 0,
			Value:     uint16(g.wram[offs]),
		}
	}
}

func timestampFromTime(t time.Time) uint32 {
	// convert to milliseconds
	return uint32(t.UnixNano() / 1e6)
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
	if local.IsInDungeon() {
		dungeonNumber := local.Dungeon
		if dungeonNumber != 0xFF && dungeonNumber < 0x20 {
			currentKeyCount := g.wram[0xF36F]

			dungeonNumber >>= 1
			dungeonOffs := smallKeyFirst + dungeonNumber

			// copy current dungeon small key counter into the dungeon's small key SRAM shadow location:
			g.wram[dungeonOffs] = currentKeyCount

			// sync Sewers and HC dungeons:
			if dungeonOffs == smallKeyFirst {
				g.wram[smallKeyFirst+1] = currentKeyCount
			} else if dungeonOffs == smallKeyFirst+1 {
				g.wram[smallKeyFirst] = currentKeyCount
			}
		}
	}

	// read in all WRAM syncables:
	for offs, w := range local.WRAM {
		if w.IsWriting {
			continue
		}

		var v uint16
		if w.Size == 2 {
			v = g.wramU16(uint32(offs))
		} else {
			v = uint16(g.wramU8(uint32(offs)))
		}

		if v != w.Value {
			if g.notFirstWRAMRead {
				w.Timestamp = nowTs
			}
			w.Value = v
			log.Printf("alttp: wram[$%04x] -> %04x @ %08x (%v)   ; %s\n", offs, w.Value, w.Timestamp, now.UTC().Format("15:04:05.999999999Z"), w.Name)
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

		// record ourselves as requiring ASM exec confirmation:
		index := uint32(len(g.updateGenerators))
		g.updateGenerators = append(g.updateGenerators, lw)

		// Force our local timestamp equal to the remote winner to prevent the value bouncing back:
		lw.IsWriting = true
		lw.PendingTimestamp = ww.Timestamp
		lw.ValueExpected = ww.Value
		log.Printf("alttp: keys[$%04x] <- %08x, %02x <- player '%s'\n", offs, ww.Timestamp, ww.Value, winner.Name())

		dungeonNumber := offs - smallKeyFirst
		notification := fmt.Sprintf("update %s to %d from %s", lw.Name, ww.Value, winner.Name())
		a.Comment(notification + ":")
		g.PushNotification(notification)

		a.LDA_imm8_b(uint8(ww.Value))
		a.Comment(fmt.Sprintf("check if current dungeon is %02x %s", dungeonNumber<<1, dungeonNames[dungeonNumber]))
		a.LDY_abs(0x040C)
		if offs < smallKeyFirst+2 {
			a.CPY_imm8_b(0x04)
			a.BCS(fmt.Sprintf("cmp%04x", offs))
		} else {
			a.CPY_imm8_b(uint8(dungeonNumber << 1))
			a.BNE(fmt.Sprintf("cmp%04x", offs))
		}

		a.STA_long(0x7EF36F)
		a.BRA(fmt.Sprintf("end%04x", offs))

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
	}

	return
}
