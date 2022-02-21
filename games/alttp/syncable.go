package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"log"
	"o2/games"
	"strings"
)

func bin16_4(v uint16) string {
	return fmt.Sprintf("0b%04b_%04b_%04b_%04b", v>>12&0xF, v>>8&0xF, v>>4&0xF, v&0xF)
}

func bin8_4(v uint8) string {
	return fmt.Sprintf("0b%04b_%04b", v>>4&0xF, v&0xF)
}

func (g *Game) NewSyncable(offset uint16, s games.SyncStrategy) games.SyncStrategy {
	g.syncableItems[offset] = s
	if offset < g.syncableItemsMin {
		g.syncableItemsMin = offset
	}
	if offset > g.syncableItemsMax {
		g.syncableItemsMax = offset
	}
	return s
}

func (g *Game) NewSyncableBitU8(offset uint16, enabled *bool, names [8]string, onUpdated games.SyncableBitU8OnUpdated) *games.SyncableBitU8 {
	s := games.NewSyncableBitU8(
		g,
		uint32(offset),
		enabled,
		names,
		onUpdated,
	)
	g.NewSyncable(offset, s)
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
	g.NewSyncable(offset, s)
	return s
}

func (g *Game) NewSyncableCustomU8(offset uint16, enabled *bool, generateUpdate games.SyncableCustomU8Update) *games.SyncableCustomU8 {
	s := games.NewSyncableCustomU8(
		g,
		uint32(offset),
		enabled,
		generateUpdate,
	)
	g.NewSyncable(offset, s)
	return s
}

func (g *Game) NewSyncableBitU16(offset uint16, enabled *bool, names [16]string, onUpdated games.SyncableBitU16OnUpdated) *games.SyncableBitU16 {
	s := games.NewSyncableBitU16(
		g,
		uint32(offset),
		enabled,
		names,
		onUpdated,
	)
	g.NewSyncable(offset, s)
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
	if verbs, ok := vanillaItemBitVerbs[offset]; ok {
		copyNonEmpties(s.Verbs[:], verbs[:])
	}
	g.NewSyncable(offset, s)
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
	s.Verbs = make([]string, len(s.ValueNames))
	if verbs, ok := vanillaItemVerbs[offset]; ok {
		copyNonEmpties(s.Verbs[:], verbs[:])
	}
	g.NewSyncable(offset, s)
	return s
}

func (g *Game) NewSyncableVTItemBitsU8(offset uint16, enabled *bool, onUpdated games.SyncableBitU8OnUpdated) *games.SyncableBitU8 {
	s := games.NewSyncableBitU8(
		g,
		uint32(offset),
		enabled,
		vtItemBitNames[offset],
		onUpdated,
	)
	if verbs, ok := vtItemBitVerbs[offset]; ok {
		copyNonEmpties(s.Verbs[:], verbs[:])
	}
	g.NewSyncable(offset, s)
	return s
}

type syncableBottle struct {
	g games.SyncableGame

	offset    uint32
	isEnabled *bool
	names     []string

	notification string
}

func (g *Game) NewSyncableBottle(offset uint16, enabled *bool) *syncableBottle {
	s := &syncableBottle{
		g:         g,
		offset:    uint32(offset),
		isEnabled: enabled,
		names:     vanillaBottleItemNames,
	}
	g.NewSyncable(offset, s)
	return s
}

func (s *syncableBottle) Size() uint      { return 1 }
func (s *syncableBottle) IsEnabled() bool { return *s.isEnabled }

func (s *syncableBottle) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.notification != "" {
		s.g.PushNotification(s.notification)
		s.notification = ""
	}
}

