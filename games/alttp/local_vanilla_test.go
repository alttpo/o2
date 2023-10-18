package alttp

import (
	"fmt"
	"testing"
)

func TestLocal_Vanilla_ItemNames(t *testing.T) {
	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator("ZELDANODENSETSU", t)
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(vanillaItemNames))

	for wramOffs := g.syncableOffsMin; wramOffs <= g.syncableOffsMax; wramOffs++ {
		if wramOffs < 0xF000 {
			continue
		}

		offs := uint16(wramOffs - 0xF000)
		if offs >= 0x35C && offs <= 0x35F {
			// skip bottles since they have special logic:
			continue
		}

		itemNames, ok := vanillaItemNames[offs]
		if !ok {
			continue
		}
		verbNames, ok := vanillaItemVerbs[offs]

		for i, itemName := range itemNames {
			module := uint8(0x07)
			subModule := uint8(0x00)

			verb := "picked up"
			if i < len(verbNames) && verbNames[i] != "" {
				verb = verbNames[i]
			}

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
							fmt.Sprintf("%s %s", verb, itemName),
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
	system, rom, err := CreateTestEmulator("ZELDANODENSETSU", t)
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(vanillaItemBitNames))

	for offs := g.syncableOffsMin; offs <= g.syncableOffsMax; offs++ {
		if offs >= 0x35C && offs <= 0x35F {
			// skip bottles since they have special logic:
			continue
		}

		itemNames, ok := vanillaItemBitNames[uint16(offs)]
		if !ok {
			continue
		}
		verbs := vanillaItemBitVerbs[uint16(offs)]

		wramOffs := 0xF000 + offs

		for i, itemName := range itemNames {
			if itemName == "" {
				continue
			}
			verb := verbs[i]
			if verb == "" {
				verb = "picked up"
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
							fmt.Sprintf("%s %s", verb, itemName),
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

		wramOffs := uint32(0xF000 + offs)

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
	system, rom, err := CreateTestEmulator("ZELDANODENSETSU", t)
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
	system, rom, err := CreateTestEmulator("ZELDANODENSETSU", t)
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(underworldNames)*16)

	for room := uint16(0); room < 0x128; room++ {
		name, ok := underworldNames[room]
		if !ok {
			continue
		}

		wramOffs := uint32(0xF000 + room<<1)

		u := &g.underworld[room]
		for bit := 0; bit < 8; bit++ {
			lowbit := bit
			lowBitName := u.BitNames[lowbit]
			var expectedNotifications []string = nil
			if lowBitName != "" {
				// low bits:
				expectedNotifications = []string{
					fmt.Sprintf("%s %s at %s", u.Verbs[lowbit], lowBitName, name),
				}
			}
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
						wantAsm:           false,
						wantNotifications: expectedNotifications,
					},
				},
			})

			highbit := bit + 8
			highBitName := u.BitNames[highbit]
			expectedNotifications = nil
			if highBitName != "" {
				// high bits:
				expectedNotifications = []string{
					fmt.Sprintf("%s %s at %s", u.Verbs[highbit], highBitName, name),
				}
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
						wantNotifications: expectedNotifications,
					},
				},
			})
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

func TestLocal_Vanilla_OverworldRooms(t *testing.T) {
	// create system emulator and test ROM:
	system, rom, err := CreateTestEmulator("ZELDANODENSETSU", t)
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	tests := make([]testCase, 0, len(overworldNames))

	for area := uint16(0); area < 0xC0; area++ {
		name, ok := overworldNames[area]
		if !ok {
			continue
		}

		wramOffs := uint32(0xF280 + area)

		u := &g.overworld[area]
		for bit := 0; bit < 8; bit++ {
			lowbit := bit
			lowBitName := u.BitNames[lowbit]

			var expectedNotifications []string
			if lowBitName != "" {
				// low bits:
				expectedNotifications = []string{
					fmt.Sprintf("%s %s at %s", u.Verbs[lowbit], lowBitName, overworldNames[area]),
				}
			}

			tests = append(tests, testCase{
				name:      fmt.Sprintf("Area %03x: %s bit %d", area, name, lowbit),
				module:    0x09,
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
						wantAsm:           false,
						wantNotifications: expectedNotifications,
					},
				},
			})
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
