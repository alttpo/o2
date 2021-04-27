package alttp

import (
	"fmt"
	"o2/snes/asm"
	"strings"
)

type (
	playerPredicate func(p *Player) bool
	playerReadU16   func(p *Player, offs uint16) uint16
	longAddress     func(offs uint16) uint32

	syncableCustomU8Update  func(s *syncableCustomU8, asm *asm.Emitter) bool
	syncableBitU8OnUpdated  func(s *syncableBitU8, asm *asm.Emitter, initial, updated uint8)
	syncableBitU16OnUpdated func(s *syncableBitU16, asm *asm.Emitter, initial, updated uint16)
	syncableMaxU8OnUpdated  func(s *syncableMaxU8, asm *asm.Emitter, initial, updated uint8)
)

func playerPredicateIdentity(_ *Player) bool       { return true }
func playerReadSRAM(p *Player, offs uint16) uint16 { return p.sramU16(offs) }
func longAddressSRAM(offs uint16) uint32           { return 0x7EF000 + uint32(offs) }
func longAddressWRAM(offs uint16) uint32           { return 0x7E0000 + uint32(offs) }

type syncableCustomU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool

	generateUpdate syncableCustomU8Update

	pendingUpdate bool
	updatingTo    uint8
	notification  string
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

func (s *syncableCustomU8) CanUpdate() bool {
	if !s.pendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.g
	if g.local.SRAM[s.offset] != s.updatingTo {
		return false
	}

	// send the notification:
	if s.notification != "" {
		g.pushNotification(s.notification)
		s.notification = ""
	}

	s.pendingUpdate = false

	return true
}

type syncableBitU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string
	mask      uint8

	onUpdated syncableBitU8OnUpdated

	pendingUpdate bool
	updatingTo    uint8
	notification  string
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

func (s *syncableBitU8) CanUpdate() bool {
	if !s.pendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.g
	if g.local.SRAM[s.offset] != s.updatingTo {
		return false
	}

	// send the notification:
	if s.notification != "" {
		g.pushNotification(s.notification)
		s.notification = ""
	}

	s.pendingUpdate = false

	return true
}

func (s *syncableBitU8) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local
	offs := s.offset

	initial := local.SRAM[offs]
	var receivedFrom [8]string

	updated := initial
	for _, p := range g.ActivePlayers() {
		v := p.SRAM[offs]
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
	s.pendingUpdate = true
	s.updatingTo = updated

	longAddr := 0x7EF000 + uint32(offs)
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
		s.notification = fmt.Sprintf("got %s", strings.Join(received, ", "))
		asm.Comment(s.notification + ":")
	} else {
		asm.Comment(fmt.Sprintf("u8 [$%06x] |= %#08b", longAddr, newBits))
	}

	asm.LDA_imm8_b(newBits)
	asm.ORA_long(longAddr)
	asm.STA_long(longAddr)

	if s.onUpdated != nil {
		s.onUpdated(s, asm, initial, updated)
	}

	return true
}

type syncableBitU16 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string
	mask      uint16

	readU16         playerReadU16
	longAddress     longAddress
	playerPredicate playerPredicate
	onUpdated       syncableBitU16OnUpdated

	pendingUpdate bool
	updatingTo    uint16
	notification  string
}

func (g *Game) newSyncableBitU16(offset uint16, enabled *bool, names []string, onUpdated syncableBitU16OnUpdated) *syncableBitU16 {
	s := &syncableBitU16{
		g:               g,
		offset:          offset,
		isEnabled:       enabled,
		names:           names,
		mask:            0xFFFF,
		readU16:         playerReadSRAM,
		longAddress:     longAddressSRAM,
		playerPredicate: playerPredicateIdentity,
		onUpdated:       onUpdated,
	}
	g.syncableItems[offset] = s
	return s
}

func (s *syncableBitU16) Offset() uint16  { return s.offset }
func (s *syncableBitU16) Size() uint      { return 2 }
func (s *syncableBitU16) IsEnabled() bool { return *s.isEnabled }

func (s *syncableBitU16) CanUpdate() bool {
	if !s.pendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.g
	if g.local.sramU16(s.offset) != s.updatingTo {
		return false
	}

	// send the notification:
	if s.notification != "" {
		g.pushNotification(s.notification)
		s.notification = ""
	}

	s.pendingUpdate = false

	return true
}

func (s *syncableBitU16) GenerateUpdate(asm *asm.Emitter) bool {
	g := s.g
	local := g.local

	// filter out local player:
	if !s.playerPredicate(local) {
		return false
	}

	offs := s.offset
	mask := s.mask

	initial := s.readU16(local, offs)
	var receivedFrom [16]string

	updated := initial
	for _, p := range g.ActivePlayers() {
		// filter out player:
		if !s.playerPredicate(p) {
			continue
		}

		// read player data:
		v := s.readU16(p, offs)
		v &= mask

		newBits := v & ^updated

		// attribute this bit to this player:
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
	s.pendingUpdate = true
	s.updatingTo = updated

	longAddr := s.longAddress(offs)
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
		s.notification = fmt.Sprintf("got %s", strings.Join(received, ", "))
		asm.Comment(s.notification + ":")
	} else {
		asm.Comment(fmt.Sprintf("u16[$%06x] |= %#016b", longAddr, newBits))
	}

	asm.LDA_imm16_w(newBits)
	asm.ORA_long(longAddr)
	asm.STA_long(longAddr)

	if s.onUpdated != nil {
		s.onUpdated(s, asm, initial, updated)
	}

	return true
}

type syncableMaxU8 struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	absMax uint8

	onUpdated syncableMaxU8OnUpdated

	pendingUpdate bool
	updatingTo    uint8
	notification  string
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

func (s *syncableMaxU8) CanUpdate() bool {
	if !s.pendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.g
	if g.local.SRAM[s.offset] != s.updatingTo {
		return false
	}

	// send the notification:
	if s.notification != "" {
		g.pushNotification(s.notification)
		s.notification = ""
	}

	s.pendingUpdate = false

	return true
}

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
	s.pendingUpdate = true
	s.updatingTo = maxV
	if s.names != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.names) {
			received := s.names[i]
			s.notification = fmt.Sprintf("got %s from %s", received, maxP.Name)
			asm.Comment(s.notification + ":")
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

type syncableBottle struct {
	g *Game

	offset    uint16
	isEnabled *bool
	names     []string

	pendingUpdate bool
	updatingTo    byte
	notification  string
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

func (s *syncableBottle) CanUpdate() bool {
	if !s.pendingUpdate {
		return true
	}

	// wait until we see the desired update:
	g := s.g
	if g.local.SRAM[s.offset] != s.updatingTo {
		return false
	}

	// send the notification:
	if s.notification != "" {
		g.pushNotification(s.notification)
		s.notification = ""
	}

	s.pendingUpdate = false

	return true
}

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
	s.pendingUpdate = true
	s.updatingTo = maxV
	if s.names != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.names) {
			received := s.names[i]
			s.notification = fmt.Sprintf("got %s from %s", received, maxP.Name)
			asm.Comment(s.notification + ":")
		}
	}

	asm.LDA_imm8_b(maxV)
	asm.STA_long(0x7EF000 + uint32(offset))

	return true
}
