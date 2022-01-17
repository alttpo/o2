package alttp

import (
	"fmt"
	"testing"
)

func TestAsm_Vanilla_Items(t *testing.T) {
	tests := []sramTestCase{
		{
			name: "No update",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			wantUpdated: false,
		},
		{
			name: "Mushroom",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x344,
					localValue:    0,
					remoteValue:   1,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Mushroom from remote",
		},
		{
			name: "Powder",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x344,
					localValue:    0,
					remoteValue:   2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Magic Powder from remote",
		},
		{
			name: "Flute activated",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x34C,
					localValue:    0,
					remoteValue:   3,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute (activated) from remote",
		},
		{
			name: "Flute activated from flute",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x34C,
					localValue:    2,
					remoteValue:   3,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute (activated) from remote",
		},
		{
			name: "Flute activated from shovel",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x34C,
					localValue:    1,
					remoteValue:   3,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute (activated) from remote",
		},
		{
			name: "Flute",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x34C,
					localValue:    0,
					remoteValue:   2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute from remote",
		},
		{
			name: "Flute from shovel",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x34C,
					localValue:    1,
					remoteValue:   2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute from remote",
		},
		{
			name: "Shovel",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x34C,
					localValue:    0,
					remoteValue:   1,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Shovel from remote",
		},
		{
			name: "Red boomerang",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x341,
					localValue:    0,
					remoteValue:   2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Red Boomerang from remote",
		},
		{
			name: "Red boomerang from blue boomerang",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x341,
					localValue:    1,
					remoteValue:   2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Red Boomerang from remote",
		},
		{
			name: "Blue boomerang",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x341,
					localValue:    0,
					remoteValue:   1,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Blue Boomerang from remote",
		},
		{
			name: "Bow no arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have no arrows:
					offset:        0x377,
					localValue:    0,
					expectedValue: 0,
				},
				{
					// expect bow w/o arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   1,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "Bow no arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have no arrows:
					offset:        0x377,
					localValue:    0,
					expectedValue: 0,
				},
				{
					// expect bow w/o arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   2,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "Bow with arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have arrows:
					offset:        0x377,
					localValue:    1,
					expectedValue: 1,
				},
				{
					// expect bow w/ arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   1,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "Bow with arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have arrows:
					offset:        0x377,
					localValue:    1,
					expectedValue: 1,
				},
				{
					// expect bow w/ arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "Bow no change",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// already have silvers selected, don't alter selection:
					offset:        0x340,
					localValue:    3,
					remoteValue:   1,
					expectedValue: 3,
				},
			},
			wantUpdated:      false,
			wantNotification: "",
		},
		{
			name: "Bow no change",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// already have silvers selected, don't alter selection:
					offset:        0x340,
					localValue:    3,
					remoteValue:   2,
					expectedValue: 3,
				},
			},
			wantUpdated:      false,
			wantNotification: "",
		},
		{
			name: "Silver bow no arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have no arrows:
					offset:        0x377,
					localValue:    0,
					expectedValue: 0,
				},
				{
					// expect silver bow w/o arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   3,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "Silver bow no arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have no arrows:
					offset:        0x377,
					localValue:    0,
					expectedValue: 0,
				},
				{
					// expect silver bow w/o arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   4,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "Silver bow with arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have arrows:
					offset:        0x377,
					localValue:    1,
					expectedValue: 1,
				},
				{
					// expect silver bow w/ arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   3,
					expectedValue: 4,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "Silver bow with arrows",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// have arrows:
					offset:        0x377,
					localValue:    1,
					expectedValue: 1,
				},
				{
					// expect silver bow w/ arrows:
					offset:        0x340,
					localValue:    0,
					remoteValue:   4,
					expectedValue: 4,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "Silver bow no change",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// already have bow selected, don't alter selection:
					offset:        0x340,
					localValue:    3,
					remoteValue:   3,
					expectedValue: 3,
				},
			},
			wantUpdated:      false,
			wantNotification: "",
		},
		{
			name: "Silver bow no change",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					// already have bow selected, don't alter selection:
					offset:        0x340,
					localValue:    3,
					remoteValue:   4,
					expectedValue: 3,
				},
			},
			wantUpdated:      false,
			wantNotification: "",
		},
		{
			name: "Hearts",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:        0x36C,
					localValue:    3 << 3,
					remoteValue:   4 << 3,
					expectedValue: 4 << 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got 1 new heart from remote",
		},
	}

	runAsmEmulationTests(t, tests)
}

func TestAsm_Vanilla_UnderworldRooms(t *testing.T) {
	tests := make([]sramTestCase, 0, len(underworldNames))

	for room := uint16(0); room < 0x130; room++ {
		name, ok := underworldNames[room]
		if !ok {
			continue
		}

		tests = append(tests, sramTestCase{
			name: fmt.Sprintf("Underworld $%03x: %s", room, name),
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			sram: []sramTest{
				{
					offset:     room << 1,
					localValue: 0,
					// quadrants visited:
					remoteValue:   0b_0000_1111,
					expectedValue: 0b_0000_1111,
				},
			},
			wantUpdated:      true,
			wantNotification: "",
			verify:           nil,
		})

		// add a test specific for boss defeated notification:
		if bossName, ok := underworldBossNames[room]; ok {
			tests = append(tests, sramTestCase{
				name: fmt.Sprintf("Underworld BOSS $%03x: %s", room, name),
				fields: sramTestCaseFields{
					ROMTitle: "ZELDANODENSETSU",
				},
				sram: []sramTest{{
					offset:     room<<1 + 1, // high byte of u16
					localValue: 0,
					// boss defeated:
					remoteValue:   0b0000_1000,
					expectedValue: 0b0000_1000,
				}},
				wantUpdated:      true,
				wantNotification: fmt.Sprintf("got %s defeated from remote", bossName),
			})
		}

	}

	runAsmEmulationTests(t, tests)
}
