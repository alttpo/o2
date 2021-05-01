package alttp

import (
	"bytes"
	"o2/snes/asm"
	"o2/snes/emulator"
	"o2/util"
	"strings"
	"testing"
)

func TestGame_generateUpdateAsm(t *testing.T) {
	type fields struct {
		ROMTitle         string
		IsCreated        bool
		GameName         string
		PlayerColor      uint16
		SyncItems        bool
		SyncDungeonItems bool
		SyncProgress     bool
		SyncHearts       bool
		SyncSmallKeys    bool
		SyncUnderworld   bool
		SyncOverworld    bool
		SyncChests       bool
		SyncTunicColor   bool
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
		setup  func(t *testing.T, g *Game)
		verify func(t *testing.T, g *Game, system *emulator.System)
	}{
		{
			name: "No update",
			fields: fields{
				ROMTitle:         "ZELDANODENSETSU",
				IsCreated:        true,
				GameName:         "ALTTP",
				PlayerColor:      0x12ef,
				SyncItems:        true,
				SyncDungeonItems: true,
				SyncProgress:     true,
				SyncHearts:       true,
				SyncSmallKeys:    true,
				SyncUnderworld:   true,
				SyncOverworld:    true,
				SyncChests:       true,
				SyncTunicColor:   false,
			},
			want: false,
		},
		{
			name: "VT: mushroom",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle:         "VT test",
				IsCreated:        true,
				GameName:         "ALTTP",
				PlayerColor:      0x12ef,
				SyncItems:        true,
				SyncDungeonItems: true,
				SyncProgress:     true,
				SyncHearts:       true,
				SyncSmallKeys:    true,
				SyncUnderworld:   true,
				SyncOverworld:    true,
				SyncChests:       true,
				SyncTunicColor:   false,
			},
			setup: func(t *testing.T, g *Game) {
				g.local.SRAM[0x38C] = 0
				g.players[1].Index = 1
				g.players[1].TTL = 255
				g.players[1].SRAM[0x38C] = 0x20
			},
			want: true,
			verify: func(t *testing.T, g *Game, system *emulator.System) {
				offset := uint16(0x38C)
				if actual, expected := system.WRAM[0xF000 + offset], uint8(0x20); actual != expected {
					t.Errorf("local.SRAM[%#04X] = %02X, expected %02X", offset, actual, expected)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rom, err := emulator.MakeTestROM(tt.fields.ROMTitle)
			if err != nil {
				t.Fatal(err)
			}

			g := &Game{
				rom:              rom,
				IsCreated:        tt.fields.IsCreated,
				GameName:         tt.fields.GameName,
				PlayerColor:      tt.fields.PlayerColor,
				SyncItems:        tt.fields.SyncItems,
				SyncDungeonItems: tt.fields.SyncDungeonItems,
				SyncProgress:     tt.fields.SyncProgress,
				SyncHearts:       tt.fields.SyncHearts,
				SyncSmallKeys:    tt.fields.SyncSmallKeys,
				SyncUnderworld:   tt.fields.SyncUnderworld,
				SyncOverworld:    tt.fields.SyncOverworld,
				SyncChests:       tt.fields.SyncChests,
				SyncTunicColor:   tt.fields.SyncTunicColor,
			}
			g.Reset()

			if tt.setup != nil {
				tt.setup(t, g)
			}

			system := emulator.System{}
			if err := system.CreateEmulator(); err != nil {
				t.Fatal(err)
			}
			// copy ROM contents into system:
			copy(system.ROM[:], g.rom.Contents)

			// setup patch code in SRAM:
			if err := system.SetupPatch(); err != nil {
				t.Fatal(err)
			}

			a := &asm.Emitter{
				Code: &bytes.Buffer{},
				Text: &strings.Builder{},
			}
			// default to 8-bit:
			a.AssumeSEP(0x30)
			updated := g.generateUpdateAsm(a)
			if updated != tt.want {
				t.Logf("%s", a.Text.String())
				t.Errorf("generateUpdateAsm() = %v, want %v", updated, tt.want)
				return
			}
			if updated {
				a.SEP(0x30)
				a.RTS()
			}
			t.Logf("%s", a.Text.String())

			aw := util.ArrayWriter{Buffer: system.SRAM[0x7C00:]}
			if _, err := a.Code.WriteTo(&aw); err != nil {
				t.Fatal(err)
			}

			system.CPU.Reset()
			for cycles := 0; cycles < 0x1000; {
				//t.Logf("%s", system.CPU.DisassembleCurrentPC())
				nCycles, _ := system.CPU.Step()
				cycles += nCycles
				if system.CPU.PC == 0x8006 {
					break
				}
			}

			if system.CPU.PC != 0x8006 {
				t.Errorf("CPU ran too long and did not reach PC=0x008006; actual=%#06x", system.CPU.PC)
				return
			}

			// call verify function for test:
			if tt.verify != nil {
				tt.verify(t, g, &system)
			}
		})
	}
}
