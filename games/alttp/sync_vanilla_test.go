package alttp

import (
	"fmt"
	"testing"
)

func TestAsm_Vanilla_Items(t *testing.T) {
	sramTests := []sramTestCase{
		{
			name:        "No update",
			wantUpdated: false,
		},
		{
			name: "Mushroom",
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

	// convert legacy tests to frame tests:
	tests := make([]testCase, 0, len(sramTests))
	for _, legacy := range sramTests {
		for _, variant := range moduleVariants {
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
				if variant.allowed {
					fr.postAsmLocal[i].value = s.expectedValue
				} else {
					fr.postAsmLocal[i].value = s.localValue
				}
			}
			if variant.allowed {
				if legacy.wantNotification != "" {
					fr.wantNotifications = []string{legacy.wantNotification}
				}
			} else {
				fr.wantAsm = false
			}
			test := testCase{
				name:      fmt.Sprintf("%02x,%02x %s", variant.module, variant.submodule, legacy.name),
				module:    variant.module,
				subModule: variant.submodule,
				frames:    []frame{fr},
			}
			tests = append(tests, test)
		}
	}

	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
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

func TestAsmFrames_Vanilla_ItemNames(t *testing.T) {
	tests := make([]testCase, 0, len(vanillaItemNames))

	for offs := uint16(0x341); offs <= 0x37B; offs++ {
		if offs >= 0x35C && offs <= 0x35F {
			// skip bottles since they have special logic:
			continue
		}

		itemNames, ok := vanillaItemNames[offs]
		if !ok {
			continue
		}

		wramOffs := 0xF000 + offs
		for i, itemName := range itemNames {
			for _, variant := range moduleVariants {
				// basic sync in:
				test := testCase{
					name:      fmt.Sprintf("%02x,%02x %04x %02x good", variant.module, variant.submodule, wramOffs, i+1),
					module:    variant.module,
					subModule: variant.submodule,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs, 0},
							},
							preGenRemote: []wramSetValue{
								{wramOffs, uint8(i + 1)},
							},
							wantAsm:     true,
							preAsmLocal: nil,
							postAsmLocal: []wramTestValue{
								{wramOffs, uint8(i + 1)},
							},
							wantNotifications: []string{
								fmt.Sprintf("got %s from remote", itemName),
							},
						},
					},
				}
				if !variant.allowed {
					test.frames[0].wantAsm = false
					test.frames[0].wantNotifications = nil
					test.frames[0].postAsmLocal[0].value = test.frames[0].preGenLocal[0].value
				}
				tests = append(tests, test)

				// expected fail from ASM code:
				test = testCase{
					name:      fmt.Sprintf("%02x,%02x %04x %02x xfail", variant.module, variant.submodule, wramOffs, i+1),
					module:    variant.module,
					subModule: variant.submodule,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs, 0},
							},
							preGenRemote: []wramSetValue{
								{wramOffs, uint8(i + 1)},
							},
							wantAsm: true,
							// just got it this frame:
							preAsmLocal: []wramSetValue{
								{wramOffs, uint8(i + 1)},
							},
							postAsmLocal: []wramTestValue{
								{wramOffs, uint8(i + 1)},
							},
							wantNotifications: nil,
						},
					},
				}
				if !variant.allowed {
					test.frames[0].wantAsm = false
					test.frames[0].wantNotifications = nil
				}
				tests = append(tests, test)
			}
		}
	}

	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
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

func TestAsmFrames_Vanilla_ItemBitNames(t *testing.T) {
	tests := make([]testCase, 0, len(vanillaItemBitNames))

	for offs := uint16(0x341); offs <= 0x37B; offs++ {
		if offs >= 0x35C && offs <= 0x35F {
			// skip bottles since they have special logic:
			continue
		}

		itemNames, ok := vanillaItemBitNames[offs]
		if !ok {
			continue
		}

		wramOffs := 0xF000 + offs

		for i, itemName := range itemNames {
			if itemName == "" {
				continue
			}

			for _, variant := range moduleVariants {
				// good
				test := testCase{
					name:      fmt.Sprintf("%02x,%02x %04x %d good", variant.module, variant.submodule, wramOffs, i),
					module:    variant.module,
					subModule: variant.submodule,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs, 0},
							},
							preGenRemote: []wramSetValue{
								{wramOffs, uint8(1 << i)},
							},
							wantAsm: true,
							postAsmLocal: []wramTestValue{
								{wramOffs, uint8(1 << i)},
							},
							wantNotifications: []string{
								fmt.Sprintf("got %s from remote", itemName),
							},
						},
					},
				}
				if !variant.allowed {
					test.frames[0].wantAsm = false
					test.frames[0].wantNotifications = nil
					test.frames[0].postAsmLocal[0].value = test.frames[0].preGenLocal[0].value
				}
				tests = append(tests, test)

				// expected fail from ASM
				test = testCase{
					name:      fmt.Sprintf("%02x,%02x %04x %d xfail", variant.module, variant.submodule, wramOffs, i),
					module:    variant.module,
					subModule: variant.submodule,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs, 0},
							},
							preGenRemote: []wramSetValue{
								{wramOffs, uint8(1 << i)},
							},
							wantAsm: true,
							// just got it this frame:
							preAsmLocal: []wramSetValue{
								{wramOffs, uint8(1 << i)},
							},
							postAsmLocal: []wramTestValue{
								{wramOffs, uint8(1 << i)},
							},
							wantNotifications: nil,
						},
					},
				}
				if !variant.allowed {
					test.frames[0].wantAsm = false
					test.frames[0].wantNotifications = nil
				}
				tests = append(tests, test)
			}
		}
	}

	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
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

