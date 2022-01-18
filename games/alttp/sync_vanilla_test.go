package alttp

import (
	"fmt"
	"testing"
)

func TestAsm_Vanilla_Items(t *testing.T) {
	tests := []sramTestCase{
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

	runAsmEmulationTests(t, "ZELDANODENSETSU", tests)
}

func TestAsm_Vanilla_ItemNames(t *testing.T) {
	tests := make([]sramTestCase, 0, len(vanillaItemNames))

	for offs := uint16(0x341); offs <= 0x37B; offs++ {
		if offs >= 0x35C && offs <= 0x35F {
			// skip bottles since they have special logic:
			continue
		}

		itemNames, ok := vanillaItemNames[offs]
		if !ok {
			continue
		}

		for i, itemName := range itemNames {
			tests = append(tests, sramTestCase{
				name: fmt.Sprintf("Slot $%03x Item %d", offs, i+1),
				sram: []sramTest{
					{
						offset:        offs,
						localValue:    0,
						remoteValue:   uint8(i + 1),
						expectedValue: uint8(i + 1),
					},
				},
				wantUpdated:      true,
				wantNotification: fmt.Sprintf("got %s from remote", itemName),
			})
		}
	}

	runAsmEmulationTests(t, "ZELDANODENSETSU", tests)
}

func TestAsm_Vanilla_ItemBitNames(t *testing.T) {
	tests := make([]sramTestCase, 0, len(vanillaItemBitNames))

	for offs := uint16(0x341); offs <= 0x37B; offs++ {
		if offs >= 0x35C && offs <= 0x35F {
			// skip bottles since they have special logic:
			continue
		}

		itemNames, ok := vanillaItemBitNames[offs]
		if !ok {
			continue
		}

		for i, itemName := range itemNames {
			if itemName == "" {
				continue
			}

			tests = append(tests, sramTestCase{
				name: fmt.Sprintf("Slot $%03x Item Flag %d", offs, i),
				sram: []sramTest{
					{
						offset:        offs,
						localValue:    0,
						remoteValue:   uint8(1 << i),
						expectedValue: uint8(1 << i),
					},
				},
				wantUpdated:      true,
				wantNotification: fmt.Sprintf("got %s from remote", itemName),
			})
		}
	}

	runAsmEmulationTests(t, "ZELDANODENSETSU", tests)
}

func TestAsm_Vanilla_Bottles(t *testing.T) {
	tests := make([]sramTestCase, 0, len(vanillaItemBitNames))

	for offs := uint16(0x35C); offs <= 0x35F; offs++ {
		bottleItemNames := vanillaBottleItemNames[1:]

		// positive tests:
		for i := range bottleItemNames {
			bottleValue := uint8(i + 2)
			itemName := bottleItemNames[i]
			expectedNotification := fmt.Sprintf("got %s from remote", itemName)

			tests = append(tests, sramTestCase{
				name: fmt.Sprintf("Slot $%03x Bottle 0 to %d", offs, bottleValue),
				sram: []sramTest{
					{
						offset:        offs,
						localValue:    0,
						remoteValue:   bottleValue,
						expectedValue: bottleValue,
					},
				},
				wantUpdated:      true,
				wantNotification: expectedNotification,
			})
		}

		// negative tests:
		for j := range bottleItemNames {
			localBottle := uint8(j + 2)
			for i := range bottleItemNames {
				remoteBottle := uint8(i + 2)

				tests = append(tests, sramTestCase{
					name: fmt.Sprintf("Slot $%03x Bottle %d to %d", offs, localBottle, remoteBottle),
					sram: []sramTest{
						{
							offset:        offs,
							localValue:    localBottle,
							remoteValue:   remoteBottle,
							expectedValue: localBottle,
						},
					},
					wantUpdated:      false,
					wantNotification: "",
				})
			}
		}
	}

	runAsmEmulationTests(t, "ZELDANODENSETSU", tests)
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

	runAsmEmulationTests(t, "ZELDANODENSETSU", tests)
}
