package games

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"log"
	"strings"
)

func bin16_4(v uint16) string {
	return fmt.Sprintf("0b%04b_%04b_%04b_%04b", v>>12&0xF, v>>8&0xF, v>>4&0xF, v&0xF)
}

func bin8_4(v uint8) string {
	return fmt.Sprintf("0b%04b_%04b", v>>4&0xF, v&0xF)
}

type SyncableGame interface {
	LocalSyncablePlayer() SyncablePlayer
	RemoteSyncablePlayers() []SyncablePlayer

	PushNotification(notification string)
}

type SyncStrategy interface {
	Size() uint
	IsEnabled() bool

	// GenerateUpdate returns true if asm instructions were emitted to the given asm.Emitter and returns false otherwise
	GenerateUpdate(newEmitter func() *asm.Emitter, index uint32) (isUpdated bool, a *asm.Emitter)

	// ConfirmAsmExecuted is called when generated ASM code is confirmed to have executed:
	ConfirmAsmExecuted(index uint32, value uint8)

	// LocalCheck compares previous frame and current frame WRAM to identify local picked up items:
	LocalCheck(wramCurrent, wramPrevious []byte) (notifications []NotificationStatement)
}

type NotificationStatement struct {
	Items    []string
	Verb     string
	Location string
	Source   string
}

func (n NotificationStatement) String() string {
	if n.Verb == "" || len(n.Items) == 0 {
		return ""
	}

	joined := strings.Join(n.Items, ", ")

	var sb strings.Builder
	sb.Grow(len(n.Verb) + len(joined) + len(n.Source) + len(n.Location) + 1 + 4 + 6)
	sb.WriteString(n.Verb)
	sb.WriteByte(' ')
	sb.WriteString(joined)
	if n.Location != "" {
		sb.WriteString(" at ")
		sb.WriteString(n.Location)
	}
	if n.Source != "" {
		sb.WriteString(" from ")
		sb.WriteString(n.Source)
	}
	return sb.String()
}

type (
	PlayerPredicate func(p SyncablePlayer) bool

	SyncableBitU8GenerateAsm func(s *SyncableBitU8, asm *asm.Emitter, initial, updated, newBits uint8)
	SyncableBitU8OnUpdated   func(s *SyncableBitU8, asm *asm.Emitter, initial, updated uint8)

	SyncableBitU16GenerateAsm func(s *SyncableBitU16, asm *asm.Emitter, initial, updated, newBits uint16)
	SyncableBitU16OnUpdated   func(s *SyncableBitU16, asm *asm.Emitter, initial, updated uint16)

	SyncableMaxU8OnUpdated   func(s *SyncableMaxU8, asm *asm.Emitter, initial, updated uint8)
	SyncableMaxU8GenerateAsm func(s *SyncableMaxU8, asm *asm.Emitter, initial, updated uint8)

	SyncableCustomU8Update func(s *SyncableCustomU8, asm func() *asm.Emitter, index uint32) (bool, *asm.Emitter)
	IsUpdateStillPending   func(s *SyncableCustomU8) bool
)

func PlayerPredicateIdentity(_ SyncablePlayer) bool { return true }

type SyncableBitU8 struct {
	SyncableGame

	Offset uint32
	MemoryKind
	SyncMask uint8

	IsEnabledPtr *bool
	BitNames     [8]string
	Verbs        [8]string

	PlayerPredicate

	GenerateAsm SyncableBitU8GenerateAsm
	OnUpdated   SyncableBitU8OnUpdated

	Notification string
}

func NewSyncableBitU8(g SyncableGame, offset uint32, enabled *bool, names [8]string, onUpdated SyncableBitU8OnUpdated) *SyncableBitU8 {
	s := &SyncableBitU8{
		SyncableGame: g,
		Offset:       offset,
		MemoryKind:   SRAM,
		IsEnabledPtr: enabled,
		BitNames:     names,
		OnUpdated:    onUpdated,
		SyncMask:     0xFF,
	}
	return s
}

