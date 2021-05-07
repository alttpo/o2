package alttp

import (
	"fmt"
	"o2/snes/asm"
	"strings"
)

var dungeonNames = []string{
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
	// CanUpdate checks whether this item is available to be updated after the last write:
	CanUpdate() bool
	// GenerateUpdate generates a 65816 asm routine to update WRAM if applicable
	// returns true if program was generated, false if asm was not modified
	GenerateUpdate(asm *asm.Emitter) bool
}

func (g *Game) initSync() {
	// reset map:
	g.syncableItems = make(map[uint16]SyncableItem)
	g.syncableBitU16 = make(map[uint16]*syncableBitU16)

	// don't set WRAM timestamps on first read from SNES:
	g.notFirstWRAMRead = false

	// WRAM offsets for small keys, crystal switches, etc:
	g.initSmallKeysSync()
	g.local.WRAM[0x0400] = &SyncableWRAM{
		Name:      "current dungeon supertile state",
		Size:      2,
		Timestamp: 0,
		Value:     0xFFFF,
	}

	// define syncable items:
	if !g.isVTRandomizer() {
		// these item slots are disabled for sync under VT randomizers since they can be swapped at will:
		g.newSyncableCustomU8(0x340, &g.SyncItems, func(s *syncableCustomU8, asm *asm.Emitter) bool {
			g := s.g
			local := g.LocalPlayer()
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
			received := ""
			if maxV == 1 {
				received = "Bow"
			} else if maxV == 3 {
				received = "Silver Bow"
				maxV = 3
			}
			s.pendingUpdate = true
			s.updatingTo = maxV
			s.notification = fmt.Sprintf("got %s from %s", received, maxP.Name)
			asm.Comment(s.notification + ":")

			asm.LDA_long(0x7EF377) // arrows
			asm.CMP_imm8_b(0x01)   // are arrows present?
			asm.LDA_imm8_b(maxV)   // bow level; 1 = wood, 3 = silver
			asm.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present
			asm.STA_long(0x7EF000 + uint32(offset))

			return true
		}).isUpdateStillPending = func(s *syncableCustomU8) bool {
			return g.LocalPlayer().SRAM[s.offset] != s.updatingTo && g.LocalPlayer().SRAM[s.offset] != s.updatingTo+1
		}
		g.newSyncableMaxU8(0x341, &g.SyncItems, []string{"Blue Boomerang", "Red Boomerang"}, nil)
		g.newSyncableMaxU8(0x344, &g.SyncItems, []string{"Mushroom", "Magic Powder"}, nil)
		g.newSyncableMaxU8(0x34C, &g.SyncItems, []string{"Shovel", "Flute", "Flute (activated)"}, nil)
	}
	g.newSyncableMaxU8(0x342, &g.SyncItems, []string{"Hookshot"}, nil)
	// skip 0x343 bomb count
	g.newSyncableMaxU8(0x345, &g.SyncItems, []string{"Fire Rod"}, nil)
	g.newSyncableMaxU8(0x346, &g.SyncItems, []string{"Ice Rod"}, nil)
	g.newSyncableMaxU8(0x347, &g.SyncItems, []string{"Bombos Medallion"}, nil)
	g.newSyncableMaxU8(0x348, &g.SyncItems, []string{"Ether Medallion"}, nil)
	g.newSyncableMaxU8(0x349, &g.SyncItems, []string{"Quake Medallion"}, nil)
	g.newSyncableMaxU8(0x34A, &g.SyncItems, []string{"Lamp"}, nil)
	g.newSyncableMaxU8(0x34B, &g.SyncItems, []string{"Hammer"}, nil)
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
		local := g.LocalPlayer()

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
		s.pendingUpdate = true
		s.updatingTo = updated

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
		s.notification = fmt.Sprintf("got %s from %s", received, maxP.Name)
		asm.Comment(s.notification + ":")

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

	if g.isVTRandomizer() {
		// Randomizer item flags:
		item_swap := g.newSyncableBitU8(0x38C, &g.SyncItems, []string{
			"Flute (activated)",
			"Flute",
			"Shovel",
			"",
			"Magic Powder",
			"Mushroom",
			"Red Boomerang",
			"Blue Boomerang",
		}, func(s *syncableBitU8, a *asm.Emitter, initial, updated uint8) {
			// mushroom/powder:
			if initial&0x10 == 0 && updated&0x10 != 0 {
				// set powder in inventory:
				a.Comment("set Magic Powder in inventory:")
				a.LDA_long(0x7EF344)
				a.BNE(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF344)
			} else if initial&0x20 == 0 && updated&0x20 != 0 {
				// set mushroom in inventory:
				a.Comment("set Mushroom in inventory:")
				a.LDA_long(0x7EF344)
				a.BNE(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF344)
			}

			// shovel/flute:
			if initial&0x01 == 0 && updated&0x01 != 0 {
				// flute (activated):
				a.Comment("set Flute (activated) in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE(6)
				a.LDA_imm8_b(3)
				a.STA_long(0x7EF34C)
			} else if initial&0x02 == 0 && updated&0x02 != 0 {
				// flute (activated):
				a.Comment("set Flute in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF34C)
			} else if initial&0x04 == 0 && updated&0x04 != 0 {
				// flute (activated):
				a.Comment("set Shovel in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF34C)
			}

			// red/blue boomerang:
			if initial&0x40 == 0 && updated&0x40 != 0 {
				// set powder in inventory:
				a.Comment("set Red Boomerang in inventory:")
				a.LDA_long(0x7EF341)
				a.BNE(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF341)
			} else if initial&0x80 == 0 && updated&0x80 != 0 {
				// set mushroom in inventory:
				a.Comment("set Blue Boomerang in inventory:")
				a.LDA_long(0x7EF341)
				a.BNE(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF341)
			}
		})
		item_swap.generateAsm = func(s *syncableBitU8, asm *asm.Emitter, initial, updated, newBits uint8) {
			const longAddr = 0x7EF38C
			// make flute (inactive) and flute (activated) mutually exclusive:
			asm.LDA_long(longAddr)
			if newBits&0b00000011 != 0 {
				asm.AND_imm8_b(0b11111100)
				s.updatingTo = initial & 0b11111100 | newBits
			} else {
				s.updatingTo = initial | newBits
			}
			asm.ORA_imm8_b(newBits)
			asm.STA_long(longAddr)
		}

		g.newSyncableBitU8(0x38E, &g.SyncItems, []string{
			"",
			"",
			"",
			"",
			"",
			"", // 2nd Progressive Bow
			"Bow",
			"Silver Bow",
		}, func(s *syncableBitU8, a *asm.Emitter, initial, updated uint8) {
			// bow/silver:
			if initial&0x40 == 0 && updated&0x40 != 0 {
				// set powder in inventory:
				a.Comment("set Bow in inventory:")
				a.LDA_long(0x7EF340)
				a.BNE(0xe)

				a.LDA_long(0x7EF377) // load arrows
				a.CMP_imm8_b(0x01)   // are arrows present?
				a.LDA_imm8_b(1)      // bow level; 1 = wood, 3 = silver
				a.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present

				a.STA_long(0x7EF340)
			} else if initial&0x80 == 0 && updated&0x80 != 0 {
				// set mushroom in inventory:
				a.Comment("set Silver Bow in inventory:")
				a.LDA_long(0x7EF340)
				a.BNE(0xe)

				a.LDA_long(0x7EF377) // load arrows
				a.CMP_imm8_b(0x01)   // are arrows present?
				a.LDA_imm8_b(3)      // bow level; 1 = wood, 3 = silver
				a.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present

				a.STA_long(0x7EF340)
			}
		})
	}

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
		local := s.g.LocalPlayer()
		initial := local.SRAM[offset]

		// check to make sure zelda telepathic follower removed if have uncle's gear:
		if initial&0x01 == 0x01 && local.SRAM[0x3CC] == 0x05 {
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
		s.pendingUpdate = true
		s.updatingTo = newBits

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
		initial := s.g.LocalPlayer().SRAM[offset]

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
		s.pendingUpdate = true
		s.updatingTo = newBits

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

	if g.isVTRandomizer() {
		// NPC flags:
		g.newSyncableMaxU8(0x410, &g.SyncProgress, nil, nil)
		g.newSyncableMaxU8(0x411, &g.SyncProgress, nil, nil)
		// coat for festive
		g.newSyncableMaxU8(0x41A, &g.SyncItems, nil, nil)

		// Progressive item counters:
		// shield
		g.newSyncableMaxU8(0x416, &g.SyncItems, nil, nil)
		// sword and shield:
		g.newSyncableBitU8(0x422, &g.SyncItems, nil, nil)
		// bow:
		g.newSyncableBitU8(0x42A, &g.SyncItems, nil, nil)
	}

	openDoor := func(asm *asm.Emitter, initial, updated uint16) bool {
		// must only be in dungeon module:
		if !g.local.IsDungeon() {
			return false
		}
		if g.local.SubModule != 0 {
			return false
		}

		// only pay attention to the door bits changing:
		initial &= 0xF000
		updated &= 0xF000
		if initial == updated {
			return false
		}

		// determine which door opened (or the first door that opened if multiple?):
		b := uint16(0x8000)
		k := uint32(0)
		for ; k < 4; k++ {
			if updated&b != 0 {
				break
			}
			b >>= 1
		}

		// emit some asm to open this door locally:
		k2 := k << 1
		doorTilemapAddr := g.wramU16(0x19A0+k2) >> 1
		doorType := g.wramU16(0x19C0 + k2)
		kind := ""
		switch doorType & 3 {
		case 0: // up
			doorTilemapAddr += 0x81
			kind = "UP"
			break
		case 1: // down
			doorTilemapAddr += 0x42
			kind = "DOWN"
			break
		case 2: // left
			doorTilemapAddr += 0x42
			kind = "LEFT"
			break
		case 3: // right
			doorTilemapAddr += 0x42
			kind = "RIGHT"
			break
		}

		asm.Comment(fmt.Sprintf("open door[%d] %s", k, kind))
		asm.REP(0x30)
		asm.LDA_imm16_w(doorTilemapAddr)
		asm.STA_abs(0x068E)
		asm.LDA_imm16_w(0x0008) // TODO: confirm this value?
		asm.STA_abs(0x0690)
		asm.SEP(0x30)
		// set door open submodule:
		asm.LDA_imm8_b(0x04)
		asm.STA_dp(0x11)
		asm.REP(0x30)
		return true
	}

	// sync wram[$0400] for current dungeon supertile door state:
	g.syncableBitU16[0x0400] = &syncableBitU16{
		g:         g,
		offset:    0x0400,
		isEnabled: &g.SyncUnderworld,
		names:     nil,
		mask:      0xFFFF,
		// one-off to read from WRAM[] instead of SRAM[]:
		readU16:     func(p *Player, offs uint16) uint16 { return p.WRAM[offs].ValueUsed },
		longAddress: longAddressWRAM,
		// filter out players not in local player's current dungeon supertile:
		playerPredicate: func(p *Player) bool {
			// player must be in dungeon module:
			if !p.IsDungeon() {
				return false
			}
			if p.SubModule != 0 {
				return false
			}
			// player must have same dungeon supertile as local:
			if p.DungeonRoom != g.local.DungeonRoom {
				return false
			}
			return true
		},
		// open the local door(s):
		onUpdated: func(s *syncableBitU16, asm *asm.Emitter, initial, updated uint16) {
			asm.Comment("open door based on wram[$0400] bits")
			openDoor(asm, initial, updated)
		},
	}

	// underworld rooms:
	for room := uint16(0x000); room < 0x128; room++ {
		g.underworld[room] = syncableBitU16{
			g:               g,
			offset:          room << 1,
			isEnabled:       &g.SyncUnderworld,
			names:           nil,
			mask:            0xFFFF,
			playerPredicate: playerPredicateIdentity,
			readU16:         playerReadSRAM,
			longAddress:     longAddressSRAM,
			onUpdated: func(s *syncableBitU16, asm *asm.Emitter, initial, updated uint16) {
				// local player must only be in dungeon module:
				if !g.local.IsDungeon() {
					return
				}
				// only pay attention to supertile state changes when the local player is in that supertile:
				if s.offset>>1 != g.local.DungeonRoom {
					return
				}

				openDoor(asm, initial, updated)
			},
		}
	}

	// notify about bosses defeated:
	// u16[$7ef190] |= 0b0000100000000000 Armos
	g.underworld[0xC8].names = make([]string, 16)
	g.underworld[0xC8].names[0xb] = "Armos defeated"

	// u16[$7ef066] |= 0b0000100000000000 Lanmola
	g.underworld[0x33].names = make([]string, 16)
	g.underworld[0x33].names[0xb] = "Lanmola defeated"

	// u16[$7ef00e] |= 0b0000100000000000 Moldorm
	g.underworld[0x07].names = make([]string, 16)
	g.underworld[0x07].names[0xb] = "Moldorm defeated"

	// u16[$7ef040] |= 0b0000100000000000 Agahnim
	g.underworld[0x20].names = make([]string, 16)
	g.underworld[0x20].names[0xb] = "Agahnim defeated"
	g.underworld[0x20].onUpdated = func(s *syncableBitU16, a *asm.Emitter, initial, updated uint16) {
		// asm runs in 16-bit mode (REP #$30) by default for underworld sync.
		if initial&0b0000100000000000 != 0 || updated&0b0000100000000000 == 0 {
			return
		}
		a.Comment("check if in HC overworld:")
		a.SEP(0x30)

		// check if in dungeon:
		a.LDA_dp(0x1B)
		a.BNE(0x6F - 0x06) // exit
		// check if in HC overworld:
		a.LDA_dp(0x8A)
		a.CMP_imm8_b(0x1B)
		a.BNE(0x6F - 0x0C) // exit

		a.Comment("find free sprite slot:")
		a.LDX_imm8_b(0x0f)  //   LDX   #$0F
		_ = 0               // loop:
		a.LDA_abs_x(0x0DD0) //   LDA.w $0DD0,X
		a.BEQ(0x05)         //   BEQ   found
		a.DEX()             //   DEX
		a.BPL(-8)           //   BPL   loop
		a.BRA(0x6F - 0x18)  //   BRA   exit
		_ = 0               // found:

		a.Comment("open portal at HC:")
		// Y:
		a.LDA_imm8_b(0x50)
		a.STA_abs_x(0x0D00)
		a.LDA_imm8_b(0x08)
		a.STA_abs_x(0x0D20)
		// X:
		a.LDA_imm8_b(0xe0)
		a.STA_abs_x(0x0D10)
		a.LDA_imm8_b(0x07)
		a.STA_abs_x(0x0D30)
		// zeros:
		a.STZ_abs_x(0x0D40)
		a.STZ_abs_x(0x0D50)
		a.STZ_abs_x(0x0D60)
		a.STZ_abs_x(0x0D70)
		a.STZ_abs_x(0x0D80)
		// gfx?
		a.LDA_imm8_b(0x01)
		a.STA_abs_x(0x0D90)
		// hitbox/persist:
		a.STA_abs_x(0x0F60)
		// zeros:
		a.STZ_abs_x(0x0DA0)
		a.STZ_abs_x(0x0DB0)
		a.STZ_abs_x(0x0DC0)
		// active
		a.LDA_imm8_b(0x09)
		a.STA_abs_x(0x0DD0)
		// zeros:
		a.STZ_abs_x(0x0DE0)
		a.STZ_abs_x(0x0DF0)
		a.STZ_abs_x(0x0E00)
		a.STZ_abs_x(0x0E10)
		// whirlpool
		a.LDA_imm8_b(0xBA)
		a.STA_abs_x(0x0E20)
		// zeros:
		a.STZ_abs_x(0x0E30)
		// harmless
		a.LDA_imm8_b(0x80)
		a.STA_abs_x(0x0E40)
		// OAM:
		a.LDA_imm8_b(0x04)
		a.STA_abs_x(0x0F50)
		// exit:
		a.REP(0x30)

		// let player know the portal is opened:
		g.PushNotification("HC portal opened")
	}

	// u16[$7ef0b4] |= 0b0000100000000000 Helmasaur
	g.underworld[0x5A].names = make([]string, 16)
	g.underworld[0x5A].names[0xb] = "Helmasaur defeated"

	// u16[$7ef158] |= 0b0000100000000000 Blind
	g.underworld[0xAC].names = make([]string, 16)
	g.underworld[0xAC].names[0xb] = "Blind defeated"

	// u16[$7ef052] |= 0b0000100000000000 Mothula
	g.underworld[0x29].names = make([]string, 16)
	g.underworld[0x29].names[0xb] = "Mothula defeated"

	// u16[$7ef1bc] |= 0b0000100000000000 Kholdstare
	g.underworld[0xDE].names = make([]string, 16)
	g.underworld[0xDE].names[0xb] = "Kholdstare defeated"

	// u16[$7ef00c] |= 0b0000100000000000 Arrghus
	g.underworld[0x06].names = make([]string, 16)
	g.underworld[0x06].names[0xb] = "Arrghus defeated"

	// u16[$7ef120] |= 0b0000100000000000 Vitreous
	g.underworld[0x90].names = make([]string, 16)
	g.underworld[0x90].names[0xb] = "Vitreous defeated"

	// u16[$7ef148] |= 0b0000100000000000 Trinexx
	g.underworld[0xA4].names = make([]string, 16)
	g.underworld[0xA4].names[0xb] = "Trinexx defeated"

	// u16[$7ef01a] |= 0b0000100000000000 Agahnim 2
	g.underworld[0x0D].names = make([]string, 16)
	g.underworld[0x0D].names[0xb] = "Agahnim 2 defeated"

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
				notification := "create pyramid hole:"
				a.Comment(notification)
				g.PushNotification(notification)
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