func TestAsmFrames_Vanilla_Bottles(t *testing.T) {
	tests := make([]testCase, 0, len(vanillaItemBitNames))

	for offs := uint16(0x35C); offs <= 0x35F; offs++ {
		bottleItemNames := vanillaBottleItemNames[1:]

		wramOffs := 0xF000 + offs

		// positive tests:
		for i := range bottleItemNames {
			bottleValue := uint8(i + 2)
			itemName := bottleItemNames[i]

			tests = append(tests, testCase{
				name:      fmt.Sprintf("%04x bottle 0 to %d", wramOffs, bottleValue),
				module:    0x07,
				subModule: 0x00,
				frames: []frame{
					{
						preGenLocal: []wramSetValue{
							{wramOffs, 0},
						},
						preGenRemote: []wramSetValue{
							{wramOffs, bottleValue},
						},
						wantAsm: true,
						postAsmLocal: []wramTestValue{
							{wramOffs, bottleValue},
						},
						wantNotifications: []string{
							fmt.Sprintf("got %s from remote", itemName),
						},
					},
				},
			})
		}

		// negative tests:
		for j := range bottleItemNames {
			localBottle := uint8(j + 2)
			for i := range bottleItemNames {
				remoteBottle := uint8(i + 2)

				tests = append(tests, testCase{
					name:      fmt.Sprintf("%04x bottle %d to %d", wramOffs, localBottle, remoteBottle),
					module:    0x07,
					subModule: 0x00,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs, localBottle},
							},
							preGenRemote: []wramSetValue{
								{wramOffs, remoteBottle},
							},
							wantAsm: false,
							postAsmLocal: []wramTestValue{
								{wramOffs, localBottle},
							},
							wantNotifications: nil,
						},
					},
				})
			}
		}
	}

	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
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

func TestAsmFrames_Vanilla_UnderworldRooms(t *testing.T) {
	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(underworldNames))

	for room := uint16(0); room < 0x128; room++ {
		name, ok := underworldNames[room]
		if !ok {
			continue
		}

		wramOffs := 0xF000 + room<<1

		tests = append(tests, testCase{
			name:      fmt.Sprintf("Room %03x: %s", room, name),
			module:    0x07,
			subModule: 0x00,
			frames: []frame{
				{
					preGenLocal: []wramSetValue{
						{wramOffs, 0},
					},
					preGenRemote: []wramSetValue{
						// quadrants visited:
						{wramOffs, 0b0000_1111},
					},
					wantAsm: true,
					postAsmLocal: []wramTestValue{
						{wramOffs, 0b0000_1111},
					},
				},
			},
		})

		for bit := 0; bit < 8; bit++ {
			lowbit := bit
			lowBitName := g.underworld[room].BitNames[lowbit]
			if lowBitName != "" && g.underworld[room].SyncMask&(1<<lowbit) != 0 {
				// low bits:
				tests = append(tests, testCase{
					name:      fmt.Sprintf("Room %03x: %s bit %d", room, name, lowbit),
					module:    0x07,
					subModule: 0x00,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs, 0},
							},
							preGenRemote: []wramSetValue{
								{wramOffs, 1 << bit},
							},
							wantAsm: true,
							postAsmLocal: []wramTestValue{
								{wramOffs, 1 << bit},
							},
							wantNotifications: []string{
								fmt.Sprintf("got %s from remote", lowBitName),
							},
						},
					},
				})
			}

			highbit := bit + 8
			highBitName := g.underworld[room].BitNames[highbit]
			if highBitName != "" && g.underworld[room].SyncMask&(1<<highbit) != 0 {
				// high bits:
				wantNotifications := []string{
					fmt.Sprintf("got %s from remote", highBitName),
				}
				// exception for Agahnim defeated to open HC portal as well:
				if room == 0x020 && highbit == 11 {
					wantNotifications = []string{"HC portal opened", "got Agahnim defeated from remote"}
				}

				tests = append(tests, testCase{
					name:      fmt.Sprintf("Room %03x: %s bit %d", room, name, highbit),
					module:    0x07,
					subModule: 0x00,
					frames: []frame{
						{
							preGenLocal: []wramSetValue{
								{wramOffs + 1, 0},
							},
							preGenRemote: []wramSetValue{
								{wramOffs + 1, 1 << bit},
							},
							wantAsm: true,
							postAsmLocal: []wramTestValue{
								{wramOffs + 1, 1 << bit},
							},
							wantNotifications: wantNotifications,
						},
					},
				})
			}
		}
	}

	// run each test:
	for i := range tests {
		tt := &tests[i]
		tt.system = system
		tt.g = g
		t.Run(tt.name, tt.runFrameTest)
	}
}