func (s *syncableBottle) GenerateUpdate(a *asm.Emitter, index uint32) bool {
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
	s.notification = ""
	if s.names != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.names) {
			if s.names[i] != "" {
				received := s.names[i]
				s.notification = fmt.Sprintf("got %s from %s", received, maxP.Name())
				a.Comment(s.notification + ":")
			}
		}
	}
	if s.notification == "" {
		a.Comment(fmt.Sprintf("got bottle value %#02x from %s:", maxV, maxP.Name()))
	}

	longAddr := localSRAM.BusAddress(offset)

	a.Comment(fmt.Sprintf("u8 [$%06x]: $%02x -> $%02x", longAddr, initial, maxV))

	failLabel := fmt.Sprintf("fail%06x", longAddr)
	nextLabel := fmt.Sprintf("next%06x", longAddr)

	a.LDA_long(longAddr)
	a.CMP_imm8_b(initial)
	a.BNE(failLabel)

	a.LDA_imm8_b(maxV)
	a.STA_long(longAddr)

	// write confirmation:
	a.Comment(fmt.Sprintf("write confirmation for #%d:", index))
	a.LDA_imm8_b(0x01)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.BRA(nextLabel)

	a.Label(failLabel)
	// write failure:
	a.Comment(fmt.Sprintf("write failure for #%d:", index))
	a.LDA_imm8_b(0x00)
	a.STA_long(a.GetBase() + 0x02 + index)

	a.Label(nextLabel)

	return true
}

func (s *syncableBottle) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []games.NotificationStatement) {
	base := uint32(0xF000)

	curr := wramCurrent[base+s.offset]
	prev := wramPrevious[base+s.offset]
	if curr == prev {
		return
	}

	longAddr := s.g.LocalSyncablePlayer().ReadableMemory(games.SRAM).BusAddress(s.offset)
	log.Printf("alttp: local: u8 [$%06x]: $%02x -> $%02x\n", longAddr, prev, curr)

	if s.names == nil {
		return
	}

	i := int(curr) - 1
	if i >= 0 && i < len(s.names) {
		if s.names[i] != "" {
			notifications = []games.NotificationStatement{
				{Items: s.names[i : i+1], Verb: "picked up"},
			}
		}
	}

	return
}

type syncableUnderworldOnUpdated func(s *syncableUnderworld, asm *asm.Emitter, initial, updated uint16)

type syncableUnderworld struct {
	games.SyncableGame

	Room uint16

	Offset   uint32
	SyncMask uint16

	IsEnabledPtr *bool
	BitNames     [16]string
	Verbs        [16]string

	games.PlayerPredicate

	OnUpdated syncableUnderworldOnUpdated

	Notification string
}

func (s *syncableUnderworld) InitFrom(g *Game, room uint16) {
	s.SyncableGame = g
	s.Room = room
	s.Offset = uint32(room << 1)
	s.IsEnabledPtr = &g.SyncUnderworld
	s.SyncMask = 0xFFFF
	s.PlayerPredicate = games.PlayerPredicateIdentity

	// name the boss in this underworld room:
	if bossName, ok := underworldBossNames[room]; ok {
		// e.g. u16[$7ef190] |= 0b00001000_00000000 Boss Defeated
		s.BitNames[0xb] = bossName
		s.Verbs[0xb] = "defeated"
	}
}

