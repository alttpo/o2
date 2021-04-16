package alttp

import (
	"fmt"
	"o2/snes/asm"
	"time"
)

// initSyncableWRAM called from Reset():
func (g *Game) initSyncableWRAM() {
	local := g.local
	local.WRAM = make(map[uint16]*SyncableWRAM)

	for offs := uint16(0xF37C); offs < 0xF38C; offs++ {
		local.WRAM[offs] = &SyncableWRAM{
			Timestamp: 0,
			Value:     uint16(g.wram[offs]),
		}
	}

	// don't set timestamp on first read:
	g.firstRead = true
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
			if !g.firstRead {
				w.Timestamp = nowTs
			}
			w.Value = v
			w.ValueUsed = v
		}
	}

	if local.Module.IsDungeon() {
		dungeonNumber := g.wram[0x040C]
		if dungeonNumber != 0xFF && dungeonNumber < 0x20 {
			dungeonNumber >>= 1
			dungeonOffs := uint16(0xF37C) + uint16(dungeonNumber)
			currentKeyCount := uint16(g.wram[0xF36F])
			local.WRAM[dungeonOffs].ValueUsed = currentKeyCount
		}
	}

	g.firstRead = false
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

		// Rely on the upcoming memory read to update our local Timestamp:
		if lw.ValueUsed != ww.Value {
			dungeonNumber := offs - 0xF37C
			a.Comment(fmt.Sprintf("update %s small keys from %s", dungeonNammes[dungeonNumber], winner.Name))
			a.LDA_imm8_b(uint8(ww.Value))
			a.STA_long(0x7E0000 + uint32(offs))

			// TODO: could be more efficient about this and do it once at the end
			a.Comment("update current dungeon small keys")
			a.LDY_abs(0x040C)
			a.CPY_imm8_b(uint8(dungeonNumber << 1))
			a.BNE(0x04)
			a.STA_long(0x7EF36F)

			updated = true
		}
	}

	return
}
