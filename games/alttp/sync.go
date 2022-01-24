package alttp

import (
	"fmt"
	"o2/games"
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

// for VT randomizers:
// InventorySwap1 $38C
const (
	IS1FluteActive uint8 = 1 << iota
	IS1FluteInactive
	IS1Shovel
	_
	IS1MagicPowder
	IS1Mushroom
	IS1RedBoomerang
	IS1BlueBoomerang
)

// InventorySwap2 $38E
const (
	_ uint8 = 1 << iota
	_
	_
	_
	_
	_
	IS2SilverBow // 0x40
	IS2WoodBow   // 0x80
)

func (g *Game) initSync() {
	// reset map:
	g.syncableItems = make(map[uint16]games.SyncStrategy)
	g.syncableItemsMin = 0xFFFF
	g.syncableItemsMax = 0x0000

	g.syncableBitU16 = make(map[uint16]*games.SyncableBitU16)
	g.syncableBitU16Min = 0xFFFF
	g.syncableBitU16Max = 0x0000

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
		g.NewSyncableVanillaBow(0x340, &g.SyncItems)
		g.NewSyncableVanillaItemU8(0x341, &g.SyncItems, nil)
		g.NewSyncableVanillaItemU8(0x344, &g.SyncItems, nil)
		g.NewSyncableVanillaItemU8(0x34C, &g.SyncItems, nil)
	}
	g.NewSyncableVanillaItemU8(0x342, &g.SyncItems, nil)
	// skip 0x343 bomb count
	g.NewSyncableVanillaItemU8(0x345, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x346, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x347, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x348, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x349, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x34A, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x34B, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x34D, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x34E, &g.SyncItems, nil)
	// skip 0x34F current bottle selection
	g.NewSyncableVanillaItemU8(0x350, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x351, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x352, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x353, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x354, &g.SyncItems,
		func(s *games.SyncableMaxU8, a *asm.Emitter, initial, updated uint8) {
			a.Comment("update armor/gloves palette:")
			a.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
		})
	g.NewSyncableVanillaItemU8(0x355, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x356, &g.SyncItems, nil)
	g.NewSyncableVanillaItemU8(0x357, &g.SyncItems, nil)
	// skip 0x358 unused

	swordSync := g.NewSyncableVanillaItemU8(0x359, &g.SyncItems,
		func(s *games.SyncableMaxU8, a *asm.Emitter, initial, updated uint8) {
			a.Comment("decompress sword gfx:")
			a.JSL(g.romFunctions[fnDecompGfxSword])
			a.Comment("update sword palette:")
			a.JSL(g.romFunctions[fnUpdatePaletteSword])
		})
	// prevent sync in of $ff (i.e. anything above $04) when smithy takes your sword for tempering
	swordSync.AbsMax = 4

	g.NewSyncableVanillaItemU8(0x35A, &g.SyncItems,
		func(s *games.SyncableMaxU8, a *asm.Emitter, initial, updated uint8) {
			a.Comment("decompress shield gfx:")
			a.JSL(g.romFunctions[fnDecompGfxShield])
			a.Comment("update shield palette:")
			a.JSL(g.romFunctions[fnUpdatePaletteShield])
		})
	g.NewSyncableVanillaItemU8(0x35B, &g.SyncItems,
		func(s *games.SyncableMaxU8, a *asm.Emitter, initial, updated uint8) {
			a.Comment("update armor/gloves palette:")
			a.JSL(g.romFunctions[fnUpdatePaletteArmorGloves])
		})

	g.NewSyncableBottle(0x35C, &g.SyncItems)
	g.NewSyncableBottle(0x35D, &g.SyncItems)
	g.NewSyncableBottle(0x35E, &g.SyncItems)
	g.NewSyncableBottle(0x35F, &g.SyncItems)

	// dungeon items:
	g.NewSyncableVanillaItemBitsU8(0x364, &g.SyncDungeonItems, nil)
	g.NewSyncableVanillaItemBitsU8(0x365, &g.SyncDungeonItems, nil)
	g.NewSyncableVanillaItemBitsU8(0x366, &g.SyncDungeonItems, nil)
	g.NewSyncableVanillaItemBitsU8(0x367, &g.SyncDungeonItems, nil)
	g.NewSyncableVanillaItemBitsU8(0x368, &g.SyncDungeonItems, nil)
	g.NewSyncableVanillaItemBitsU8(0x369, &g.SyncDungeonItems, nil)

	// heart containers:
	g.NewSyncableCustomU8(0x36C, &g.SyncHearts, func(s *games.SyncableCustomU8, a *asm.Emitter, index uint32) bool {
		g := s.SyncableGame
		local := g.LocalSyncablePlayer()

		localSRAM := local.ReadableMemory(games.SRAM)
		initial := (localSRAM.ReadU8(0x36C) & ^uint8(7)) | (localSRAM.ReadU8(0x36B) & 3)

		maxP := local
		updated := initial
		for _, p := range g.RemoteSyncablePlayers() {
			pSRAM := p.ReadableMemory(games.SRAM)
			v := (pSRAM.ReadU8(0x36C) & ^uint8(7)) | (pSRAM.ReadU8(0x36B) & 3)
			if v > updated {
				updated, maxP = v, p
			}
		}

		if updated == initial {
			// no change:
			return false
		}

		// notify local player of new item received:
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
		s.Notification = fmt.Sprintf("got %s from %s", received, maxP.Name())
		a.Comment(s.Notification + ":")

		a.LDA_imm8_b(updated & ^uint8(7))
		a.STA_long(localSRAM.BusAddress(0x36C))
		a.LDA_imm8_b(updated & uint8(3))
		a.STA_long(localSRAM.BusAddress(0x36B))

		// write confirmation:
		a.LDA_imm8_b(0x01)
		a.STA_long(a.GetBase() + 0x02 + index)

		return true
	})

	// bombs capacity:
	g.NewSyncableMaxU8(0x370, &g.SyncItems, nil, nil)
	// arrows capacity:
	g.NewSyncableMaxU8(0x371, &g.SyncItems, nil, nil)

	// pendants:
	g.NewSyncableVanillaItemBitsU8(0x374, &g.SyncDungeonItems, nil)

	// player ability flags:
	g.NewSyncableVanillaItemBitsU8(0x379, &g.SyncItems, nil)

	// crystals:
	g.NewSyncableVanillaItemBitsU8(0x37A, &g.SyncDungeonItems, nil)

	// magic reduction (1/1, 1/2, 1/4):
	g.NewSyncableVanillaItemU8(0x37B, &g.SyncItems, nil)

	if g.isVTRandomizer() {
		// Randomizer item flags:
		invSwap1 := g.NewSyncableVTItemBitsU8(0x38C, &g.SyncItems, func(s *games.SyncableBitU8, a *asm.Emitter, initial, updated uint8) {
			// mushroom/powder:
			if initial&IS1MagicPowder == 0 && updated&IS1MagicPowder != 0 {
				// set powder in inventory:
				a.Comment("set Magic Powder in inventory:")
				a.LDA_long(0x7EF344)
				a.BNE_imm8(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF344)
			} else if initial&IS1Mushroom == 0 && updated&IS1Mushroom != 0 {
				// set mushroom in inventory:
				a.Comment("set Mushroom in inventory:")
				a.LDA_long(0x7EF344)
				a.BNE_imm8(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF344)
			}

			// shovel/flute:
			if initial&IS1FluteActive == 0 && updated&IS1FluteActive != 0 {
				// flute (activated):
				a.Comment("set Flute (active) in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE_imm8(6)
				a.LDA_imm8_b(3)
				a.STA_long(0x7EF34C)
			} else if initial&IS1FluteInactive == 0 && updated&IS1FluteInactive != 0 {
				// flute (activated):
				a.Comment("set Flute (inactive) in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE_imm8(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF34C)
			} else if initial&IS1Shovel == 0 && updated&IS1Shovel != 0 {
				// flute (activated):
				a.Comment("set Shovel in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE_imm8(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF34C)
			}

			// red/blue boomerang:
			if initial&IS1RedBoomerang == 0 && updated&IS1RedBoomerang != 0 {
				// set powder in inventory:
				a.Comment("set Red Boomerang in inventory:")
				a.LDA_long(0x7EF341)
				a.BNE_imm8(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF341)
			} else if initial&IS1BlueBoomerang == 0 && updated&IS1BlueBoomerang != 0 {
				// set mushroom in inventory:
				a.Comment("set Blue Boomerang in inventory:")
				a.LDA_long(0x7EF341)
				a.BNE_imm8(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF341)
			}
		})
		invSwap1.GenerateAsm = func(s *games.SyncableBitU8, a *asm.Emitter, initial, updated, newBits uint8) {
			const longAddr = 0x7EF38C
			// make flute (inactive) and flute (activated) mutually exclusive:
			a.LDA_long(longAddr)
			if newBits&0b00000011 != 0 {
				a.AND_imm8_b(0b11111100)
			}
			a.ORA_imm8_b(newBits)
			a.STA_long(longAddr)
		}

		g.NewSyncableVTItemBitsU8(0x38E, &g.SyncItems, func(s *games.SyncableBitU8, a *asm.Emitter, initial, updated uint8) {
			// bow/silver:
			if initial&IS2SilverBow == 0 && updated&IS2SilverBow != 0 {
				// set silver bow in inventory:
				a.Comment("set Silver Bow in inventory:")
				a.LDA_long(0x7EF340)
				a.BNE_imm8(0xe)

				a.LDA_long(0x7EF377) // load arrows
				a.CMP_imm8_b(0x01)   // are arrows present?
				a.LDA_imm8_b(3)      // bow level; 1 = wood, 3 = silver
				a.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present

				a.STA_long(0x7EF340)
			} else if initial&IS2WoodBow == 0 && updated&IS2WoodBow != 0 {
				// set bow in inventory:
				a.Comment("set Bow in inventory:")
				a.LDA_long(0x7EF340)
				a.BNE_imm8(0xe)

				a.LDA_long(0x7EF377) // load arrows
				a.CMP_imm8_b(0x01)   // are arrows present?
				a.LDA_imm8_b(1)      // bow level; 1 = wood, 3 = silver
				a.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present

				a.STA_long(0x7EF340)
			}
		})
	}

	// world state:
	g.NewSyncableVanillaItemU8(0x3C5, &g.SyncProgress,
		func(s *games.SyncableMaxU8, a *asm.Emitter, initial, updated uint8) {
			if initial < 2 && updated >= 2 {
				a.Comment("load sprite gfx:")
				a.JSL(g.romFunctions[fnLoadSpriteGfx])

				// overworld only:
				if g.local.Module == 0x09 && g.local.SubModule == 0 {
					a.Comment("reset overworld:")
					a.LDA_imm8_b(0x00)
					a.STA_dp(0x1D)
					a.STA_dp(0x8C)
					a.JSL(g.romFunctions[fnOverworldFinishMirrorWarp])
					// clear sfx:
					a.LDA_imm8_b(0x05)
					a.STA_abs(0x012D)
				}
			}
		})

	// progress flags 1/2:
	g.NewSyncableCustomU8(0x3C6, &g.SyncProgress, func(s *games.SyncableCustomU8, a *asm.Emitter, index uint32) bool {
		offset := s.Offset
		local := s.SyncableGame.LocalSyncablePlayer()
		localSRAM := local.ReadableMemory(games.SRAM)
		initial := localSRAM.ReadU8(offset)

		// check to make sure zelda telepathic follower removed if have uncle's gear:
		if initial&0x01 == 0x01 && localSRAM.ReadU8(0x3CC) == 0x05 {
			a.Comment("already have uncle's gear; remove telepathic zelda follower:")
			a.LDA_long(0x7EF3CC)
			a.CMP_imm8_b(0x05)
			a.BNE_imm8(0x06)
			a.LDA_imm8_b(0x00)   // 2 bytes
			a.STA_long(0x7EF3CC) // 4 bytes

			// write confirmation:
			a.LDA_imm8_b(0x01)
			a.STA_long(a.GetBase() + 0x02 + index)

			return true
		}

		newBits := initial
		for _, p := range g.RemoteSyncablePlayers() {
			v := p.ReadableMemory(games.SRAM).ReadU8(offset)
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
		orBits := newBits & ^initial
		a.Comment(fmt.Sprintf("progress1 |= %#08b", orBits))

		addr := localSRAM.BusAddress(offset)
		a.LDA_imm8_b(orBits)
		a.ORA_long(addr)
		a.STA_long(addr)

		// write confirmation:
		a.LDA_imm8_b(0x01)
		a.STA_long(a.GetBase() + 0x02 + index)

		// if receiving uncle's gear, remove zelda telepathic follower:
		if newBits&0x01 == 0x01 && initial&0x01 == 0 {
			a.Comment("received uncle's gear; remove telepathic zelda follower:")
			// this may run when link is still in bed so uncle adds the follower before link can get up:
			a.LDA_long(0x7EF3CC)
			a.CMP_imm8_b(0x05)
			a.BNE_imm8(0x06)
			a.LDA_imm8_b(0x00)   // 2 bytes
			a.STA_long(0x7EF3CC) // 4 bytes
		}

		return true
	})

	// map markers:
	g.NewSyncableVanillaItemU8(0x3C7, &g.SyncProgress, nil)

	// skip 0x3C8 start at location

	// progress flags 2/2:
	g.NewSyncableCustomU8(0x3C9, &g.SyncProgress, func(s *games.SyncableCustomU8, a *asm.Emitter, index uint32) bool {
		offset := s.Offset
		initial := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(games.SRAM).ReadU8(offset)

		newBits := initial
		for _, p := range g.RemoteSyncablePlayers() {
			v := p.ReadableMemory(games.SRAM).ReadU8(offset)
			newBits |= v
		}

		if newBits == initial {
			// no change:
			return false
		}

		// notify local player of new item received:
		orBits := newBits & ^initial
		a.Comment(fmt.Sprintf("progress2 |= %#08b", orBits))

		addr := 0x7EF000 + uint32(offset)
		a.LDA_imm8_b(orBits)
		a.ORA_long(addr)
		a.STA_long(addr)

		// write confirmation:
		a.LDA_imm8_b(0x01)
		a.STA_long(a.GetBase() + 0x02 + index)

		// remove purple chest follower if purple chest opened:
		if newBits&0x10 == 0x10 {
			a.Comment("lose purple chest follower:")
			a.LDA_long(0x7EF3CC)
			a.CMP_imm8_b(0x0C)
			a.BNE_imm8(0x06)
			a.LDA_imm8_b(0x00)   // 2 bytes
			a.STA_long(0x7EF3CC) // 4 bytes
		}
		// lose smithy follower if already rescued:
		if newBits&0x20 == 0x20 {
			a.Comment("lose smithy follower:")
			a.LDA_long(0x7EF3CC)
			a.CMP_imm8_b(0x07)
			a.BNE_imm8(0x06)
			a.LDA_imm8_b(0x00)   // 2 bytes
			a.STA_long(0x7EF3CC) // 4 bytes
			a.CMP_imm8_b(0x08)
			a.BNE_imm8(0x06)
			a.LDA_imm8_b(0x00)   // 2 bytes
			a.STA_long(0x7EF3CC) // 4 bytes
		}

		return true
	})

	if g.isVTRandomizer() {
		// NPC flags:
		g.NewSyncableMaxU8(0x410, &g.SyncProgress, nil, nil)
		g.NewSyncableMaxU8(0x411, &g.SyncProgress, nil, nil)
		// coat for festive
		g.NewSyncableMaxU8(0x41A, &g.SyncItems, nil, nil)

		// Progressive item counters:
		// shield
		g.NewSyncableMaxU8(0x416, &g.SyncItems, nil, nil)
		// sword and shield:
		g.NewSyncableBitU8(0x422, &g.SyncItems, nil, nil)
		// bow:
		g.NewSyncableBitU8(0x42A, &g.SyncItems, nil, nil)
	}

	// sync wram[$0400] for current dungeon supertile door state:
	g.syncableBitU16[0x0400] = games.NewSyncableBitU16(
		g,
		0x0400,
		&g.SyncUnderworld,
		nil,
		// open the local door(s):
		func(s *games.SyncableBitU16, a *asm.Emitter, initial, updated uint16) {
			a.Comment("open door based on wram[$0400] bits")
			g.openDoor(a, initial, updated)
		},
	)
	g.syncableBitU16[0x0400].MemoryKind = games.WRAM
	// filter out players not in local player's current dungeon supertile:
	g.syncableBitU16[0x0400].PlayerPredicate = func(sp games.SyncablePlayer) bool {
		p := sp.(*Player)
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
	}
	g.syncableBitU16Min = 0x0400
	g.syncableBitU16Max = 0x0400

	// underworld rooms:
	for room := uint16(0x000); room < 0x128; room++ {
		g.underworld[room].InitFrom(g, room)
	}

	// when Agahnim is defeated, open HC portal for any remote players in that
	// overworld area:
	// u16[$7ef040] |= 0b00001000_00000000 Agahnim
	g.underworld[0x20].OnUpdated = func(s *syncableUnderworld, a *asm.Emitter, initial, updated uint16) {
		// asm runs in 16-bit mode (REP #$30) by default for underworld sync.
		if initial&0b00001000_00000000 != 0 || updated&0b00001000_00000000 == 0 {
			return
		}
		a.Comment("check if in HC overworld:")
		a.SEP(0x30)

		// check if in dungeon:
		a.LDA_dp(0x1B)
		a.BNE_imm8(0x6F - 0x06) // exit
		// check if in HC overworld:
		a.LDA_dp(0x8A)
		a.CMP_imm8_b(0x1B)
		a.BNE_imm8(0x6F - 0x0C) // exit

		a.Comment("find free sprite slot:")
		a.LDX_imm8_b(0x0f)      //   LDX   #$0F
		_ = 0                   // loop:
		a.LDA_abs_x(0x0DD0)     //   LDA.w $0DD0,X
		a.BEQ_imm8(0x05)        //   BEQ   found
		a.DEX()                 //   DEX
		a.BPL_imm8(-8)          //   BPL   loop
		a.BRA_imm8(0x6F - 0x18) //   BRA   exit
		_ = 0                   // found:

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

	// Swamp Palace:
	//                   fedcba98_76543210
	// u16[$7ef06e] |= 0b00000000_10000000
	g.underworld[0x37].BitNames[0x7] = "SP right floodgate"
	g.underworld[0x37].Verbs[0x7] = "flooded"

	//                   fedcba98_76543210
	// u16[$7ef06a] |= 0b00000000_10000000
	g.underworld[0x35].BitNames[0x7] = "SP left floodgate"
	g.underworld[0x35].Verbs[0x7] = "flooded"

	g.setUnderworldSyncMasks()

	// overworld areas:
	for offs := uint16(0x280); offs < 0x340; offs++ {
		g.overworld[offs-0x280] = games.SyncableBitU8{
			SyncableGame: g,
			Offset:       uint32(offs),
			IsEnabledPtr: &g.SyncUnderworld,
			BitNames:     nil,
			OnUpdated:    nil,
			SyncMask:     0xFF,
		}
	}

	// Pyramid bat crash: ([$7EF2DB] | 0x20)
	g.overworld[0x5B].OnUpdated = func(s *games.SyncableBitU8, a *asm.Emitter, initial, updated uint8) {
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
		g.underworld[room].SyncMask = mask
	}

	// desync swamp inner watergate at $7EF06A (supertile $35):
	g.underworld[0x035].SyncMask &= ^uint16(0x0080)
}

func (g *Game) openDoor(a *asm.Emitter, initial, updated uint16) bool {
	// must only be in dungeon module:
	if !g.LocalPlayer().IsDungeon() {
		return false
	}
	if g.LocalPlayer().SubModule != 0 {
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

	a.Comment(fmt.Sprintf("open door[%d] %s", k, kind))
	a.REP(0x30)
	a.LDA_imm16_w(doorTilemapAddr)
	a.STA_abs(0x068E)
	a.LDA_imm16_w(0x0008) // TODO: confirm this value?
	a.STA_abs(0x0690)
	a.SEP(0x30)
	// set door open submodule:
	a.LDA_imm8_b(0x04)
	a.STA_dp(0x11)
	a.REP(0x30)
	return true
}
