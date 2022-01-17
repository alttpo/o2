package alttp

import (
	"fmt"
	"o2/games"
	"o2/snes/asm"
	"strings"
)

func (g *Game) NewSyncableBitU8(offset uint16, enabled *bool, names []string, onUpdated games.SyncableBitU8OnUpdated) *games.SyncableBitU8 {
	s := games.NewSyncableBitU8(
		g,
		uint32(offset),
		enabled,
		names,
		onUpdated,
	)
	g.syncableItems[offset] = s
	return s
}

func (g *Game) NewSyncableMaxU8(offset uint16, enabled *bool, names []string, onUpdated games.SyncableMaxU8OnUpdated) *games.SyncableMaxU8 {
	s := games.NewSyncableMaxU8(
		g,
		uint32(offset),
		enabled,
		names,
		onUpdated,
	)
	g.syncableItems[offset] = s
	return s
}

func (g *Game) NewSyncableCustomU8(offset uint16, enabled *bool, generateUpdate games.SyncableCustomU8Update) *games.SyncableCustomU8 {
	s := games.NewSyncableCustomU8(
		g,
		uint32(offset),
		enabled,
		generateUpdate,
	)
	g.syncableItems[offset] = s
	return s
}

func (g *Game) NewSyncableBitU16(offset uint16, enabled *bool, names []string, onUpdated games.SyncableBitU16OnUpdated) *games.SyncableBitU16 {
	s := games.NewSyncableBitU16(
		g,
		uint32(offset),
		enabled,
		names,
		onUpdated,
	)
	g.syncableItems[offset] = s
	return s
}

func (g *Game) NewSyncableVanillaItemBitsU8(offset uint16, enabled *bool, onUpdated games.SyncableBitU8OnUpdated) *games.SyncableBitU8 {
	s := games.NewSyncableBitU8(
		g,
		uint32(offset),
		enabled,
		vanillaItemBitNames[offset],
		onUpdated,
	)
	g.syncableItems[offset] = s
	return s
}

func (g *Game) NewSyncableVanillaItemU8(offset uint16, enabled *bool, onUpdated games.SyncableMaxU8OnUpdated) *games.SyncableMaxU8 {
	s := games.NewSyncableMaxU8(
		g,
		uint32(offset),
		enabled,
		vanillaItemNames[offset],
		onUpdated,
	)
	g.syncableItems[offset] = s
	return s
}

type syncableBottle struct {
	g games.SyncableGame

	offset    uint32
	isEnabled *bool
	names     []string

	pendingUpdate bool
	updatingTo    byte
	notification  string
}

func (g *Game) newSyncableBottle(offset uint16, enabled *bool) *syncableBottle {
	s := &syncableBottle{
		g:         g,
		offset:    uint32(offset),
		isEnabled: enabled,
		names:     vanillaBottleItemNames,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableBottle) Size() uint      { return 1 }
func (s *syncableBottle) IsEnabled() bool { return *s.isEnabled }

func (s *syncableBottle) CanUpdate() bool {
	if !s.pendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.g
	if g.LocalSyncablePlayer().ReadableMemory(games.SRAM).ReadU8(s.offset) != s.updatingTo {
		return false
	}

	// send the notification:
	if s.notification != "" {
		g.PushNotification(s.notification)
		s.notification = ""
	}

	s.pendingUpdate = false

	return true
}

func (s *syncableBottle) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.LocalSyncablePlayer()
	offset := s.offset

	localSRAM := local.ReadableMemory(games.SRAM)
	initial := localSRAM.ReadU8(offset)
	if initial >= 2 {
		// don't change existing bottle contents:
		return false
	}

	// max() is an odd choice here but something has to reconcile any differences among
	// all remote players that have this bottle slot filled.
	maxP := local
	maxV := initial
	for _, p := range g.RemoteSyncablePlayers() {
		v := p.ReadableMemory(games.SRAM).ReadU8(offset)
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
	s.pendingUpdate = true
	s.updatingTo = maxV
	s.notification = ""
	if s.names != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.names) {
			if s.names[i] != "" {
				received := s.names[i]
				s.notification = fmt.Sprintf("got %s from %s", received, maxP.Name())
				asm.Comment(s.notification + ":")
			}
		}
	}
	if s.notification == "" {
		asm.Comment(fmt.Sprintf("got bottle value %#02x from %s:", maxV, maxP.Name()))
	}

	asm.LDA_imm8_b(maxV)
	asm.STA_long(localSRAM.BusAddress(offset))

	return true
}

