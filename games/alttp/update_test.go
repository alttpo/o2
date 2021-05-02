package alttp

import (
	"bytes"
	"o2/interfaces"
	"o2/snes/asm"
	"o2/snes/emulator"
	"o2/util"
	"strings"
	"testing"
)

func TestGame_generateUpdateAsm(t *testing.T) {
	type fields struct {
		// title that goes into the $FFC0 header of the ROM; used to vary the game type detected e.g. "VT " for randomizers
		ROMTitle string
	}

	// sramTest represents a single byte of SRAM used for verifying sync logic
	type sramTest struct {
		// offset from $7EF000 in WRAM, e.g. $340 for bow, $341 for boomerang, etc.
		offset        uint16
		// value to set for the local player
		localValue    uint8
		// value to set for the remote player syncing in
		remoteValue   uint8
		// expected value to see for the local player after ASM code runs
		expectedValue uint8
	}

	type test struct {
		name             string
		fields           fields
		// individual bytes of SRAM to be set and tested
		sram             []sramTest
		wantUpdated      bool
		// expected front-end notification to be sent or "" if none expected
		wantNotification string
		// any custom logic to run before generating update ASM:
		setup            func(t *testing.T, g *Game, tt *test)
		// any verification logic to run after verifying update:
		verify           func(t *testing.T, g *Game, system *emulator.System, tt *test)
	}

	tests := []test{
		{
			name: "No update",
			fields: fields{
				ROMTitle: "ZELDANODENSETSU",
			},
			wantUpdated: false,
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
			wantUpdated:      true,
			wantNotification: "got Mushroom from remote",
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
			wantUpdated: true,
			wantNotification: "got Magic Powder from remote",
		},
		{
			name: "VT flute activated",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x1,
					expectedValue: 0x1,
				},
				{
					offset:        0x34C,
					expectedValue: 3,
				},
			},
			wantUpdated: true,
			wantNotification: "got Flute (activated) from remote",
		},
		{
			name: "VT flute",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x2,
					expectedValue: 0x2,
				},
				{
					offset:        0x34C,
					expectedValue: 2,
				},
			},
			wantUpdated: true,
			wantNotification: "got Flute from remote",
		},
		{
			name: "VT shovel",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x4,
					expectedValue: 0x4,
				},
				{
					offset:        0x34C,
					expectedValue: 1,
				},
			},
			wantUpdated: true,
			wantNotification: "got Shovel from remote",
		},
		{
			name: "VT red boomerang",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x40,
					expectedValue: 0x40,
				},
				{
					offset:        0x341,
					expectedValue: 2,
				},
			},
			wantUpdated: true,
			wantNotification: "got Red Boomerang from remote",
		},
		{
			name: "VT blue boomerang",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0,
					remoteValue:   0x80,
					expectedValue: 0x80,
				},
				{
					offset:        0x341,
					expectedValue: 1,
				},
			},
			wantUpdated: true,
			wantNotification: "got Blue Boomerang from remote",
		},
		{
			name: "VT bow no arrows",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38E,
					localValue:    0,
					remoteValue:   0x40,
					expectedValue: 0x40,
				},
				{
					// have arrows:
					offset:        0x377,
					localValue:    0,
					expectedValue: 0,
				},
				{
					// expect bow w/o arrows:
					offset:        0x340,
					expectedValue: 1,
				},
			},
			wantUpdated: true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "VT bow with arrows",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38E,
					localValue:    0,
					remoteValue:   0x40,
					expectedValue: 0x40,
				},
				{
					// have arrows:
					offset:        0x377,
					localValue:    1,
					expectedValue: 1,
				},
				{
					// expect bow w/ arrows:
					offset:        0x340,
					expectedValue: 2,
				},
			},
			wantUpdated: true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "VT bow no change",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38E,
					localValue:    0,
					remoteValue:   0x40,
					expectedValue: 0x40,
				},
				{
					// already have silvers selected, don't alter selection:
					offset:        0x340,
					localValue:    3,
					expectedValue: 3,
				},
			},
			wantUpdated: true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "VT silver bow no arrows",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38E,
					localValue:    0,
					remoteValue:   0x80,
					expectedValue: 0x80,
				},
				{
					// have no arrows:
					offset:        0x377,
					localValue:    0,
					expectedValue: 0,
				},
				{
					// expect silver bow w/o arrows:
					offset:        0x340,
					expectedValue: 3,
				},
			},
			wantUpdated: true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "VT silver bow with arrows",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38E,
					localValue:    0,
					remoteValue:   0x80,
					expectedValue: 0x80,
				},
				{
					// have arrows:
					offset:        0x377,
					localValue:    1,
					expectedValue: 1,
				},
				{
					// expect silver bow w/ arrows:
					offset:        0x340,
					expectedValue: 4,
				},
			},
			wantUpdated: true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "VT silver bow no change",
			fields: fields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38E,
					localValue:    0,
					remoteValue:   0x80,
					expectedValue: 0x80,
				},
				{
					// already have bow selected, don't alter selection:
					offset:        0x340,
					localValue:    2,
					expectedValue: 2,
				},
			},
			wantUpdated: true,
			wantNotification: "got Silver Bow from remote",
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.name, func(t *testing.T) {
			// create a ROM for testing our patch process and the generated ASM code:
			rom, err := emulator.MakeTestROM(tt.fields.ROMTitle)
			if err != nil {
				t.Fatal(err)
			}

			// instantiate the Game instance for testing:
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

			// subscribe to front-end Notifications from the game:
			lastNotification := ""
			g.Notifications.Subscribe(interfaces.ObserverImpl(func(object interface{}) {
				lastNotification = object.(string)
				t.Logf("notify: '%s'", lastNotification)
			}))

			// create the CPU-only SNES emulator:
			system := emulator.System{}
			if err := system.CreateEmulator(); err != nil {
				t.Fatal(err)
			}
			// copy ROM contents into system emulator:
			copy(system.ROM[:], g.rom.Contents)

			// setup patch code in emulator SRAM:
			if err := system.SetupPatch(); err != nil {
				t.Fatal(err)
			}

			// set up SRAM per each player:
			g.players[1].Index = 1
			g.players[1].TTL = 255
			g.players[1].Name = "remote"
			for _, sram := range tt.sram {
				system.WRAM[0xF000+sram.offset] = sram.localValue
				g.local.SRAM[sram.offset] = sram.localValue
				g.players[1].SRAM[sram.offset] = sram.remoteValue
			}

			if tt.setup != nil {
				tt.setup(t, g, tt)
			}

			a := &asm.Emitter{
				Code: &bytes.Buffer{},
				Text: &strings.Builder{},
			}
			// default to 8-bit:
			a.AssumeSEP(0x30)
			updated := g.generateUpdateAsm(a)
			if updated != tt.wantUpdated {
				t.Logf("%s", a.Text.String())
				t.Errorf("generateUpdateAsm() = %v, want %v", updated, tt.wantUpdated)
				return
			}
			if updated {
				a.Comment("restore 8-bit mode and return to RESET code:")
				a.SEP(0x30)
				a.RTS()
			}
			t.Logf("%s", a.Text.String())

			aw := util.ArrayWriter{Buffer: system.SRAM[0x7C00:]}
			if _, err := a.Code.WriteTo(&aw); err != nil {
				t.Fatal(err)
			}

			// run the CPU until it either runs away or hits the expected stopping point in the ROM code:
			// see the emulator.MakeTestROM() function for details
			system.CPU.Reset()
			for cycles := 0; cycles < 0x1000; {
				t.Logf("%s", system.CPU.DisassembleCurrentPC())
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

			// copy SRAM shadow in WRAM into local player copy:
			copy(g.local.SRAM[:], system.WRAM[0xF000:])

			// verify SRAM values:
			for _, sram := range tt.sram {
				if actual, expected := system.WRAM[0xF000+sram.offset], sram.expectedValue; actual != expected {
					t.Errorf("local.SRAM[%#04X] = %02X, expected %02X", sram.offset, actual, expected)
				}
			}

			// call generateUpdateAsm() again for next frame to receive notifications:
			a.Code = &bytes.Buffer{}
			_ = g.generateUpdateAsm(a)

			if tt.wantNotification != "" && lastNotification != tt.wantNotification {
				t.Errorf("notification = '%s', expected '%s'", lastNotification, tt.wantNotification)
			}

			// call custom verify function for test:
			if tt.verify != nil {
				tt.verify(t, g, &system, tt)
			}
		})
	}
}
