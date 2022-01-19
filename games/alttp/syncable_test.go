package alttp

import (
	"bytes"
	"log"
	"o2/games"
	"o2/snes/asm"
	"testing"
)

type testSyncableGame struct {
	players [2]Player
}

func (t *testSyncableGame) LocalSyncablePlayer() games.SyncablePlayer {
	return &t.players[0]
}

func (t *testSyncableGame) RemoteSyncablePlayers() []games.SyncablePlayer {
	activePlayers := make([]games.SyncablePlayer, 0, len(t.players))
	for i := range t.players {
		activePlayers = append(activePlayers, &t.players[i])
	}
	return activePlayers
}

func (t *testSyncableGame) PushNotification(notification string) {
	log.Printf("notification: '%s'\n", notification)
}

func Test_syncableBitU8_GenerateUpdate(t *testing.T) {
	type fields struct {
		offset    uint16
		isEnabled *bool
		names     []string
		mask      uint8
		p0sram    uint8
		p1sram    uint8
	}
	tests := []struct {
		name             string
		fields           fields
		want             bool
		wantAsm          []byte
		wantNotification string
	}{
		{
			name: "SyncableBitU8 syncs from zero to most bits",
			fields: fields{
				offset:    0x379,
				isEnabled: new(bool),
				names:     []string{"0", "1", "2", "3", "4", "5", "6", "7"},
				mask:      0xFF,
				p0sram:    0x00,
				p1sram:    0x66,
			},
			want:             true,
			wantAsm:          []byte{0xa9, 0x66, 0xf, 0x79, 0xf3, 0x7e, 0x8f, 0x79, 0xf3, 0x7e},
			wantNotification: "got 1 from p1, 2 from p1, 5 from p1, 6 from p1",
		},
		{
			name: "SyncableBitU8 syncs from non-zero to most bits",
			fields: fields{
				offset:    0x379,
				isEnabled: new(bool),
				names:     []string{"0", "1", "2", "3", "4", "5", "6", "7"},
				mask:      0xFF,
				p0sram:    0x24,
				p1sram:    0x66,
			},
			want:             true,
			wantAsm:          []byte{0xa9, 0x42, 0xf, 0x79, 0xf3, 0x7e, 0x8f, 0x79, 0xf3, 0x7e},
			wantNotification: "got 1 from p1, 6 from p1",
		},
		{
			name: "SyncableBitU8 ignores empty names",
			fields: fields{
				offset:    0x379,
				isEnabled: new(bool),
				names:     []string{"", "1", "2", "", "", "5", "6", ""},
				mask:      0xFF,
				p0sram:    0x42,
				p1sram:    0x77,
			},
			want:             true,
			wantAsm:          []byte{0xa9, 0x35, 0xf, 0x79, 0xf3, 0x7e, 0x8f, 0x79, 0xf3, 0x7e},
			wantNotification: "got 2 from p1, 5 from p1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// arrange:
			g := &testSyncableGame{players: [2]Player{}}
			g.players[0] = Player{
				IndexF:       0,
				Ttl:          255,
				Team:         0,
				NameF:        "p0",
				Module:       7,
				PriorModule:  9,
				SubModule:    0,
				SubSubModule: 0,
				SRAM:         [1280]byte{},
				WRAM:         make(map[uint16]*SyncableWRAM),
			}
			g.players[1] = Player{
				IndexF:       1,
				Ttl:          255,
				Team:         0,
				NameF:        "p1",
				Module:       7,
				PriorModule:  9,
				SubModule:    0,
				SubSubModule: 0,
				SRAM:         [1280]byte{},
				WRAM:         make(map[uint16]*SyncableWRAM),
			}
			g.players[0].SRAM[tt.fields.offset] = tt.fields.p0sram
			g.players[1].SRAM[tt.fields.offset] = tt.fields.p1sram
			s := &games.SyncableBitU8{
				SyncableGame: g,
				IsEnabledPtr: new(bool),
				Offset:       uint32(tt.fields.offset),
				BitNames:     tt.fields.names,
				SyncMask:     tt.fields.mask,
			}
			*s.IsEnabledPtr = true

			a := asm.NewEmitter(true)
			a.AssumeSEP(0x30)

			// act:
			got := s.GenerateUpdate(a)

			// assert:
			if got != tt.want {
				t.Errorf("GenerateUpdate() = %v, want %v", got, tt.want)
			}
			if actual, expected := a.Bytes(), tt.wantAsm; !bytes.Equal(expected, actual) {
				t.Errorf("asm.Bytes() = %#v, want %#v\n%s\n", actual, expected, a.Text.String())
			}
			if actual, expected := s.Notification, tt.wantNotification; actual != expected {
				t.Errorf("notification = '%s', want '%s'", actual, expected)
			}
		})
	}
}
