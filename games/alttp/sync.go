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
	g.newSyncableMaxU8(0x354, &g.SyncItems, []string{"Power Gloves", "Titan's Mitts"}).onUpdated = func(asm *asm.Emitter, initial, updated uint8) {
		asm.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
	}
	g.newSyncableMaxU8(0x355, &g.SyncItems, []string{"Pegasus Boots"})
	g.newSyncableMaxU8(0x356, &g.SyncItems, []string{"Flippers"})
	g.newSyncableMaxU8(0x357, &g.SyncItems, []string{"Moon Pearl"})
	// skip 0x358 unused
	g.newSyncableMaxU8(0x359, &g.SyncItems, []string{"Fighter Sword", "Master Sword", "Tempered Sword", "Golden Sword"}).onUpdated = func(asm *asm.Emitter, initial, updated uint8) {
		asm.JSL(g.romFunctions[fnDecompGfxSword])
		asm.JSL(g.romFunctions[fnUpdatePaletteSword])
	}
	g.newSyncableMaxU8(0x35A, &g.SyncItems, []string{"Blue Shield", "Red Shield", "Mirror Shield"}).onUpdated = func(asm *asm.Emitter, initial, updated uint8) {
		asm.JSL(g.romFunctions[fnDecompGfxShield])
		asm.JSL(g.romFunctions[fnUpdatePaletteShield])
	}
	g.newSyncableMaxU8(0x35B, &g.SyncItems, []string{"Blue Mail", "Red Mail"}).onUpdated = func(asm *asm.Emitter, initial, updated uint8) {
		asm.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
	}

	bottleItemNames := []string{"", "Empty Bottle", "Red Potion", "Green Potion", "Blue Potion", "Fairy", "Bee", "Good Bee"}
	g.newSyncableBottle(0x35C, &g.SyncItems, bottleItemNames)
	g.newSyncableBottle(0x35D, &g.SyncItems, bottleItemNames)
	g.newSyncableBottle(0x35E, &g.SyncItems, bottleItemNames)
	g.newSyncableBottle(0x35F, &g.SyncItems, bottleItemNames)

	// dungeon items:
	g.newSyncableBitU8(0x364, &g.SyncDungeonItems, []string{
		"",
		"",
		"Ganon's Tower Compass",
		"Turtle Rock Compass",
		"Thieves Town Compass",
		"Tower of Hera Compass",
		"Ice Palace Compass",
		"Skull Woods Compass"})
	g.newSyncableBitU8(0x365, &g.SyncDungeonItems, []string{
		"Misery Mire Compass",
		"Dark Palace Compass",
		"Swamp Palace Compass",
		"Hyrule Castle 2 Compass",
		"Desert Palace Compass",
		"Eastern Palace Compass",
		"Hyrule Castle Compass",
		"Sewer Passage Compass"})
	g.newSyncableBitU8(0x366, &g.SyncDungeonItems, []string{
		"",
		"",
		"Ganon's Tower Big Key",
		"Turtle Rock Big Key",
		"Thieves Town Big Key",
		"Tower of Hera Big Key",
		"Ice Palace Big Key",
		"Skull Woods Big Key"})
	g.newSyncableBitU8(0x367, &g.SyncDungeonItems, []string{
		"Misery Mire Big Key",
		"Dark Palace Big Key",
		"Swamp Palace Big Key",
		"Hyrule Castle 2 Big Key",
		"Desert Palace Big Key",
		"Eastern Palace Big Key",
		"Hyrule Castle Big Key",
		"Sewer Passage Big Key"})
	g.newSyncableBitU8(0x368, &g.SyncDungeonItems, []string{
		"",
		"",
		"Ganon's Tower Map",
		"Turtle Rock Map",
		"Thieves Town Map",
		"Tower of Hera Map",
		"Ice Palace Map",
		"Skull Woods Map"})
	g.newSyncableBitU8(0x369, &g.SyncDungeonItems, []string{
		"Misery Mire Map",
		"Dark Palace Map",
		"Swamp Palace Map",
		"Hyrule Castle 2 Map",
		"Desert Palace Map",
		"Eastern Palace Map",
		"Hyrule Castle Map",
		"Sewer Passage Map"})

	// bombs capacity:
	g.newSyncableMaxU8(0x370, &g.SyncItems, nil)
	// arrows capacity:
	g.newSyncableMaxU8(0x371, &g.SyncItems, nil)

	// pendants:
	g.newSyncableBitU8(0x374, &g.SyncDungeonItems, []string{
		"Red Pendant",
		"Blue Pendant",
		"Green Pendant",
		"",
		"",
		"",
		"",
		""})

	// player ability flags:
	g.newSyncableBitU8(0x379, &g.SyncItems, nil)

	// crystals:
	g.newSyncableBitU8(0x37A, &g.SyncDungeonItems, []string{
		"Crystal #6",
		"Crystal #1",
		"Crystal #5",
		"Crystal #7",
		"Crystal #2",
		"Crystal #4",
		"Crystal #3",
		""})

	// magic reduction (1/1, 1/2, 1/4):
	g.newSyncableMaxU8(0x37B, &g.SyncItems, []string{"1/2 Magic", "1/4 Magic"})

	// world state:
	g.newSyncableMaxU8(0x3C5, &g.SyncProgress, []string{
		"Q#Hyrule Castle Dungeon started",
		"Q#Hyrule Castle Dungeon completed",
		"Q#Search for Crystals started"}).onUpdated =
		func(asm *asm.Emitter, initial, updated uint8) {
			if initial < 2 && updated >= 2 {
				asm.JSL(g.romFunctions[fnLoadSpriteGfx])

				// overworld only:
				if g.local.Module == 0x09 && g.local.SubModule == 0 {
					asm.LDA_imm8_b(0x00)
					asm.STA_dp(0x1D)
					asm.STA_dp(0x8C)
					asm.JSL(g.romFunctions[fnOverworldFinishMirrorWarp])
					// clear sfx:
					asm.LDA_imm8_b(0x05)
					asm.STA_abs(0x012D)
				}
			}
		}

	// progress flags 1/2:
	g.newSyncableCustomU8(0x3C6, &g.SyncProgress, nil,
		func(s *syncableCustomU8, asm *asm.Emitter) bool {
			offset := s.offset
			initial := s.g.local.SRAM[offset]

			newBits := initial
			for _, p := range g.ActivePlayers() {
				v := p.SRAM[offset]
				// if local player has not achieved uncle leaving house, leave it cleared otherwise link never wakes up:
				if initial&0x10 == 0 {
					v &= ^uint8(0x10)
				}
				newBits |= v
			}

			if newBits == initial {
				// no change:
				return false
			}

			// notify local player of new item received:
			//g.notifyNewItem(s.names[v])

			addr := 0x7EF000 + uint32(offset)
			asm.LDA_imm8_b(newBits & ^initial)
			asm.ORA_long(addr)
			asm.STA_long(addr)

			// if receiving uncle's gear, remove zelda telepathic follower:
			if newBits&0x01 == 0x01 {
				asm.LDA_long(0x7EF3CC)
				asm.CMP_imm8_b(0x05)
				asm.BNE(0x06)
				asm.LDA_imm8_b(0x00)   // 2 bytes
				asm.STA_long(0x7EF3CC) // 4 bytes
			}

			return true
		})

	// map icons:
	g.newSyncableMaxU8(0x3C7, &g.SyncProgress, nil)
	// skip 0x3C8 start at location

	// progress flags 2/2:
	g.newSyncableCustomU8(0x3C9, &g.SyncProgress, nil,
		func(s *syncableCustomU8, asm *asm.Emitter) bool {
			offset := s.offset
			initial := s.g.local.SRAM[offset]

			newBits := initial
			for _, p := range g.ActivePlayers() {
				v := p.SRAM[offset]
				newBits |= v
			}

			if newBits == initial {
				// no change:
				return false
			}

			// notify local player of new item received:
			//g.notifyNewItem(s.names[v])

			addr := 0x7EF000 + uint32(offset)
			asm.LDA_imm8_b(newBits & ^initial)
			asm.ORA_long(addr)
			asm.STA_long(addr)

			// remove purple chest follower if purple chest opened:
			if newBits&0x10 == 0x10 {
				asm.LDA_long(0x7EF3CC)
				asm.CMP_imm8_b(0x0C)
				asm.BNE(0x06)
				asm.LDA_imm8_b(0x00)   // 2 bytes
				asm.STA_long(0x7EF3CC) // 4 bytes
			}
			// lose smithy follower if already rescued:
			if newBits&0x20 == 0x20 {
				asm.LDA_long(0x7EF3CC)
				asm.CMP_imm8_b(0x07)
				asm.BNE(0x06)
				asm.LDA_imm8_b(0x00)   // 2 bytes
				asm.STA_long(0x7EF3CC) // 4 bytes
				asm.CMP_imm8_b(0x08)
				asm.BNE(0x06)
				asm.LDA_imm8_b(0x00)   // 2 bytes
				asm.STA_long(0x7EF3CC) // 4 bytes
			}

			return true
		})

}

