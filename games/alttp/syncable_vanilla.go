package alttp

import (
	"fmt"
	"github.com/alttpo/snes/asm"
	"log"
	"o2/games"
)

type SyncableVanillaBow struct {
	SyncableGame games.SyncableGame

	Offset       uint32
	IsEnabledPtr *bool

	Notification string
}

func (g *Game) NewSyncableVanillaBow(offset uint16, enabled *bool) *SyncableVanillaBow {
	s := &SyncableVanillaBow{
		SyncableGame: g,
		Offset:       uint32(offset),
		IsEnabledPtr: enabled,
	}
	g.NewSyncable(offset, s)
	return s
}

func (s *SyncableVanillaBow) Size() uint      { return 1 }
func (s *SyncableVanillaBow) IsEnabled() bool { return *s.IsEnabledPtr }

func (s *SyncableVanillaBow) GenerateUpdate(a *asm.Emitter, index uint32) bool {
	g := s.SyncableGame

	local := g.LocalSyncablePlayer()
	offset := s.Offset

	initial := local.ReadableMemory(games.SRAM).ReadU8(offset)
	// treat w/ and w/o arrows as the same:
	if initial == 2 {
		initial = 1
	} else if initial >= 4 {
		initial = 3
	}

	maxP := local
	maxV := initial
	for _, p := range g.RemoteSyncablePlayers() {
		v := p.ReadableMemory(games.SRAM).ReadU8(offset)
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
	received := vanillaItemNames[0x340][maxV]
	s.Notification = fmt.Sprintf("got %s from %s", received, maxP.Name())
	a.Comment(s.Notification + ":")

	a.LDA_long(0x7EF377) // arrows
	a.CMP_imm8_b(0x01)   // are arrows present?
	a.LDA_imm8_b(maxV)   // bow level; 1 = wood, 3 = silver
	a.ADC_imm8_b(0x00)   // add +1 to bow if arrows are present
	a.STA_long(local.ReadableMemory(games.SRAM).BusAddress(offset))

	// write confirmation:
	a.LDA_imm8_b(0x01)
	a.STA_long(a.GetBase() + 0x02 + index)

	return true
}

func (s *SyncableVanillaBow) ConfirmAsmExecuted(index uint32, value uint8) {
	if value == 0x00 {
		return
	}

	// send the notification:
	if s.Notification != "" {
		s.SyncableGame.PushNotification(s.Notification)
		s.Notification = ""
	}
}

func (s *SyncableVanillaBow) LocalCheck(wramCurrent, wramPrevious []byte) (notifications []games.NotificationStatement) {
	base := uint32(0xF000)

	curr := wramCurrent[base+s.Offset]
	prev := wramPrevious[base+s.Offset]
	if curr == prev {
		return
	}

	longAddr := s.SyncableGame.LocalSyncablePlayer().ReadableMemory(games.SRAM).BusAddress(s.Offset)
	log.Printf("alttp: local: u8 [$%06x]: $%02x -> $%02x\n", longAddr, prev, curr)

	valueNames := vanillaItemNames[uint16(s.Offset)]
	if valueNames == nil {
		return
	}

	i := int(curr) - 1
	if i >= 0 && i < len(valueNames) {
		if valueNames[i] != "" {
			notifications = []games.NotificationStatement{
				{Items: valueNames[i : i+1], Verb: "picked up"},
			}
		}
	}

	return
}
