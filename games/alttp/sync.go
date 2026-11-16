package alttp

import (
	"fmt"
	"o2/games"

	"github.com/alttpo/snes/asm"
)

var modulesOKForSync = map[uint8]struct{}{
	0x07: {},
	0x09: {},
	0x0B: {},
	0x0C: {}, // unused
	0x0D: {}, // unused
	0x0E: {},
	0x0F: {},
	0x10: {},
	0x11: {},
	0x12: {},
	0x13: {},
	0x15: {},
	0x16: {},
	0x17: {},
	0x18: {},
}

// for VT randomizers:
// InventorySwap1 $38C
const (
	IS1FluteActive uint8 = 1 << iota
	IS1FluteInactive
	IS1Shovel
	IS1MushroomPast
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
	g.syncable = [0x10000]games.SyncStrategy{}
	g.syncableOffsMin = 0x1FFFF
	g.syncableOffsMax = 0x00000

	// don't set WRAM timestamps on first read from SNES:
	g.notFirstWRAMRead = false

	// WRAM offsets for small keys, crystal switches, etc:
	g.initSmallKeysSync()
	g.initHeartsSync()
	g.resetCrystalSwitch()
	g.resetStarSwitch()
	g.resetRoomState()

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
			// don't decompress gfx twice:
			if _, ok := g.generated[0xF35A]; ok {
				return
			}
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

	// heart containers and heart pieces ($36B, $36C) are synced as deltas; see synchearts.go

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
			newBits := updated & ^initial

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

			const fluteMask = IS1FluteInactive | IS1FluteActive

			// shovel/flute: ; $01 = Shovel | $02 = Inactive Flute | $03 = Active Flute
			if initial&fluteMask != IS1FluteActive && newBits&fluteMask == IS1FluteActive {
				// flute (activated):
				a.Comment("set Flute (active) in inventory:")
				a.LDA_long(0x7EF34C)
				a.CMP_imm8_b(1) // is shovel?
				a.BEQ_imm8(6)   // yes, dont override
				a.LDA_imm8_b(3) // flute (active)
				a.STA_long(0x7EF34C)
			} else if initial&fluteMask == 0 && newBits&fluteMask == IS1FluteInactive {
				// flute (inactive):
				a.Comment("set Flute (inactive) in inventory:")
				a.LDA_long(0x7EF34C)
				a.CMP_imm8_b(1) // is shovel?
				a.BEQ_imm8(6)   // yes, dont override
				a.LDA_imm8_b(2) // flue (inactive)
				a.STA_long(0x7EF34C)
			} else if initial&IS1Shovel == 0 && newBits&IS1Shovel != 0 {
				// shovel:
				a.Comment("set Shovel in inventory:")
				a.LDA_long(0x7EF34C)
				a.BNE_imm8(6)   // only set if nothing in inventory:
				a.LDA_imm8_b(1) // shovel
				a.STA_long(0x7EF34C)
			}

			// red/blue boomerang:
			if initial&IS1RedBoomerang == 0 && updated&IS1RedBoomerang != 0 {
				// set red boomerang in inventory:
				a.Comment("set Red Boomerang in inventory:")
				a.LDA_long(0x7EF341)
				a.BNE_imm8(6)
				a.LDA_imm8_b(2)
				a.STA_long(0x7EF341)
			} else if initial&IS1BlueBoomerang == 0 && updated&IS1BlueBoomerang != 0 {
				// set blue boomerang in inventory:
				a.Comment("set Blue Boomerang in inventory:")
				a.LDA_long(0x7EF341)
				a.BNE_imm8(6)
				a.LDA_imm8_b(1)
				a.STA_long(0x7EF341)
			}
		})
		invSwap1.GenerateAsm = func(s *games.SyncableBitU8, a *asm.Emitter, initial, updated, newBits uint8) {
			const longAddr = 0x7EF38C

			const fluteMask = IS1FluteInactive | IS1FluteActive

			nonFluteNewBits := newBits & ^fluteMask
			orBits := nonFluteNewBits
			modifyMask := uint8(0xFF)

			modificationNeeded := nonFluteNewBits != 0

			// transition flute only from inactive/missing to active:
			if initial&fluteMask != IS1FluteActive && newBits&fluteMask == IS1FluteActive {
				modificationNeeded = true
				modifyMask = ^fluteMask
				// turn on active flute:
				orBits |= IS1FluteActive
			} else if initial&fluteMask == 0 && newBits&fluteMask == IS1FluteInactive {
				modificationNeeded = true
				modifyMask = ^fluteMask
				// turn on inactive flute:
				orBits |= IS1FluteInactive
			}

			if modificationNeeded {
				a.LDA_long(longAddr)
				if modifyMask != 0xFF {
					a.AND_imm8_b(modifyMask)
				}
				a.ORA_imm8_b(orBits)
				a.STA_long(longAddr)
			}
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
	g.NewSyncableCustomU8(0x3C6, &g.SyncProgress, func(s *games.SyncableCustomU8, newEmitter func() *asm.Emitter, index uint32) (isUpdated bool, a *asm.Emitter) {
		offset := s.Offset
		local := s.SyncableGame.LocalSyncablePlayer()
		localSRAM := local.ReadableMemory(games.SRAM)
		if !localSRAM.IsFresh(s.Offset) {
			return
		}

		initial := localSRAM.ReadU8(offset)

		// check to make sure zelda telepathic follower removed if have uncle's gear:
		if initial&0x01 == 0x01 && localSRAM.IsFresh(0x3CC) && localSRAM.ReadU8(0x3CC) == 0x05 {
			isUpdated, a = true, newEmitter()

			a.Comment("already have uncle's gear; remove telepathic zelda follower:")
			a.LDA_long(0x7EF3CC)
			a.CMP_imm8_b(0x05)
			a.BNE_imm8(0x06)
			a.LDA_imm8_b(0x00)   // 2 bytes
			a.STA_long(0x7EF3CC) // 4 bytes

			// write confirmation:
			a.LDA_imm8_b(0x01)
			a.STA_long(a.GetBase() + 0x02 + index)

			return
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
			return
		}

		isUpdated, a = true, newEmitter()

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

		return
	})

	// map markers:
	g.NewSyncableVanillaItemU8(0x3C7, &g.SyncProgress, nil)

	// skip 0x3C8 start at location

	// progress flags 2/2:
	g.NewSyncableCustomU8(0x3C9, &g.SyncProgress, func(s *games.SyncableCustomU8, newEmitter func() *asm.Emitter, index uint32) (isUpdated bool, a *asm.Emitter) {
		offset := s.Offset
		localMemory := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(games.SRAM)
		if !localMemory.IsFresh(s.Offset) {
			return
		}

		initial := localMemory.ReadU8(offset)

		newBits := initial
		for _, p := range g.RemoteSyncablePlayers() {
			v := p.ReadableMemory(games.SRAM).ReadU8(offset)
			newBits |= v
		}

		if newBits == initial {
			// no change:
			return
		}

		isUpdated, a = true, newEmitter()

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

		return
	})

	if g.isVTRandomizer() {
		// NPC flags:
		g.NewSyncableMaxU8(0x410, &g.SyncProgress, nil, nil)
		g.NewSyncableMaxU8(0x411, &g.SyncProgress, nil, nil)
		// programmable items:
		// TODO: these should be U16
		g.NewSyncableMaxU8(0x41A, &g.SyncItems, nil, nil)
		g.NewSyncableMaxU8(0x41C, &g.SyncItems, nil, nil)
		g.NewSyncableMaxU8(0x41E, &g.SyncItems, nil, nil)

		// Progressive item counters:
		// sword:
		g.NewSyncableMaxU8(0x417, &g.SyncItems, nil, nil)
		// goal item counter:
		g.NewSyncableMaxU8(0x418, &g.SyncItems, nil, nil)
		// shield:
		g.NewSyncableMaxU8(0x422, &g.SyncItems, nil, func(s *games.SyncableMaxU8, a *asm.Emitter, initial, updated uint8) {
			if updated <= initial {
				return
			}

			shieldNames := vanillaItemNames[0x35A]
			shieldName := "progressive shield update"
			if int(updated)-1 < len(shieldNames) {
				shieldName = shieldNames[int(updated)-1]
				a.Comment(fmt.Sprintf("progressive upgrade shield to %s:", shieldName))
			} else {
				a.Comment("progressive upgrade shield:")
			}

			//a.LDA_long(0x7ef422) // redundant to previous sta.l instruction
			a.STA_long(0x7ef35a)

			// don't decompress gfx twice:
			if _, ok := g.generated[0xF35A]; ok {
				return
			}
			s.Notification = fmt.Sprintf("got %s from %s", shieldName, s.PlayerWithMaxValue.Name())

			a.Comment("decompress shield gfx:")
			a.JSL(g.romFunctions[fnDecompGfxShield])
			a.Comment("update shield palette:")
			a.JSL(g.romFunctions[fnUpdatePaletteShield])
		})
		// mail / armor:
		g.NewSyncableMaxU8(0x46E, &g.SyncItems, nil, nil)

		// pendants counter:
		g.NewSyncableMaxU8(0x429, &g.SyncProgress, nil, nil)
		// crystals counter:
		g.NewSyncableMaxU8(0x476, &g.SyncProgress, nil, nil)

		// dungeons completed:
		g.NewSyncableBitU16(0x472, &g.SyncProgress, [16]string{}, nil)

		// dungeon location checks: 0x4c0..0x4cd
		for d := 0; d < 0xe; d++ {
			g.NewSyncableMaxU8(uint16(0x4c0+d), &g.SyncProgress, nil, func(s *games.SyncableMaxU8, asm *asm.Emitter, initial, updated uint8) {
				s.Notification = fmt.Sprintf(
					"synced %s location checks = %d from %s",
					dungeonNames[s.Offset-0x4c0],
					updated,
					s.PlayerWithMaxValue.Name(),
				)
			})
		}
	}

	// underworld rooms:
	for room := uint16(0x000); room < 0x128; room++ {
		g.underworld[room].InitFrom(g, room)
	}

	// when Agahnim is defeated, open HC portal for any remote players in that
	// overworld area:
	// u16[$7ef040] |= 0b00001000_00000000 Agahnim
	g.underworld[0x20].OnUpdated = func(
		s *syncableUnderworld,
		a *asm.Emitter,
		initial, updated uint16,
		failLabel, nextLabel string,
	) {
		// asm runs in 16-bit mode (REP #$30) by default for underworld sync.
		if initial&0b00001000_00000000 != 0 || updated&0b00001000_00000000 == 0 {
			return
		}
		a.Comment("check if in HC overworld:")
		a.SEP(0x30)

		// check if in dungeon:
		a.LDA_dp(0x1B)
		a.BNE("hcportal_exit")
		// check if in HC overworld:
		a.LDA_dp(0x8A)
		a.CMP_imm8_b(0x1B)
		a.BNE("hcportal_exit")

		a.Comment("find free sprite slot:")
		a.LDX_imm8_b(0x0f)
		a.Label("hcportal_nextslot")
		a.LDA_abs_x(0x0DD0)
		a.BEQ("hcportal_open")
		a.DEX()
		a.BPL("hcportal_nextslot")
		a.BRA("hcportal_exit")

		a.Label("hcportal_open")
		a.Comment("open HC portal:")

		if true {
			// X = sprite to reset
			a.JSL(g.romFunctions[fnResetSpriteProperties])
		} else {
			// NOTE: this clearing of sprite properties is handled by ResetSpriteProperties:
			// zero filler / not important state:
			a.STZ_abs_x(0x0D40)
			a.STZ_abs_x(0x0D50)
			a.STZ_abs_x(0x0D60)
			a.STZ_abs_x(0x0D70)
			a.STZ_abs_x(0x0D80)

			a.STZ_abs_x(0x0DA0)
			a.STZ_abs_x(0x0DB0)
			a.STZ_abs_x(0x0DC0)

			a.STZ_abs_x(0x0DE0)
			a.STZ_abs_x(0x0DF0)
			a.STZ_abs_x(0x0E00)
			a.STZ_abs_x(0x0E10)

			a.STZ_abs_x(0x0E30)

			a.STZ_abs_x(0x0E50)

			a.STZ_abs_x(0x0E70)
			a.STZ_abs_x(0x0E80)
			a.STZ_abs_x(0x0E90)
			a.STZ_abs_x(0x0EA0)
			a.STZ_abs_x(0x0EB0)
			a.STZ_abs_x(0x0EC0)
			a.STZ_abs_x(0x0ED0)
			a.STZ_abs_x(0x0EE0)
			a.STZ_abs_x(0x0EF0)

			a.STZ_abs_x(0x0F10)
			a.STZ_abs_x(0x0F20)
			a.STZ_abs_x(0x0F30)
			a.STZ_abs_x(0x0F40)

			a.STZ_abs_x(0x0F80)
			a.STZ_abs_x(0x0F90)

			a.STZ_abs_x(0x0FC0)
			a.STZ_abs_x(0x0FD0)
			a.STZ_abs_x(0x0FE0)
			a.STZ_abs_x(0x0FF0)
		}
		// whirlpool sprite / CastleWarp:
		a.LDA_imm8_b(0xBA)
		a.STA_abs_x(0x0E20)
		// active, RE: CheckIfActive_bank1E
		a.LDA_imm8_b(0x09)
		a.STA_abs_x(0x0DD0)
		a.LDA_imm8_b(0x80)
		a.STA_abs_x(0x0CAA) // = $80
		a.STZ_abs_x(0x0F00) // = $00
		// harmless sprite:
		a.STA_abs_x(0x0E40) // = $80
		// Y:
		a.LDA_imm8_b(0x50)
		a.STA_abs_x(0x0D00)
		a.LDA_imm8_b(0x08)
		a.STA_abs_x(0x0D20)
		// X:
		a.LDA_imm8_b(0xE0)
		a.STA_abs_x(0x0D10)
		a.LDA_imm8_b(0x07)
		a.STA_abs_x(0x0D30)
		// gfx?
		a.LDA_imm8_b(0x01)
		a.STA_abs_x(0x0D90) // = $01
		// hitbox/persist:
		a.STA_abs_x(0x0F60) // = $01 ; important for screen coordinate check
		a.STZ_abs_x(0x0F70) // = $00 ; important for screen coordinate check
		// OAM:
		a.LDA_imm8_b(0x04)
		a.STA_abs_x(0x0F50) // = $04
		// 0E60 = 44
		a.LDA_imm8_b(0x44)
		a.STA_abs_x(0x0E60)
		// 0FA0 = 3E
		a.LDA_imm8_b(0x3E)
		a.STA_abs_x(0x0FA0) // = $3E
		// 0FB0 = 06
		a.LDA_imm8_b(0x06)
		a.STA_abs_x(0x0FB0) // = $06

		// exit:
		a.Label("hcportal_exit")
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
		g.overworld[offs-0x280].InitFrom(g, offs-0x280)
	}

	// disable overlay sync at "Great Swamp S" and "Swamp":
	g.overworld[0x3B].SyncMask = 0xDF
	g.overworld[0x7B].SyncMask = 0xDF

	// remove heart pieces and heart containers other players collected from our screen, and Agahnim's barrier:
	for i := range g.overworld {
		g.overworld[i].OnUpdated = g.removeCollectedHeartSprites
	}

	// draw overlays other players opened while we are in the area:
	for _, o := range []overlayEvent{
		// the weathervane in Kakariko Village, blown up by playing the flute next to it:
		{area: 0x18, fn: fnOverworldAlterWeathervane, notification: "weathervane destroyed"},
		// the Thieves' Town entrance, opened by pulling the grate (sprite $14):
		{area: 0x58, fn: fnOverworldOpenGargoylesDomain, sprite: 0x14, notification: "Thieves' Town opened"},
		// the pyramid hole, opened by the bat crash:
		{area: 0x5B, fn: fnOverworldCreatePyramidHole, notification: "pyramid hole opened"},
	} {
		o := o
		g.overworld[o.area].OnUpdated = func(s *syncableOverworld, a *asm.Emitter, initial, updated uint8) {
			g.drawOpenedOverlay(o, a, initial, updated)
			g.removeCollectedHeartSprites(s, a, initial, updated)
		}
	}
}