func (s *SyncableBitU8) Size() uint      { return 1 }
func (s *SyncableBitU8) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *SyncableBitU8) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *SyncableBitU8) GenerateUpdate(newEmitter func() *asm.Emitter, index uint32) (isUpdated bool, a *asm.Emitter) {
	g := s.SyncableGame
	local := g.LocalSyncablePlayer()

	if s.PlayerPredicate != nil && !s.PlayerPredicate(local) {
		return false, nil
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
		return false, nil
	}

	isUpdated, a = true, newEmitter()

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

	a.Comment(fmt.Sprintf("u8 [$%06x] = %#08b | %#08b", longAddr, initial, newBits))

	failLabel := fmt.Sprintf("fail%06x", longAddr)
	nextLabel := fmt.Sprintf("next%06x", longAddr)

	if s.GenerateAsm != nil {
		s.GenerateAsm(s, a, initial, updated, newBits)
	} else {
		a.LDA_long(longAddr)
		a.CMP_imm8_b(initial)
		a.BNE(failLabel)
		a.ORA_imm8_b(newBits)
		a.STA_long(longAddr)
	}

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

	return
}

func (s *SyncableBitU8) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []NotificationStatement) {
	base := uint32(0xF000)
	if s.MemoryKind == WRAM {
		base = 0
	}

	curr := wramCurrent[base+s.Offset]
	prev := wramPrevious[base+s.Offset]
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(s.MemoryKind).BusAddress(s.Offset)
	log.Printf("alttp: local: u8 [$%06x]: %s -> %s\n", longAddr, bin8_4(prev), bin8_4(curr))

	k := uint8(1)
	for i := 0; i < len(s.BitNames); i++ {
		if prev&k == 0 && curr&k == k {
			if s.BitNames[i] != "" {
				verb := s.Verbs[i]
				if verb == "" {
					verb = "picked up"
				}
				notifications = append(notifications, NotificationStatement{
					Items: []string{s.BitNames[i]},
					Verb:  verb,
				})
			}
		}
		k <<= 1
	}

	return
}

type SyncableBitU16 struct {
	SyncableGame

	Offset uint32
	MemoryKind
	SyncMask uint16

	IsEnabledPtr *bool
	BitNames     [16]string
	Verbs        [16]string

	PlayerPredicate

	GenerateAsm SyncableBitU16GenerateAsm
	OnUpdated   SyncableBitU16OnUpdated

	Notification string
}

func NewSyncableBitU16(g SyncableGame, offset uint32, enabled *bool, names [16]string, onUpdated SyncableBitU16OnUpdated) *SyncableBitU16 {
	s := &SyncableBitU16{
		SyncableGame:    g,
		Offset:          offset,
		MemoryKind:      SRAM,
		IsEnabledPtr:    enabled,
		BitNames:        names,
		SyncMask:        0xFFFF,
		PlayerPredicate: PlayerPredicateIdentity,
		OnUpdated:       onUpdated,
	}
	return s
}

