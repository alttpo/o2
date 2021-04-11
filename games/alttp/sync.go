package alttp

import (
	"encoding/binary"
	"o2/snes/asm"
)

type SyncableItem interface {
	// Offset offset from $7EF000
	Offset() uint16
	// Size size in bytes of value handled (1 or 2)
	Size() uint
	// IsEnabled whether sync is enabled for this item:
	IsEnabled() bool
	// GenerateUpdate generates a 65816 asm routine to update WRAM if applicable
	// returns true if program was generated, false if asm was not modified
	GenerateUpdate(asm *asm.Emitter) bool
}

func (g *Game) initSync() {
	// reset map:
	g.syncableItems = make(map[uint16]SyncableItem)

	// define syncable items:
	g.newSyncableMaxU8(0x340, &g.SyncItems, []string{"Bow", "Bow", "Silver Bow", "Silver Bow"})
	g.newSyncableMaxU8(0x341, &g.SyncItems, []string{"Blue Boomerang", "Red Boomerang"})
	g.newSyncableMaxU8(0x342, &g.SyncItems, []string{"Hookshot"})
	// skip 0x343 bomb count
	g.newSyncableMaxU8(0x344, &g.SyncItems, []string{"Mushroom", "Magic Powder"})
	g.newSyncableMaxU8(0x345, &g.SyncItems, []string{"Fire Rod"})
	g.newSyncableMaxU8(0x346, &g.SyncItems, []string{"Ice Rod"})
	g.newSyncableMaxU8(0x347, &g.SyncItems, []string{"Bombos Medallion"})
	g.newSyncableMaxU8(0x348, &g.SyncItems, []string{"Ether Medallion"})
	g.newSyncableMaxU8(0x349, &g.SyncItems, []string{"Quake Medallion"})
	g.newSyncableMaxU8(0x34A, &g.SyncItems, []string{"Lamp"})
	g.newSyncableMaxU8(0x34B, &g.SyncItems, []string{"Hammer"})
	g.newSyncableMaxU8(0x34C, &g.SyncItems, []string{"Shovel", "Flute", "Flute (activated)"})
	g.newSyncableMaxU8(0x34D, &g.SyncItems, []string{"Bug Catching Net"})
	g.newSyncableMaxU8(0x34E, &g.SyncItems, []string{"Book of Mudora"})
	// skip 0x34F current bottle selection
	g.newSyncableMaxU8(0x350, &g.SyncItems, []string{"Cane of Somaria"})
	g.newSyncableMaxU8(0x351, &g.SyncItems, []string{"Cane of Byrna"})
	g.newSyncableMaxU8(0x352, &g.SyncItems, []string{"Magic Cape"})
	g.newSyncableMaxU8(0x353, &g.SyncItems, []string{"Magic Scroll", "Magic Mirror"})
	g.newSyncableMaxU8(0x354, &g.SyncItems, []string{"Power Gloves", "Titan's Mitts"}).onUpdated = func(asm *asm.Emitter) {
		asm.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
	}
	g.newSyncableMaxU8(0x355, &g.SyncItems, []string{"Pegasus Boots"})
	g.newSyncableMaxU8(0x356, &g.SyncItems, []string{"Flippers"})
	g.newSyncableMaxU8(0x357, &g.SyncItems, []string{"Moon Pearl"})
	// skip 0x358 unused
	g.newSyncableMaxU8(0x359, &g.SyncItems, []string{"Fighter Sword", "Master Sword", "Tempered Sword", "Golden Sword"}).onUpdated = func(asm *asm.Emitter) {
		asm.JSL(g.romFunctions[fnDecompGfxSword])
		asm.JSL(g.romFunctions[fnUpdatePaletteSword])
	}
	g.newSyncableMaxU8(0x35A, &g.SyncItems, []string{"Blue Shield", "Red Shield", "Mirror Shield"}).onUpdated = func(asm *asm.Emitter) {
		asm.JSL(g.romFunctions[fnDecompGfxShield])
		asm.JSL(g.romFunctions[fnUpdatePaletteShield])
	}
	g.newSyncableMaxU8(0x35B, &g.SyncItems, []string{"Blue Mail", "Red Mail"}).onUpdated = func(asm *asm.Emitter) {
		asm.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
	}
}

func Max(values []uint16) uint16 {
	maxV := uint16(0)
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
	}
	return maxV
}

type syncableMaxU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	onUpdated func(asm *asm.Emitter)
}

func (g *Game) newSyncableMaxU8(offset uint16, enabled *bool, names []string) *syncableMaxU8 {
	s := &syncableMaxU8{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
		names:     names,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableMaxU8) Offset() uint16  { return s.offset }
func (s *syncableMaxU8) Size() uint      { return 1 }
func (s *syncableMaxU8) IsEnabled() bool { return *s.isEnabled }

func (s *syncableMaxU8) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local
	offset := s.offset

	maxP := local
	maxV := local.SRAM[offset]
	initial := maxV
	for _, p := range g.ActivePlayers() {
		v := p.SRAM[offset]
		if v > maxV {
			maxV, maxP = v, p
		}
	}

	if maxV == initial {
		// no change:
		return false
	}

	// notify local player of new item received:
	_ = maxP
	//g.notifyNewItem(s.names[v])

	asm.LDA_imm8_b(maxV)
	asm.STA_long(0x7EF000 + uint32(offset))

	if s.onUpdated != nil {
		s.onUpdated(asm)
	}

	return true
}

type syncableMaxU16 struct {
	g *Game

	offset    uint16
	isEnabled *bool
}

func (g *Game) newSyncableMaxU16(offset uint16, enabled *bool) {
	g.syncableItems[offset] = &syncableMaxU16{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
	}
}

func (s *syncableMaxU16) Offset() uint16  { return s.offset }
func (s *syncableMaxU16) Size() uint      { return 2 }
func (s *syncableMaxU16) IsEnabled() bool { return *s.isEnabled }

func (s *syncableMaxU16) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local
	offset := s.offset

	maxP := local
	maxV := binary.LittleEndian.Uint16(local.SRAM[offset : offset+2])
	initial := maxV
	for _, p := range g.ActivePlayers() {
		v := binary.LittleEndian.Uint16(p.SRAM[offset : offset+2])
		if v > maxV {
			maxV, maxP = v, p
		}
	}

	if maxV == initial {
		// no change:
		return false
	}

	// notify local player of new item received:
	_ = maxP
	//g.notifyNewItem(s.names[v])

	asm.LDA_imm16_w(maxV)
	asm.STA_long(0x7EF000 + uint32(offset))

	return true
}
