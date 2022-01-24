package alttp

import (
	"fmt"
	"testing"
)

func TestLocal_Vanilla_ItemNames(t *testing.T) {
	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(vanillaItemNames))

	for offs := g.syncableItemsMin; offs <= g.syncableItemsMax; offs++ {
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
			module := uint8(0x07)
			subModule := uint8(0x00)

			// picked up item:
			test := testCase{
				name:      fmt.Sprintf("%02x,%02x %04x %02x good", module, subModule, wramOffs, i+1),
				module:    module,
				subModule: subModule,
				frames: []frame{
					{
						preGenLocal: []wramSetValue{
							{wramOffs, 0},
						},
						wantAsm: false,
					},
					{
						preGenLocal: []wramSetValue{
							{wramOffs, uint8(i + 1)},
						},
						wantAsm: false,
						wantNotifications: []string{
							fmt.Sprintf("picked up %s", itemName),
						},
					},
				},
			}
			tests = append(tests, test)

			// no change:
			test = testCase{
				name:      fmt.Sprintf("%02x,%02x %04x %02x xfail", module, subModule, wramOffs, i+1),
				module:    module,
				subModule: subModule,
				frames: []frame{
					{
						preGenLocal: []wramSetValue{
							{wramOffs, uint8(i + 1)},
						},
						wantAsm: false,
					},
					{
						preGenLocal: []wramSetValue{
							{wramOffs, uint8(i + 1)},
						},
						wantAsm:           false,
						wantNotifications: nil,
					},
				},
			}
			tests = append(tests, test)
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

func TestLocal_Vanilla_ItemBitNames(t *testing.T) {
	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(vanillaItemBitNames))

	for offs := g.syncableItemsMin; offs <= g.syncableItemsMax; offs++ {
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

			module := uint8(0x07)
			subModule := uint8(0x00)
			// good
			test := testCase{
				name:      fmt.Sprintf("%02x,%02x %04x %d good", module, subModule, wramOffs, i),
				module:    module,
				subModule: subModule,
				frames: []frame{
					{
						preGenLocal: []wramSetValue{
							{wramOffs, 0},
						},
						wantAsm: false,
					},
					{
						preGenLocal: []wramSetValue{
							{wramOffs, uint8(1 << i)},
						},
						wantAsm: false,
						wantNotifications: []string{
							fmt.Sprintf("picked up %s", itemName),
						},
					},
				},
			}
			tests = append(tests, test)

			// expected fail
			test = testCase{
				name:      fmt.Sprintf("%02x,%02x %04x %d xfail", module, subModule, wramOffs, i),
				module:    module,
				subModule: subModule,
				frames: []frame{
					{
						preGenLocal: []wramSetValue{
							{wramOffs, uint8(1 << i)},
						},
						wantAsm: false,
					},
					{
						preGenLocal: []wramSetValue{
							{wramOffs, uint8(1 << i)},
						},
						wantNotifications: nil,
					},
				},
			}
			tests = append(tests, test)
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

func TestLocal_Vanilla_Bottles(t *testing.T) {
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
						wantAsm: false,
					},
					{
						preGenLocal: []wramSetValue{
							{wramOffs, bottleValue},
						},
						wantAsm: false,
						wantNotifications: []string{
							fmt.Sprintf("picked up %s", itemName),
						},
					},
				},
			})
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

func TestLocal_Vanilla_UnderworldRooms(t *testing.T) {
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

		u := &g.underworld[room]
		for bit := 0; bit < 8; bit++ {
			lowbit := bit
			lowBitName := u.BitNames[lowbit]
			if lowBitName != "" {
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
						},
						{
							preGenLocal: []wramSetValue{
								{wramOffs, 1 << bit},
							},
							wantAsm: false,
							wantNotifications: []string{
								fmt.Sprintf("local %s", lowBitName),
							},
						},
					},
				})
			}

			highbit := bit + 8
			highBitName := u.BitNames[highbit]
			if highBitName != "" {
				// high bits:
				wantNotifications := []string{
					fmt.Sprintf("local %s", highBitName),
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
						},
						{
							preGenLocal: []wramSetValue{
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