func (s *SyncableBitU16) Size() uint      { return 2 }
func (s *SyncableBitU16) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *SyncableBitU16) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *SyncableBitU16) GenerateUpdate(newEmitter func() *asm.Emitter, index uint32) (isUpdated bool, a *asm.Emitter) {
	g := s.SyncableGame
	local := g.LocalSyncablePlayer()

	// filter out local player:
	if s.PlayerPredicate != nil && !s.PlayerPredicate(local) {
		return false, nil
	}

	offs := s.Offset
	mask := s.SyncMask

	initial := local.ReadableMemory(s.MemoryKind).ReadU16(offs)
	var receivedFrom [16]string

	updated := initial
	for _, p := range g.RemoteSyncablePlayers() {
		// filter out player:
		if s.PlayerPredicate != nil && !s.PlayerPredicate(p) {
			continue
		}

		// read player data:
		v := p.ReadableMemory(s.MemoryKind).ReadU16(offs)
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
		return false, nil
	}

	a = newEmitter()

	// notify local player of new item received:
	s.Notification = ""

	longAddr := local.ReadableMemory(s.MemoryKind).BusAddress(offs)
	newBits := updated & ^initial

	{
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
			a.Comment(s.Notification + ":")
		}
	}

	a.Comment(fmt.Sprintf("u16[$%06x] = %#016b | %#016b", longAddr, initial, newBits))

	failLabel := fmt.Sprintf("fail%06x", longAddr)
	nextLabel := fmt.Sprintf("next%06x", longAddr)
	a.LDA_long(longAddr)
	a.CMP_imm16_w(initial)
	a.BNE(failLabel)

	if s.GenerateAsm != nil {
		s.GenerateAsm(s, a, initial, updated, newBits)
	} else {
		a.ORA_imm16_w(newBits)
		a.STA_long(longAddr)
	}

	if s.OnUpdated != nil {
		s.OnUpdated(s, a, initial, updated)
	}

	// write confirmation:
	a.Comment(fmt.Sprintf("write confirmation for #%d:", index))
	a.SEP(0x30)
	a.LDA_imm8_b(0x01)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x30)
	a.BRA(nextLabel)

	a.Label(failLabel)
	// write failure:
	a.Comment(fmt.Sprintf("write failure for #%d:", index))
	a.SEP(0x30)
	a.LDA_imm8_b(0x00)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x30)

	a.Label(nextLabel)

	return true, a
}

func (s *SyncableBitU16) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []NotificationStatement) {
	base := uint32(0xF000)
	if s.MemoryKind == WRAM {
		base = 0
	}

	curr := binary.LittleEndian.Uint16(wramCurrent[base+s.Offset : base+s.Offset+2])
	prev := binary.LittleEndian.Uint16(wramPrevious[base+s.Offset : base+s.Offset+2])
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(s.MemoryKind).BusAddress(s.Offset)
	log.Printf("alttp: local: u16[$%06x]: %s -> %s\n", longAddr, bin16_4(prev), bin16_4(curr))

	k := uint16(1)
	for i := 0; i < len(s.BitNames); i++ {
		if prev&k == 0 && curr&k == k {
			if s.BitNames[i] != "" {
				verb := s.Verbs[i]
				if verb == "" {
					verb = "picked up"
				}
				notifications = append(notifications, NotificationStatement{
					Items: []string{s.BitNames[i]},
					Verb:  verb,
				})
			}
		}
		k <<= 1
	}

	return
}

type SyncableMaxU8 struct {
	SyncableGame

	Offset uint32
	MemoryKind
	IsEnabledPtr *bool
	AbsMax       uint8

	ValueNames []string
	Verbs      []string

	PlayerPredicate

	GenerateAsm SyncableMaxU8GenerateAsm
	OnUpdated   SyncableMaxU8OnUpdated

	Notification string
}

func NewSyncableMaxU8(g SyncableGame, offset uint32, enabled *bool, names []string, onUpdated SyncableMaxU8OnUpdated) *SyncableMaxU8 {
	s := &SyncableMaxU8{
		SyncableGame: g,
		Offset:       offset,
		MemoryKind:   SRAM,
		IsEnabledPtr: enabled,
		ValueNames:   names,
		AbsMax:       255,
		OnUpdated:    onUpdated,
	}
	return s
}

