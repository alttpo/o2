package alttp

import (
	"fmt"
	"o2/snes/asm"
	"strings"
)

var dungeonNammes = []string{
	"Sewer Passage",     // $37C
	"Hyrule Castle",     // $37D
	"Eastern Palace",    // $37E
	"Desert Palace",     // $37F
	"Hyrule Castle 2",   // $380
	"Swamp Palace",      // $381
	"Dark Palace",       // $382
	"Misery Mire",       // $383
	"Skull Woods",       // $384
	"Ice Palace",        // $385
	"Tower of Hera",     // $386
	"Gargoyle's Domain", // $387
	"Turtle Rock",       // $388
	"Ganon's Tower",     // $389
	"Extra Dungeon 1",   // $38A unused
	"Extra Dungeon 2",   // $38B unused
}

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
	g.newSyncableCustomU8(0x340, &g.SyncItems, func(s *syncableCustomU8, asm *asm.Emitter) bool {
		g := s.g
		local := g.local
		offset := s.offset

		initial := local.SRAM[offset]
		// treat w/ and w/o arrows as the same:
		if initial == 2 {
			initial = 1
		} else if initial >= 4 {
			initial = 3
		}

		maxP := local
		maxV := initial
		for _, p := range g.ActivePlayers() {
			v := p.SRAM[offset]
			// treat w/ and w/o arrows as the same:
			if v == 2 {
				v = 1
			} else if v >= 4 {
				v = 3
			}
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

		received := ""
		if maxV == 1 {
			received = "Bow"
		} else if maxV == 3 {
			received = "Silver Bow"
			maxV = 3
		}
		asm.Comment(fmt.Sprintf("got %s from %s:", received, maxP.Name))

		asm.LDA_long(0x7EF377) // arrows
		asm.CMP_imm8_b(0x01)   // are arrows present?
		asm.LDA_imm8_b(maxV)   // bow level; 1 = wood, 3 = silver
		asm.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present
		asm.STA_long(0x7EF000 + uint32(offset))

		return true
	})
	g.newSyncableMaxU8(0x341, &g.SyncItems, []string{"Blue Boomerang", "Red Boomerang"}, nil)
	g.newSyncableMaxU8(0x342, &g.SyncItems, []string{"Hookshot"}, nil)
	// skip 0x343 bomb count
	g.newSyncableMaxU8(0x344, &g.SyncItems, []string{"Mushroom", "Magic Powder"}, nil)
	g.newSyncableMaxU8(0x345, &g.SyncItems, []string{"Fire Rod"}, nil)
	g.newSyncableMaxU8(0x346, &g.SyncItems, []string{"Ice Rod"}, nil)
	g.newSyncableMaxU8(0x347, &g.SyncItems, []string{"Bombos Medallion"}, nil)
	g.newSyncableMaxU8(0x348, &g.SyncItems, []string{"Ether Medallion"}, nil)
	g.newSyncableMaxU8(0x349, &g.SyncItems, []string{"Quake Medallion"}, nil)
	g.newSyncableMaxU8(0x34A, &g.SyncItems, []string{"Lamp"}, nil)
	g.newSyncableMaxU8(0x34B, &g.SyncItems, []string{"Hammer"}, nil)
	g.newSyncableMaxU8(0x34C, &g.SyncItems, []string{"Shovel", "Flute", "Flute (activated)"}, nil)
	g.newSyncableMaxU8(0x34D, &g.SyncItems, []string{"Bug Catching Net"}, nil)
	g.newSyncableMaxU8(0x34E, &g.SyncItems, []string{"Book of Mudora"}, nil)
	// skip 0x34F current bottle selection
	g.newSyncableMaxU8(0x350, &g.SyncItems, []string{"Cane of Somaria"}, nil)
	g.newSyncableMaxU8(0x351, &g.SyncItems, []string{"Cane of Byrna"}, nil)
	g.newSyncableMaxU8(0x352, &g.SyncItems, []string{"Magic Cape"}, nil)
	g.newSyncableMaxU8(0x353, &g.SyncItems, []string{"Magic Scroll", "Magic Mirror"}, nil)
	g.newSyncableMaxU8(0x354, &g.SyncItems, []string{"Power Gloves", "Titan's Mitts"},
		func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8) {
			asm.Comment("update armor/gloves palette:")
			asm.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
		})
	g.newSyncableMaxU8(0x355, &g.SyncItems, []string{"Pegasus Boots"}, nil)
	g.newSyncableMaxU8(0x356, &g.SyncItems, []string{"Flippers"}, nil)
	g.newSyncableMaxU8(0x357, &g.SyncItems, []string{"Moon Pearl"}, nil)
	// skip 0x358 unused

	swordSync := g.newSyncableMaxU8(0x359, &g.SyncItems, []string{"Fighter Sword", "Master Sword", "Tempered Sword", "Golden Sword"},
		func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8) {
			asm.Comment("decompress sword gfx:")
			asm.JSL(g.romFunctions[fnDecompGfxSword])
			asm.Comment("update sword palette:")
			asm.JSL(g.romFunctions[fnUpdatePaletteSword])
		})
	// prevent sync in of $ff (i.e. anything above $04) when smithy takes your sword for tempering
	swordSync.absMax = 4

	g.newSyncableMaxU8(0x35A, &g.SyncItems, []string{"Blue Shield", "Red Shield", "Mirror Shield"},
		func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8) {
			asm.Comment("decompress shield gfx:")
			asm.JSL(g.romFunctions[fnDecompGfxShield])
			asm.Comment("update shield palette:")
			asm.JSL(g.romFunctions[fnUpdatePaletteShield])
		})
	g.newSyncableMaxU8(0x35B, &g.SyncItems, []string{"Blue Mail", "Red Mail"},
		func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8) {
			asm.Comment("update armor/gloves palette:")
			asm.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
		})

	bottleItemNames := []string{"Shroom", "Empty Bottle", "Red Potion", "Green Potion", "Blue Potion", "Fairy", "Bee", "Good Bee"}
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
		"Skull Woods Compass"},
		nil)
	g.newSyncableBitU8(0x365, &g.SyncDungeonItems, []string{
		"Misery Mire Compass",
		"Dark Palace Compass",
		"Swamp Palace Compass",
		"Hyrule Castle 2 Compass",
		"Desert Palace Compass",
		"Eastern Palace Compass",
		"Hyrule Castle Compass",
		"Sewer Passage Compass"},
		nil)
	g.newSyncableBitU8(0x366, &g.SyncDungeonItems, []string{
		"",
		"",
		"Ganon's Tower Big Key",
		"Turtle Rock Big Key",
		"Thieves Town Big Key",
		"Tower of Hera Big Key",
		"Ice Palace Big Key",
		"Skull Woods Big Key"},
		nil)
	g.newSyncableBitU8(0x367, &g.SyncDungeonItems, []string{
		"Misery Mire Big Key",
		"Dark Palace Big Key",
		"Swamp Palace Big Key",
		"Hyrule Castle 2 Big Key",
		"Desert Palace Big Key",
		"Eastern Palace Big Key",
		"Hyrule Castle Big Key",
		"Sewer Passage Big Key"},
		nil)
	g.newSyncableBitU8(0x368, &g.SyncDungeonItems, []string{
		"",
		"",
		"Ganon's Tower Map",
		"Turtle Rock Map",
		"Thieves Town Map",
		"Tower of Hera Map",
		"Ice Palace Map",
		"Skull Woods Map"},
		nil)
	g.newSyncableBitU8(0x369, &g.SyncDungeonItems, []string{
		"Misery Mire Map",
		"Dark Palace Map",
		"Swamp Palace Map",
		"Hyrule Castle 2 Map",
		"Desert Palace Map",
		"Eastern Palace Map",
		"Hyrule Castle Map",
		"Sewer Passage Map"},
		nil)

	// heart containers:
	g.newSyncableCustomU8(0x36C, &g.SyncHearts, func(s *syncableCustomU8, asm *asm.Emitter) bool {
		g := s.g
		local := g.local

		initial := (local.SRAM[0x36C] & ^uint8(7)) | (local.SRAM[0x36B] & 3)

		maxP := local
		updated := initial
		for _, p := range g.ActivePlayers() {
			v := (p.SRAM[0x36C] & ^uint8(7)) | (p.SRAM[0x36B] & 3)
			if v > updated {
				updated, maxP = v, p
			}
		}

		if updated == initial {
			// no change:
			return false
		}

		// notify local player of new item received:
		_ = maxP

		oldHearts := initial & ^uint8(7)
		oldPieces := initial & uint8(3)
		newHearts := updated & ^uint8(7)
		newPieces := updated & uint8(3)

		diffHearts := (newHearts + (newPieces << 1)) - (oldHearts + (oldPieces << 1))
		fullHearts := diffHearts >> 3
		pieces := (diffHearts & 7) >> 1

		hc := &strings.Builder{}
		if fullHearts == 1 {
			hc.WriteString("1 new heart")
		} else if fullHearts > 1 {
			hc.WriteString(fmt.Sprintf("%d new hearts", fullHearts))
		}
		if fullHearts >= 1 && pieces >= 1 {
			hc.WriteString(", ")
		}

		if pieces == 1 {
			hc.WriteString("1 new heart piece")
		} else if pieces > 0 {
			hc.WriteString(fmt.Sprintf("%d new heart pieces", pieces))
		}

		received := hc.String()
		asm.Comment(fmt.Sprintf("got %s from %s:", received, maxP.Name))

		asm.LDA_imm8_b(updated & ^uint8(7))
		asm.STA_long(0x7EF000 + uint32(0x36C))
		asm.LDA_imm8_b(updated & uint8(3))
		asm.STA_long(0x7EF000 + uint32(0x36B))

		return true
	})

	// bombs capacity:
	g.newSyncableMaxU8(0x370, &g.SyncItems, nil, nil)
	// arrows capacity:
	g.newSyncableMaxU8(0x371, &g.SyncItems, nil, nil)

	// pendants:
	g.newSyncableBitU8(0x374, &g.SyncDungeonItems, []string{
		"Red Pendant",
		"Blue Pendant",
		"Green Pendant",
		"",
		"",
		"",
		"",
		""},
		nil)

	// player ability flags:
	g.newSyncableBitU8(0x379, &g.SyncItems, []string{
		"",
		"Swim Ability",
		"Dash Ability",
		"Pull Ability",
		"",
		"Talk Ability",
		"Read Ability",
		""},
		nil)

	// crystals:
	g.newSyncableBitU8(0x37A, &g.SyncDungeonItems, []string{
		"Crystal #6",
		"Crystal #1",
		"Crystal #5",
		"Crystal #7",
		"Crystal #2",
		"Crystal #4",
		"Crystal #3",
		""},
		nil)

	// magic reduction (1/1, 1/2, 1/4):
	g.newSyncableMaxU8(0x37B, &g.SyncItems, []string{"1/2 Magic", "1/4 Magic"}, nil)

	// world state:
	g.newSyncableMaxU8(0x3C5, &g.SyncProgress, []string{
		"Hyrule Castle Dungeon started",
		"Hyrule Castle Dungeon completed",
		"Search for Crystals started"},
		func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8) {
			if initial < 2 && updated >= 2 {
				asm.Comment("load sprite gfx:")
				asm.JSL(g.romFunctions[fnLoadSpriteGfx])

				// overworld only:
				if g.local.Module == 0x09 && g.local.SubModule == 0 {
					asm.Comment("reset overworld:")
					asm.LDA_imm8_b(0x00)
					asm.STA_dp(0x1D)
					asm.STA_dp(0x8C)
					asm.JSL(g.romFunctions[fnOverworldFinishMirrorWarp])
					// clear sfx:
					asm.LDA_imm8_b(0x05)
					asm.STA_abs(0x012D)
				}
			}
		})

	// progress flags 1/2:
	g.newSyncableCustomU8(0x3C6, &g.SyncProgress, func(s *syncableCustomU8, asm *asm.Emitter) bool {
		offset := s.offset
		initial := s.g.local.SRAM[offset]

		// check to make sure zelda telepathic follower removed if have uncle's gear:
		if initial&0x01 == 0x01 && s.g.local.SRAM[0x3CC] == 0x05 {
			asm.Comment("already have uncle's gear; remove telepathic zelda follower:")
			asm.LDA_long(0x7EF3CC)
			asm.CMP_imm8_b(0x05)
			asm.BNE(0x06)
			asm.LDA_imm8_b(0x00)   // 2 bytes
			asm.STA_long(0x7EF3CC) // 4 bytes
			return true
		}

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

		orBits := newBits & ^initial
		asm.Comment(fmt.Sprintf("progress1 |= %#08b", orBits))

		addr := 0x7EF000 + uint32(offset)
		asm.LDA_imm8_b(orBits)
		asm.ORA_long(addr)
		asm.STA_long(addr)

		// if receiving uncle's gear, remove zelda telepathic follower:
		if newBits&0x01 == 0x01 && initial&0x01 == 0 {
			asm.Comment("received uncle's gear; remove telepathic zelda follower:")
			// this may run when link is still in bed so uncle adds the follower before link can get up:
			asm.LDA_long(0x7EF3CC)
			asm.CMP_imm8_b(0x05)
			asm.BNE(0x06)
			asm.LDA_imm8_b(0x00)   // 2 bytes
			asm.STA_long(0x7EF3CC) // 4 bytes
		}

		return true
	})

	// map markers:
	g.newSyncableMaxU8(0x3C7, &g.SyncProgress, []string{
		//"Map Marker at Castle",
		"Map Marker at Kakariko",
		"Map Marker at Sahasrahla",
		"Map Marker at Pendants",
		"Map Marker at Master Sword",
		"Map Marker at Agahnim Tower",
		"Map Marker at Darkness",
		"Map Marker at Crystals",
		"Map Marker at Ganon's Tower",
	}, nil)

	// skip 0x3C8 start at location

	// progress flags 2/2:
	g.newSyncableCustomU8(0x3C9, &g.SyncProgress, func(s *syncableCustomU8, asm *asm.Emitter) bool {
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

		orBits := newBits & ^initial
		asm.Comment(fmt.Sprintf("progress2 |= %#08b", orBits))

		addr := 0x7EF000 + uint32(offset)
		asm.LDA_imm8_b(orBits)
		asm.ORA_long(addr)
		asm.STA_long(addr)

		// remove purple chest follower if purple chest opened:
		if newBits&0x10 == 0x10 {
			asm.Comment("lose purple chest follower:")
			asm.LDA_long(0x7EF3CC)
			asm.CMP_imm8_b(0x0C)
			asm.BNE(0x06)
			asm.LDA_imm8_b(0x00)   // 2 bytes
			asm.STA_long(0x7EF3CC) // 4 bytes
		}
		// lose smithy follower if already rescued:
		if newBits&0x20 == 0x20 {
			asm.Comment("lose smithy follower:")
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

	// WRAM offsets for small keys, crystal switches, etc:
	g.initSyncableWRAM()

	// underworld rooms:
	for room := uint16(0x000); room < 0x128; room++ {
		g.underworld[room] = syncableBitU16{
			g:         g,
			offset:    room << 1,
			isEnabled: &g.SyncUnderworld,
			names:     nil,
			onUpdated: nil,
			mask:      0xFFFF,
		}
	}
	g.setUnderworldSyncMasks()

	// overworld areas:
	for offs := uint16(0x280); offs < 0x340; offs++ {
		g.overworld[offs-0x280] = syncableBitU8{
			g:         g,
			offset:    offs,
			isEnabled: &g.SyncUnderworld,
			names:     nil,
			onUpdated: nil,
			mask:      0xFF,
		}
	}

	// Pyramid bat crash: ([$7EF2DB] | 0x20)
	g.overworld[0x5B].onUpdated = func(s *syncableBitU8, a *asm.Emitter, initial, updated uint8) {
		if initial&0x20 == 0 && updated&0x20 == 0x20 {
			if g.local.OverworldArea == 0x5B {
				a.Comment("create pyramid hole:")
				a.JSL(g.romFunctions[fnOverworldCreatePyramidHole])
			}
		}
	}
}

func (g *Game) setUnderworldSyncMasks() {
	if g.SyncChests == g.lastSyncChests {
		return
	}

	g.lastSyncChests = g.SyncChests

	mask := uint16(0xFFFF)
	if !g.SyncChests {
		// chops off the 6 bits that sync chests/keys
		mask = ^uint16(0x03F0)
	}

	// set the masks for all the underworld rooms:
	for room := uint16(0x000); room < 0x128; room++ {
		g.underworld[room].mask = mask
	}

	// desync swamp inner watergate at $7EF06A (supertile $35):
	g.underworld[0x035].mask &= ^uint16(0x0080)
}

type syncableCustomU8Update func(s *syncableCustomU8, asm *asm.Emitter) bool

type syncableCustomU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool

	generateUpdate syncableCustomU8Update
}

func (g *Game) newSyncableCustomU8(offset uint16, enabled *bool, generateUpdate syncableCustomU8Update) *syncableCustomU8 {
	s := &syncableCustomU8{
		g:              g,
		offset:         offset,
		isEnabled:      enabled,
		generateUpdate: generateUpdate,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableCustomU8) Offset() uint16                       { return s.offset }
func (s *syncableCustomU8) Size() uint                           { return 1 }
func (s *syncableCustomU8) IsEnabled() bool                      { return *s.isEnabled }
func (s *syncableCustomU8) GenerateUpdate(asm *asm.Emitter) bool { return s.generateUpdate(s, asm) }

type syncableBitU8OnUpdated func(s *syncableBitU8, asm *asm.Emitter, initial, updated uint8)

type syncableBitU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string
	mask      uint8

	onUpdated syncableBitU8OnUpdated
}

func (g *Game) newSyncableBitU8(offset uint16, enabled *bool, names []string, onUpdated syncableBitU8OnUpdated) *syncableBitU8 {
	s := &syncableBitU8{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
		names:     names,
		onUpdated: onUpdated,
		mask:      0xFF,
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
	var receivedFrom [8]string

	updated := initial
	for _, p := range g.ActivePlayers() {
		v := p.SRAM[offset]
		v &= s.mask
		newBits := v & ^updated
		if newBits != 0 {
			k := uint8(1)
			for i := 0; i < 8; i++ {
				if newBits&k == k {
					receivedFrom[i] = p.Name
				}
				k <<= 1
			}
		}

		updated |= v
	}

	if updated == initial {
		// no change:
		return false
	}

	// notify local player of new item received:
	//g.notifyNewItem(s.names[v])

	addr := 0x7EF000 + uint32(offset)
	newBits := updated & ^initial

	if s.names != nil {
		received := make([]string, 0, len(s.names))
		k := uint8(1)
		for i := 0; i < len(s.names); i++ {
			if initial&k == 0 && updated&k == k {
				item := fmt.Sprintf("%s from %s", s.names[i], receivedFrom[i])
				received = append(received, item)
			}
			k <<= 1
		}
		asm.Comment(fmt.Sprintf("got %s:", strings.Join(received, ", ")))
	} else {
		asm.Comment(fmt.Sprintf("sram[$%04x] |= %#08b", offset, newBits))
	}

	asm.LDA_imm8_b(newBits)
	asm.ORA_long(addr)
	asm.STA_long(addr)

	if s.onUpdated != nil {
		s.onUpdated(s, asm, initial, updated)
	}

	return true
}

type syncableBitU16OnUpdated func(s *syncableBitU16, asm *asm.Emitter, initial, updated uint16)

type syncableBitU16 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string
	mask      uint16

	onUpdated syncableBitU16OnUpdated
}

func (g *Game) newSyncableBitU16(offset uint16, enabled *bool, names []string, onUpdated syncableBitU16OnUpdated) *syncableBitU16 {
	s := &syncableBitU16{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
		names:     names,
		onUpdated: onUpdated,
		mask:      0xFFFF,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableBitU16) Offset() uint16  { return s.offset }
func (s *syncableBitU16) Size() uint      { return 2 }
func (s *syncableBitU16) IsEnabled() bool { return *s.isEnabled }

func (s *syncableBitU16) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local
	offset := s.offset
	mask := s.mask

	initial := local.sramU16(offset)
	var receivedFrom [16]string

	updated := initial
	for _, p := range g.ActivePlayers() {
		v := p.sramU16(offset)
		v &= mask
		newBits := v & ^updated
		if newBits != 0 {
			k := uint16(1)
			for i := 0; i < 16; i++ {
				if newBits&k == k {
					receivedFrom[i] = p.Name
				}
				k <<= 1
			}
		}

		updated |= v
	}

	if updated == initial {
		// no change:
		return false
	}

	// notify local player of new item received:
	//g.notifyNewItem(s.names[v])

	addr := 0x7EF000 + uint32(offset)
	newBits := updated & ^initial

	if s.names != nil {
		received := make([]string, 0, len(s.names))
		k := uint16(1)
		for i := 0; i < len(s.names); i++ {
			if initial&k == 0 && updated&k == k {
				item := fmt.Sprintf("%s from %s", s.names[i], receivedFrom[i])
				received = append(received, item)
			}
			k <<= 1
		}
		asm.Comment(fmt.Sprintf("got %s:", strings.Join(received, ", ")))
	} else {
		asm.Comment(fmt.Sprintf("sram[$%04x] |= %#08b", offset, newBits))
	}

	asm.LDA_imm16_w(newBits)
	asm.ORA_long(addr)
	asm.STA_long(addr)

	if s.onUpdated != nil {
		s.onUpdated(s, asm, initial, updated)
	}

	return true
}

type syncableMaxU8OnUpdated func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8)

