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
		ROMTitle string
	}

	type sramTest struct {
		offset        uint16
		localValue    uint8
		remoteValue   uint8
		expectedValue uint8
	}

	type test struct {
		name   string
		fields fields
		sram   []sramTest
		want   bool
		setup  func(t *testing.T, g *Game, tt *test)
		verify func(t *testing.T, g *Game, system *emulator.System, tt *test)
	}

	tests := []test{
		{
			name: "No update",
			fields: fields{
				ROMTitle: "ZELDANODENSETSU",
			},
			want: false,
		},
		{
			name: "VT mushroom",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x20,
					expectedValue: 0x20,
				},
				{
					offset:        0x344,
					expectedValue: 1,
				},
			},
			want: true,
		},
		{
			name: "VT powder",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x10,
					expectedValue: 0x10,
				},
				{
					offset:        0x344,
					expectedValue: 2,
				},
			},
			want: true,
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			rom, err := emulator.MakeTestROM(tt.fields.ROMTitle)
			if err != nil {
				t.Fatal(err)
			}

			g := &Game{
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
			}
			g.Reset()

			// set up SRAM per each player:
			g.players[1].Index = 1
			g.players[1].TTL = 255
			for _, sram := range tt.sram {
				g.local.SRAM[sram.offset] = sram.localValue
				g.players[1].SRAM[sram.offset] = sram.remoteValue
			}

			if tt.setup != nil {
				tt.setup(t, g, tt)
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

			// verify SRAM values:
			for _, sram := range tt.sram {
				if actual, expected := system.WRAM[0xF000+sram.offset], sram.expectedValue; actual != expected {
					t.Errorf("local.SRAM[%#04X] = %02X, expected %02X", sram.offset, actual, expected)
				}
			}

			// call verify function for test:
			if tt.verify != nil {
				tt.verify(t, g, &system, tt)
			}
		})
	}
}
