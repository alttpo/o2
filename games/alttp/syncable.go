package alttp

import (
	"fmt"
	"o2/games"
	"o2/snes/asm"
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
