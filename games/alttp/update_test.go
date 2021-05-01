package alttp

import (
	"bytes"
	"o2/snes"
	"o2/snes/asm"
	"o2/snes/emulator"
	"o2/util"
	"strings"
	"testing"
)

func TestGame_generateUpdateAsm(t *testing.T) {
	type fields struct {
		rom              *snes.ROM
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
	type args struct {
		a *asm.Emitter
	}

	rom, err := emulator.MakeTestROM()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
		verify func(t *testing.T, g *Game, system *emulator.System)
	}{
		{
			name: "No update",
			fields: fields{
				rom:              rom,
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
			args: args{
				a: &asm.Emitter{
					Code: &bytes.Buffer{},
					Text: &strings.Builder{},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Game{
				rom:              tt.fields.rom,
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

			if got := g.generateUpdateAsm(tt.args.a); got != tt.want {
				t.Logf("%s", tt.args.a.Text.String())
				t.Errorf("generateUpdateAsm() = %v, want %v", got, tt.want)
				return
			}
			t.Logf("%s", tt.args.a.Text.String())

			aw := util.ArrayWriter{Buffer: system.SRAM[0x7C00:]}
			if _, err := tt.args.a.Code.WriteTo(&aw); err != nil {
				t.Fatal(err)
			}

			system.CPU.Reset()
			for cycles := 0; cycles < 0x1000; {
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