type syncableUnderworldOnUpdated func(s *syncableUnderworld, asm *asm.Emitter, initial, updated uint16)

type syncableUnderworld struct {
	games.SyncableGame

	Room uint16

	Offset   uint32
	SyncMask uint16

	IsEnabledPtr *bool
	BitNames     []string

	games.PlayerPredicate

	OnUpdated syncableUnderworldOnUpdated

	PendingUpdate bool
	UpdatingTo    uint16
	Notification  string
}

func (s *syncableUnderworld) Size() uint      { return 2 }
func (s *syncableUnderworld) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *syncableUnderworld) CanUpdate() bool {
	if !s.PendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.SyncableGame
	if g.LocalSyncablePlayer().ReadableMemory(games.SRAM).ReadU16(s.Offset) != s.UpdatingTo {
		return false
	}

	// send the notification:
	if s.Notification != "" {
		g.PushNotification(s.Notification)
		s.Notification = ""
	}

	s.PendingUpdate = false

	return true
}

func (s *syncableUnderworld) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.SyncableGame
	local := g.LocalSyncablePlayer()

	// filter out local player:
	if s.PlayerPredicate != nil && !s.PlayerPredicate(local) {
		return false
	}

	offs := s.Offset
	mask := s.SyncMask

	initial := local.ReadableMemory(games.SRAM).ReadU16(offs)
	var receivedFrom [16]string

	updated := initial
	for _, p := range g.RemoteSyncablePlayers() {
		// filter out player:
		if s.PlayerPredicate != nil && !s.PlayerPredicate(p) {
			continue
		}

		// read player data:
		v := p.ReadableMemory(games.SRAM).ReadU16(offs)
		v &= mask

		newBits := v & ^updated

		// attribute this bit to this player:
		if newBits != 0 {
			k := uint16(1)
			for i := 0; i < 16; i++ {
				if newBits&k == k {
					receivedFrom[i] = p.Name()
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
	s.PendingUpdate = true
	s.UpdatingTo = updated
	s.Notification = ""

	longAddr := local.ReadableMemory(games.SRAM).BusAddress(offs)
	newBits := updated & ^initial

	asm.Comment(fmt.Sprintf("underworld room state changed: $%03x '%s'", s.Room, underworldNames[s.Room]))
	asm.Comment("                  dddd_bkut_sehc_qqqq")
	asm.Comment(fmt.Sprintf("u16[$%06x] |= 0b%04b_%04b_%04b_%04b", longAddr, newBits>>12&0xF, newBits>>8&0xF, newBits>>4&0xF, newBits&0xF))

	if s.BitNames != nil {
		received := make([]string, 0, len(s.BitNames))
		k := uint16(1)
		for i := 0; i < len(s.BitNames); i++ {
			if initial&k == 0 && updated&k == k {
				if s.BitNames[i] != "" {
					item := fmt.Sprintf("%s from %s", s.BitNames[i], receivedFrom[i])
					received = append(received, item)
				}
			}
			k <<= 1
		}
		if len(received) > 0 {
			s.Notification = fmt.Sprintf("got %s", strings.Join(received, ", "))
			asm.Comment(s.Notification + ":")
		}
	}

	asm.LDA_imm16_w(newBits)
	asm.ORA_long(longAddr)
	asm.STA_long(longAddr)

	{
		// this cast should not fail:
		g := s.SyncableGame.(*Game)
		localPlayer := g.LocalPlayer()

		// local player must only be in dungeon module:
		if localPlayer.IsDungeon() {
			// only pay attention to supertile state changes when the local player is in that supertile:
			if s.Room == localPlayer.DungeonRoom {
				// open the door for the local player:
				g.openDoor(asm, initial, updated)
			}
		}
	}

	if s.OnUpdated != nil {
		s.OnUpdated(s, asm, initial, updated)
	}

	return true
}

func (s *syncableUnderworld) InitFrom(g *Game, room uint16) {
	s.SyncableGame = g
	s.Room = room
	s.Offset = uint32(room << 1)
	s.IsEnabledPtr = &g.SyncUnderworld
	s.SyncMask = 0xFFFF
	s.PlayerPredicate = games.PlayerPredicateIdentity
	s.BitNames = nil

	// name the boss in this underworld room:
	if bossName, ok := underworldBossNames[room]; ok {
		// e.g. u16[$7ef190] |= 0b00001000_00000000 Boss Defeated
		s.BitNames = make([]string, 16)
		s.BitNames[0xb] = fmt.Sprintf("%s defeated", bossName)
	}
}