// overlayEvent is an overworld overlay drawn by a routine that sets bit $20 of the area's flags, without animation.
type overlayEvent struct {
	area         uint16
	fn           romFunction
	sprite       uint8 // sprite type to remove once opened, if not 0
	notification string
}

// drawOpenedOverlay draws the area's overlay with its routine when another player set bit $20 of the area's flags while
// the local player is in the area. The overlay is otherwise only loaded with the area, the next time it is entered.
func (g *Game) drawOpenedOverlay(o overlayEvent, a *asm.Emitter, initial, updated uint8) {
	const overlay = 0x20
	if initial&overlay != 0 || updated&overlay == 0 {
		return
	}
	if g.local.OverworldArea != o.area || g.local.Location&0x010000 != 0 {
		return
	}
	fn, ok := g.romFunctions[o.fn]
	if !ok {
		return
	}

	lbl := fmt.Sprintf("ow%02x_overlay", o.area)
	a.Comment(o.notification + " if still in this overworld area, in submodule 0:")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x09)
	a.BNE(lbl + "_done")
	a.LDA_dp(0x11)
	a.BNE(lbl + "_done")
	a.LDA_dp(0x1B)
	a.BNE(lbl + "_done")
	a.LDA_dp(0x8A)
	a.CMP_imm8_b(uint8(o.area))
	a.BNE(lbl + "_done")
	a.JSL(fn)
	if o.sprite != 0 {
		a.LDX_imm8_b(0x0F)
		a.Label(lbl + "_loop")
		a.LDA_abs_x(0x0E20)
		a.CMP_imm8_b(o.sprite)
		a.BNE(lbl + "_next")
		a.STZ_abs_x(0x0DD0)
		a.Label(lbl + "_next")
		a.DEX()
		a.BPL(lbl + "_loop")
	}
	a.Label(lbl + "_done")
}