func (s *syncableUnderworld) Size() uint      { return 2 }
func (s *syncableUnderworld) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *syncableUnderworld) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *syncableUnderworld) GenerateUpdate(a *asm.Emitter, index uint32) bool {
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
	s.Notification = ""

	longAddr := local.ReadableMemory(games.SRAM).BusAddress(offs)
	newBits := updated & ^initial

	a.Comment(fmt.Sprintf("underworld room state changed: $%03x '%s'", s.Room, underworldNames[s.Room]))
	//a.Comment("                  dddd_bkut_sehc_qqqq")
	//a.Comment(fmt.Sprintf("u16[$%06x] |= 0b%04b_%04b_%04b_%04b", longAddr, newBits>>12&0xF, newBits>>8&0xF, newBits>>4&0xF, newBits&0xF))

	{
		received := make([]string, 0, len(s.BitNames))
		k := uint16(1)
		for i := 0; i < len(s.BitNames); i++ {
			if initial&k == 0 && updated&k == k {
				if s.BitNames[i] != "" {
					item := fmt.Sprintf("%s %s from %s", s.BitNames[i], s.Verbs[i], receivedFrom[i])
					received = append(received, item)
				}
			}
			k <<= 1
		}
		if len(received) > 0 {
			s.Notification = fmt.Sprintf("got %s", strings.Join(received, ", "))
			a.Comment(s.Notification + ":")
		}
	}

	a.Comment(fmt.Sprintf("u16[$%06x] = %s | %s", longAddr, bin16_4(initial), bin16_4(newBits)))

	goodLabel := fmt.Sprintf("good%06x", longAddr)
	failLabel := fmt.Sprintf("fail%06x", longAddr)
	nextLabel := fmt.Sprintf("next%06x", longAddr)
	a.LDA_long(longAddr)
	a.CMP_imm16_w(initial)
	a.BEQ(goodLabel)
	a.JMP_abs(failLabel)

	a.Label(goodLabel)
	a.ORA_imm16_w(newBits)
	a.STA_long(longAddr)

	if s.OnUpdated != nil {
		s.OnUpdated(s, a, initial, updated)
	}

	// write confirmation:
	a.Comment(fmt.Sprintf("write confirmation for #%d:", index))
	a.SEP(0x30)
	a.LDA_imm8_b(0x01)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x30)

	{
		// this cast should not fail:
		g := s.SyncableGame.(*Game)
		localPlayer := g.LocalPlayer()

		// local player must only be in dungeon module:
		if localPlayer.IsDungeon() {
			// only pay attention to supertile state changes when the local player is in that supertile:
			if s.Room == localPlayer.DungeonRoom {
				// open the door for the local player:
				g.openDoor(a, initial, updated)
			}
		}
	}
	a.BRA(nextLabel)

	a.Label(failLabel)
	// write failure:
	a.Comment(fmt.Sprintf("write failure for #%d:", index))
	a.SEP(0x30)
	a.LDA_imm8_b(0x00)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x30)

	a.Label(nextLabel)

	return true
}

func (s *syncableUnderworld) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []games.NotificationStatement) {
	base := uint32(0xF000)

	curr := binary.LittleEndian.Uint16(wramCurrent[base+s.Offset : base+s.Offset+2])
	prev := binary.LittleEndian.Uint16(wramPrevious[base+s.Offset : base+s.Offset+2])
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(games.SRAM).BusAddress(s.Offset)
	log.Printf("alttp: local: u16[$%06x]: %s -> %s ; %s\n", longAddr, bin16_4(prev), bin16_4(curr), underworldNames[s.Room])

	k := uint16(1)
	for i := 0; i < len(s.BitNames); i++ {
		if prev&k == 0 && curr&k == k {
			if s.BitNames[i] != "" {
				verb := s.Verbs[i]
				if verb == "" {
					verb = "enabled"
				}
				notifications = append(notifications, games.NotificationStatement{
					Items:    []string{s.BitNames[i]},
					Verb:     verb,
					Location: underworldNames[s.Room],
				})
			}
		}
		k <<= 1
	}

	return
}

type (
	syncableOverworldOnUpdated func(s *syncableOverworld, asm *asm.Emitter, initial, updated uint8)
)

type syncableOverworld struct {
	games.SyncableGame

	Offset uint32
	Area   uint16
	games.MemoryKind

	SyncMask     uint8
	IsEnabledPtr *bool

	BitNames [8]string
	Verbs    [8]string

	games.PlayerPredicate

	OnUpdated    syncableOverworldOnUpdated
	Notification string
}

