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
			w.Timestamp = nowTs
			w.Value = v
		}
	}
}

func (g *Game) doSyncSmallKeys(a *asm.Emitter) (updated bool) {
	// update local copy of small-keys data:
	local := g.local

	for offs, lw := range local.WRAM {
		for _, p := range g.ActivePlayers() {
			rw := p.WRAM[offs]
			if rw.Timestamp <= lw.Timestamp {
				continue
			}

			// Rely on the upcoming memory read to update our local Timestamp:
			//lw.Timestamp = rw.Timestamp
			a.Comment(fmt.Sprintf("update %s small keys", dungeonNammes[offs-0xF37C]))
			a.LDA_imm8_b(uint8(rw.Value))
			a.STA_long(0x7E0000 + uint32(offs))
			updated = true
		}
	}

	return
}