type syncableMaxU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	absMax uint8

	onUpdated syncableMaxU8OnUpdated
}

func (g *Game) newSyncableMaxU8(offset uint16, enabled *bool, names []string, onUpdated syncableMaxU8OnUpdated) *syncableMaxU8 {
	s := &syncableMaxU8{
		g:         g,
		offset:    offset,
		isEnabled: enabled,
		names:     names,
		absMax:    255,
		onUpdated: onUpdated,
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
		// discard value if too high:
		if v > s.absMax {
			continue
		}

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

	if s.names != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.names) {
			received := s.names[i]
			asm.Comment(fmt.Sprintf("got %s from %s:", received, maxP.Name))
		}
	} else {
		asm.Comment(fmt.Sprintf("sram[$%04x] = $%02x", offset, maxV))
	}

	asm.LDA_imm8_b(maxV)
	asm.STA_long(0x7EF000 + uint32(offset))

	if s.onUpdated != nil {
		s.onUpdated(s, asm, initial, maxV)
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
	maxV := local.sramU16(offset)
	initial := maxV
	for _, p := range g.ActivePlayers() {
		v := p.sramU16(offset)
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
	if initial >= 2 {
		// don't change existing bottle contents:
		return false
	}

	// max() is an odd choice here but something has to reconcile any differences among
	// all remote players that have this bottle slot filled.
	maxP := local
	maxV := initial
	for _, p := range g.ActivePlayers() {
		v := p.SRAM[offset]
		// ignore "shroom" bottle item:
		if v == 1 {
			v = 0
		}
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

	if s.names != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.names) {
			received := s.names[i]
			asm.Comment(fmt.Sprintf("got %s from %s:", received, maxP.Name))
		}
	}

	asm.LDA_imm8_b(maxV)
	asm.STA_long(0x7EF000 + uint32(offset))

	return true
}