type generateUpdateDelegate func(s *syncableCustomU8, asm *asm.Emitter) bool

type syncableCustomU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	generateUpdate generateUpdateDelegate
}

func (g *Game) newSyncableCustomU8(offset uint16, enabled *bool, names []string, generateUpdate generateUpdateDelegate) *syncableCustomU8 {
	s := &syncableCustomU8{
		g:              g,
		offset:         offset,
		isEnabled:      enabled,
		names:          names,
		generateUpdate: generateUpdate,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableCustomU8) Offset() uint16                       { return s.offset }
func (s *syncableCustomU8) Size() uint                           { return 1 }
func (s *syncableCustomU8) IsEnabled() bool                      { return *s.isEnabled }
func (s *syncableCustomU8) GenerateUpdate(asm *asm.Emitter) bool { return s.generateUpdate(s, asm) }

type syncableBitU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	onUpdated func(asm *asm.Emitter, initial, updated uint8)
}

func (g *Game) newSyncableBitU8(offset uint16, enabled *bool, names []string) *syncableBitU8 {
	s := &syncableBitU8{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
		names:     names,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableBitU8) Offset() uint16  { return s.offset }
func (s *syncableBitU8) Size() uint      { return 1 }
func (s *syncableBitU8) IsEnabled() bool { return *s.isEnabled }

func (s *syncableBitU8) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local
	offset := s.offset

	initial := local.SRAM[offset]

	newBits := initial
	for _, p := range g.ActivePlayers() {
		v := p.SRAM[offset]
		newBits |= v
	}

	if newBits == initial {
		// no change:
		return false
	}

	// notify local player of new item received:
	//g.notifyNewItem(s.names[v])

	//log.Printf("sram[%04x]: %02x -> %02x\n", offset, initial, newBits)
	addr := 0x7EF000 + uint32(offset)
	asm.LDA_imm8_b(newBits & ^initial)
	asm.ORA_long(addr)
	asm.STA_long(addr)

	if s.onUpdated != nil {
		s.onUpdated(asm, initial, newBits)
	}

	return true
}

type syncableMaxU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	onUpdated func(asm *asm.Emitter, initial, updated uint8)
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
		s.onUpdated(asm, initial, maxV)
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

type syncableBottle struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string
}

func (g *Game) newSyncableBottle(offset uint16, enabled *bool, names []string) *syncableBottle {
	s := &syncableBottle{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
		names:     names,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableBottle) Offset() uint16  { return s.offset }
func (s *syncableBottle) Size() uint      { return 1 }
func (s *syncableBottle) IsEnabled() bool { return *s.isEnabled }

func (s *syncableBottle) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local
	offset := s.offset

	initial := local.SRAM[offset]
	if initial != 0 {
		// don't change existing bottle contents:
		return false
	}

	// max() is an odd choice here but something has to reconcile any differences among
	// all remote players that have this bottle slot filled.
	maxP := local
	maxV := initial
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

	return true
}