// removeCollectedHeartSprites removes the heart piece ($EB) and heart container ($EA) sprites of the local player's
// overworld area once another player collected it. Picking one up sets bit $40 of the area's flags at $7EF280 (which
// SpritePrep_HeartContainer checks when the area loads), so the sprite would otherwise stay until the area reloads and
// could be collected twice. Destroying Agahnim's barrier (sprite $40, at Hyrule Castle) with the Master Sword sets the
// same bit, which SpritePrep_AgahnimsBarrier checks to draw it destroyed ($0DC0 = 4, which also makes it harmless).
func (g *Game) removeCollectedHeartSprites(s *syncableOverworld, a *asm.Emitter, initial, updated uint8) {
	const heartCollected = 0x40
	if initial&heartCollected != 0 || updated&heartCollected == 0 {
		return
	}
	if g.local.OverworldArea != s.Area {
		return
	}

	lbl := fmt.Sprintf("ow%02x_heart", s.Area)
	a.Comment("remove heart piece/container sprites and destroy the barrier if still in this overworld area:")
	a.LDA_dp(0x1B)
	a.BNE(lbl + "_done")
	a.LDA_dp(0x8A)
	a.CMP_imm8_b(uint8(s.Area))
	a.BNE(lbl + "_done")
	a.LDX_imm8_b(0x0F)
	a.Label(lbl + "_loop")
	a.LDA_abs_x(0x0DD0)
	a.BEQ(lbl + "_next")
	a.LDA_abs_x(0x0E20)
	a.CMP_imm8_b(0xEB)
	a.BEQ(lbl + "_kill")
	a.CMP_imm8_b(0xEA)
	a.BEQ(lbl + "_kill")
	a.CMP_imm8_b(0x40)
	a.BNE(lbl + "_next")
	a.LDA_imm8_b(0x04)
	a.STA_abs_x(0x0DC0)
	a.BRA(lbl + "_next")
	a.Label(lbl + "_kill")
	a.STZ_abs_x(0x0DD0)
	a.Label(lbl + "_next")
	a.DEX()
	a.BPL(lbl + "_loop")
	a.Label(lbl + "_done")
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

	// make sure to sync TT East Attic bombable floor bit for lighting up the Blind boss room:
	g.underworld[0x065].SyncMask |= uint16(0x0100)
}

func (g *Game) openDoor(a *asm.Emitter, initial, updated uint16) bool {
	// must only be in dungeon module:
	if !g.LocalPlayer().IsDungeon() {
		return false
	}
	if g.LocalPlayer().SubModule != 0 {
		return false
	}

	// only pay attention to door bits that were set; $7EF000 keeps the bits of door slots 0-3, like $0400:
	opened := uint8((updated &^ initial) >> 8)
	for b := uint8(0x80); b >= 0x10; b >>= 1 {
		if opened&b == 0 {
			continue
		}
		d, ok := g.roomKeyDoorFor(b)
		if !ok {
			continue
		}
		label := fmt.Sprintf("sram_door_%03x", g.local.DungeonRoom)
		a.Comment("unlock the door if no other submodule is running:")
		a.LDA_dp(0x11)
		a.AND_imm16_w(0x00FF)
		a.BNE(label)
		emitUnlockDoor(a, d, label+"_unlock")
		a.Label(label)
		return true
	}
	return false
}
