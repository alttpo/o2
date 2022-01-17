package alttp

import (
	"testing"
)

func TestGame_AsmEmulation_VT(t *testing.T) {
	tests := []sramTestCase{
		{
			name: "No update",
			fields: sramTestCaseFields{
				ROMTitle: "ZELDANODENSETSU",
			},
			wantUpdated: false,
		},
		{
			name: "VT mushroom",
			fields: sramTestCaseFields{
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
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Magic Powder from remote",
		},
		{
			name: "VT flute active",
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Flute (active) from remote",
		},
		{
			name: "VT flute (active) from shovel",
			fields: sramTestCaseFields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0x4,
					remoteValue:   0x1,
					expectedValue: 0x5,
				},
				{
					offset:        0x34C,
					localValue:    1,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute (active) from remote",
		},
		{
			name: "VT flute (activated) from flute",
			fields: sramTestCaseFields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0x2,
					remoteValue:   0x1,
					expectedValue: 0x1,
				},
				{
					offset:        0x34C,
					localValue:    2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute (active) from remote",
		},
		{
			name: "VT flute",
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Flute (inactive) from remote",
		},
		{
			name: "VT flute from shovel",
			fields: sramTestCaseFields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0x4,
					remoteValue:   0x2,
					expectedValue: 0x6,
				},
				{
					offset:        0x34C,
					localValue:    1,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Flute (inactive) from remote",
		},
		{
			name: "VT shovel",
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Shovel from remote",
		},
		{
			name: "VT shovel from flute",
			fields: sramTestCaseFields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0x2,
					remoteValue:   0x4,
					expectedValue: 0x6,
				},
				{
					offset:        0x34C,
					localValue:    2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Shovel from remote",
		},
		{
			name: "VT shovel from flute (activated)",
			fields: sramTestCaseFields{
				// ROM title must start with "VT " to indicate randomizer
				ROMTitle: "VT test",
			},
			sram: []sramTest{
				{
					offset:        0x38C,
					localValue:    0x1,
					remoteValue:   0x4,
					expectedValue: 0x5,
				},
				{
					offset:        0x34C,
					localValue:    3,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Shovel from remote",
		},
		{
			name: "VT red boomerang",
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Red Boomerang from remote",
		},
		{
			name: "VT blue boomerang",
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Blue Boomerang from remote",
		},
		{
			name: "VT bow no arrows",
			fields: sramTestCaseFields{
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
					// expect bow w/o arrows:
					offset:        0x340,
					expectedValue: 1,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "VT bow with arrows",
			fields: sramTestCaseFields{
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
					// expect bow w/ arrows:
					offset:        0x340,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "VT bow no change",
			fields: sramTestCaseFields{
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
					// already have silvers selected, don't alter selection:
					offset:        0x340,
					localValue:    3,
					expectedValue: 3,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Bow from remote",
		},
		{
			name: "VT silver bow no arrows",
			fields: sramTestCaseFields{
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
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "VT silver bow with arrows",
			fields: sramTestCaseFields{
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
					// expect silver bow w/ arrows:
					offset:        0x340,
					expectedValue: 4,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
		{
			name: "VT silver bow no change",
			fields: sramTestCaseFields{
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
					// already have bow selected, don't alter selection:
					offset:        0x340,
					localValue:    2,
					expectedValue: 2,
				},
			},
			wantUpdated:      true,
			wantNotification: "got Silver Bow from remote",
		},
	}

	runAsmEmulationTests(t, tests)
}
