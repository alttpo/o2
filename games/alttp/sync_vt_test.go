package alttp

import (
	"fmt"
	"testing"
)

func TestAsm_VT_Items(t *testing.T) {
	sramTests := []sramTestCase{
		{
			name:        "No update",
			wantUpdated: false,
		},
		{
			name: "VT mushroom",
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

	// convert legacy tests to frame tests:
	tests := make([]testCase, 0, len(sramTests))
	for _, legacy := range sramTests {
		fr := frame{
			preGenLocal:       make([]wramSetValue, len(legacy.sram)),
			preGenRemote:      make([]wramSetValue, len(legacy.sram)),
			wantAsm:           legacy.wantUpdated,
			preAsmLocal:       nil,
			postAsmLocal:      make([]wramTestValue, len(legacy.sram)),
			wantNotifications: nil,
		}
		for i, s := range legacy.sram {
			fr.preGenLocal[i].offset = 0xF000 + s.offset
			fr.preGenLocal[i].value = s.localValue
			fr.preGenRemote[i].offset = 0xF000 + s.offset
			fr.preGenRemote[i].value = s.remoteValue
			fr.postAsmLocal[i].offset = 0xF000 + s.offset
			fr.postAsmLocal[i].value = s.expectedValue
		}
		if legacy.wantNotification != "" {
			fr.wantNotifications = []string{legacy.wantNotification}
		}
		test := testCase{
			name:      legacy.name,
			module:    0x07,
			subModule: 0x00,
			frames:    []frame{fr},
		}
		tests = append(tests, test)
	}

	// create system emulator and test ROM:
	// ROM title must start with "VT " to indicate randomizer
	system, rom, err := CreateTestEmulator(t, "VT test")
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	// run each test:
	for i := range tests {
		tt := &tests[i]
		tt.system = system
		tt.g = g
		t.Run(tt.name, tt.runFrameTest)
	}
}

func TestAsm_VT_ItemBits(t *testing.T) {
	tests := make([]testCase, 0, len(vtItemBitNames)*8)

	for offs := uint16(0x38C); offs <= 0x38E; offs++ {
		bitNames, ok := vtItemBitNames[offs]
		if !ok {
			continue
		}

		for i := range bitNames {
			bitName := bitNames[i]
			var expectedNotifications []string
			if bitName != "" {
				expectedNotifications = []string{fmt.Sprintf("got %s from remote", bitName)}
			}

			wramOffs := 0xF000 + offs

			tests = append(tests, testCase{
				name:      fmt.Sprintf("$%03x bit %d", offs, i),
				module:    0x07,
				subModule: 0x00,
				frames: []frame{
					{
						preGenLocal: []wramSetValue{
							{wramOffs, 0},
						},
						preGenRemote: []wramSetValue{
							{wramOffs, 1 << i},
						},
						wantAsm: true,
						postAsmLocal: []wramTestValue{
							{wramOffs, 1 << i},
						},
						wantNotifications: expectedNotifications,
					},
				},
			})
		}
	}

	// create system emulator and test ROM:
	// ROM title must start with "VT " to indicate randomizer
	system, rom, err := CreateTestEmulator(t, "VT test")
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	// run each test:
	for i := range tests {
		tt := &tests[i]
		tt.system = system
		tt.g = g
		t.Run(tt.name, tt.runFrameTest)
	}
}