func (s *syncableOverworld) InitFrom(g *Game, area uint16) {
	s.SyncableGame = g
	s.Offset = uint32(0x280 + area)
	s.Area = area
	s.MemoryKind = games.SRAM
	s.IsEnabledPtr = &g.SyncOverworld
	s.SyncMask = 0xFF
	s.BitNames = [8]string{
		"",
		"bomb wall",
		"",
		"",
		"",
		"overlay",
		"item",
		"",
	}
	s.Verbs = [8]string{
		"",
		"opened",
		"",
		"",
		"",
		"activated",
		"collected",
		"",
	}

	if names, ok := overworldBitNames[area]; ok {
		copyNonEmpties(s.BitNames[:], names[:])
	}
	if verbs, ok := overworldBitVerbs[area]; ok {
		copyNonEmpties(s.Verbs[:], verbs[:])
	}
}

func copyNonEmpties(dst []string, src []string) {
	for i := range dst {
		if src[i] == "" {
			continue
		}
		dst[i] = src[i]
	}
}

func (s *syncableOverworld) Size() uint      { return 1 }
func (s *syncableOverworld) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *syncableOverworld) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *syncableOverworld) GenerateUpdate(a *asm.Emitter, index uint32) bool {
	g := s.SyncableGame
	local := g.LocalSyncablePlayer()

	if s.PlayerPredicate != nil && !s.PlayerPredicate(local) {
		return false
	}

	offs := s.Offset

	initial := local.ReadableMemory(s.MemoryKind).ReadU8(offs)
	var receivedFrom [8]string

	updated := initial
	for _, p := range g.RemoteSyncablePlayers() {
		if s.PlayerPredicate != nil && !s.PlayerPredicate(p) {
			continue
		}

		v := p.ReadableMemory(s.MemoryKind).ReadU8(offs)
		v &= s.SyncMask

		newBits := v & ^updated
		if newBits != 0 {
			k := uint8(1)
			for i := 0; i < 8; i++ {
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
	s.Notification = ""

	longAddr := local.ReadableMemory(s.MemoryKind).BusAddress(offs)
	newBits := updated & ^initial

	{
		received := make([]string, 0, len(s.BitNames))
		k := uint8(1)
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
			a.Comment(s.Notification + ":")
		}
	}

	a.Comment(fmt.Sprintf("u8 [$%06x] = %s | %s", longAddr, bin8_4(initial), bin8_4(newBits)))

	failLabel := fmt.Sprintf("fail%06x", longAddr)
	nextLabel := fmt.Sprintf("next%06x", longAddr)
	a.LDA_long(longAddr)
	a.CMP_imm8_b(initial)
	a.BNE(failLabel)

	a.ORA_imm8_b(newBits)
	a.STA_long(longAddr)

	if s.OnUpdated != nil {
		s.OnUpdated(s, a, initial, updated)
	}

	// write confirmation:
	a.Comment(fmt.Sprintf("write confirmation for #%d:", index))
	a.LDA_imm8_b(0x01)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.BRA(nextLabel)

	a.Label(failLabel)
	// write failure:
	a.Comment(fmt.Sprintf("write failure for #%d:", index))
	a.LDA_imm8_b(0x00)
	a.STA_long(a.GetBase() + 0x02 + index)

	a.Label(nextLabel)

	return true
}

func (s *syncableOverworld) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []games.NotificationStatement) {
	base := uint32(0xF000)

	curr := wramCurrent[base+s.Offset]
	prev := wramPrevious[base+s.Offset]
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(s.MemoryKind).BusAddress(s.Offset)
	log.Printf("alttp: local: u8 [$%06x]: %s -> %s ; %s\n", longAddr, bin8_4(prev), bin8_4(curr), overworldNames[s.Area])

	k := uint8(1)
	for i := 0; i < len(s.BitNames); i++ {
		if prev&k == 0 && curr&k == k {
			if s.BitNames[i] != "" {
				verb := s.Verbs[i]
				if verb == "" {
					verb = "enabled"
				}
				notifications = append(notifications, games.NotificationStatement{
					Items:    []string{s.BitNames[i]},
					Verb:     verb,
					Location: overworldNames[s.Area],
				})
			}
		}
		k <<= 1
	}

	return
}