func (s *SyncableMaxU8) Size() uint      { return 1 }
func (s *SyncableMaxU8) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *SyncableMaxU8) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *SyncableMaxU8) GenerateUpdate(newEmitter func() *asm.Emitter, index uint32) (isUpdated bool, a *asm.Emitter) {
	g := s.SyncableGame
	local := g.LocalSyncablePlayer()
	if s.PlayerPredicate != nil && !s.PlayerPredicate(local) {
		return false, nil
	}

	offset := s.Offset

	maxP := local
	localMemory := local.ReadableMemory(s.MemoryKind)
	maxV := localMemory.ReadU8(offset)
	initial := maxV
	for _, p := range g.RemoteSyncablePlayers() {
		if s.PlayerPredicate != nil && !s.PlayerPredicate(p) {
			continue
		}

		v := p.ReadableMemory(s.MemoryKind).ReadU8(offset)
		// discard value if too high:
		if v > s.AbsMax {
			continue
		}

		if v > maxV {
			maxV, maxP = v, p
		}
	}

	if maxV == initial {
		// no change:
		return false, nil
	}

	isUpdated, a = true, newEmitter()

	// notify local player of new item received:
	s.Notification = ""
	if s.ValueNames != nil {
		i := int(maxV) - 1
		if i >= 0 && i < len(s.ValueNames) {
			if s.ValueNames[i] != "" {
				received := s.ValueNames[i]
				s.Notification = fmt.Sprintf("got %s from %s", received, maxP.Name())
				a.Comment(s.Notification + ":")
			}
		}
	}

	longAddr := localMemory.BusAddress(offset)

	a.Comment(fmt.Sprintf("u8[$%06x]: $%02x -> $%02x", longAddr, initial, maxV))

	failLabel := fmt.Sprintf("fail%06x", longAddr)
	nextLabel := fmt.Sprintf("next%06x", longAddr)
	a.LDA_long(longAddr)
	a.CMP_imm8_b(initial)
	a.BNE(failLabel)

	if s.GenerateAsm != nil {
		s.GenerateAsm(s, a, initial, maxV)
	} else {
		a.LDA_imm8_b(maxV)
		a.STA_long(longAddr)
	}

	if s.OnUpdated != nil {
		s.OnUpdated(s, a, initial, maxV)
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

	return
}

func (s *SyncableMaxU8) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []NotificationStatement) {
	base := uint32(0xF000)
	if s.MemoryKind == WRAM {
		base = 0
	}

	curr := wramCurrent[base+s.Offset]
	prev := wramPrevious[base+s.Offset]
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(s.MemoryKind).BusAddress(s.Offset)
	log.Printf("alttp: local: u8 [$%06x]: $%02x -> $%02x\n", longAddr, prev, curr)

	if s.ValueNames == nil {
		return
	}

	i := int(curr) - 1
	if i >= 0 && i < len(s.ValueNames) {
		if s.ValueNames[i] != "" {
			verb := "picked up"
			if i < len(s.Verbs) && s.Verbs[i] != "" {
				verb = s.Verbs[i]
			}
			notifications = []NotificationStatement{
				{
					Items: s.ValueNames[i : i+1],
					Verb:  verb,
				},
			}
		}
	}

	return
}

type SyncableCustomU8 struct {
	SyncableGame

	Offset uint32
	MemoryKind
	IsEnabledPtr *bool

	CustomGenerateUpdate SyncableCustomU8Update

	Notification string
}

func NewSyncableCustomU8(g SyncableGame, offset uint32, enabled *bool, generateUpdate SyncableCustomU8Update) *SyncableCustomU8 {
	s := &SyncableCustomU8{
		SyncableGame:         g,
		Offset:               offset,
		MemoryKind:           SRAM,
		IsEnabledPtr:         enabled,
		CustomGenerateUpdate: generateUpdate,
	}
	return s
}

func (s *SyncableCustomU8) Size() uint      { return 1 }
func (s *SyncableCustomU8) IsEnabled() bool { return *s.IsEnabledPtr }
func (s *SyncableCustomU8) GenerateUpdate(newEmitter func() *asm.Emitter, index uint32) (bool, *asm.Emitter) {
	return s.CustomGenerateUpdate(s, newEmitter, index)
}

func (s *SyncableCustomU8) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *SyncableCustomU8) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []NotificationStatement) {
	base := uint32(0xF000)
	if s.MemoryKind == WRAM {
		base = 0
	}

	curr := wramCurrent[base+s.Offset]
	prev := wramPrevious[base+s.Offset]
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(s.MemoryKind).BusAddress(s.Offset)
	log.Printf("alttp: local: u8 [$%06x]: $%02x -> $%02x\n", longAddr, prev, curr)

	return
}
