package alttp

import (
	"fmt"
	"testing"
	"time"
)

func sameDungeonSmallKeyTest(
	variant moduleVariant,
	wramOffs uint32,
	dungeonNumber uint32,
	rightMeow time.Time,
	initialValue, preAsmValue, remoteValue, expectedValue uint8,
) testCase {
	test := testCase{
		name:      fmt.Sprintf("%02x,%02x dungeon=%02x %04x %d->%d", variant.module, variant.submodule, dungeonNumber, wramOffs, initialValue, remoteValue),
		module:    variant.module,
		subModule: variant.submodule,
		frames: []frame{
			{
				preGenLocal: []wramSetValue{
					{0x040c, uint8(dungeonNumber << 1)}, // current dungeon
					{0xf36f, initialValue},              // current dungeon key counter
					{wramOffs, initialValue},            // dungeon array key counter
				},
				preGenLocalUpdate: func(local *Player) {
					lw := local.WRAM[uint16(wramOffs)]
					lw.Timestamp = timestampFromTime(rightMeow) + 1
				},
				preGenRemoteUpdate: func(remote *Player) {
					if remote.WRAM == nil {
						remote.WRAM = make(map[uint16]*SyncableWRAM)
					}
					remote.WRAM[uint16(wramOffs)] = &SyncableWRAM{
						Name:      fmt.Sprintf("wram[$%04x]", wramOffs),
						Size:      1,
						Timestamp: timestampFromTime(rightMeow) + 2,
						Value:     uint16(remoteValue),
					}
				},
				wantAsm: true,
				preAsmLocal: []wramSetValue{
					{0xf36f, preAsmValue},
					{wramOffs, preAsmValue},
				},
				postAsmLocal: []wramTestValue{
					{0xf36f, expectedValue},
					{wramOffs, expectedValue},
				},
				wantNotifications: []string{
					fmt.Sprintf("update %s small keys to %d from remote", dungeonNames[uint16(wramOffs)-smallKeyFirst], remoteValue),
				},
			},
		},
	}

	if !variant.allowed {
		test.frames[0].wantAsm = false
		test.frames[0].wantNotifications = nil
		test.frames[0].postAsmLocal[0].value = preAsmValue
		test.frames[0].postAsmLocal[1].value = preAsmValue
	}

	return test
}

func diffDungeonSmallKeyTest(
	variant moduleVariant,
	wramOffs uint32,
	dungeonNumber uint32,
	rightMeow time.Time,
	initialValue, preAsmValue, remoteValue, expectedValue uint8,
) testCase {
	test := testCase{
		name:      fmt.Sprintf("%02x,%02x dungeon=%02x %04x %d->%d", variant.module, variant.submodule, dungeonNumber, wramOffs, initialValue, remoteValue),
		module:    variant.module,
		subModule: variant.submodule,
		frames: []frame{
			{
				preGenLocal: []wramSetValue{
					{0x040c, uint8(dungeonNumber << 1)}, // current dungeon
					{wramOffs, initialValue},            // dungeon key counter
					{0xf36f, 7},                         // current unrelated dungeon key counter
				},
				preGenLocalUpdate: func(local *Player) {
					lw := local.WRAM[uint16(wramOffs)]
					lw.Timestamp = timestampFromTime(rightMeow) + 1
				},
				preGenRemoteUpdate: func(remote *Player) {
					if remote.WRAM == nil {
						remote.WRAM = make(map[uint16]*SyncableWRAM)
					}
					remote.WRAM[uint16(wramOffs)] = &SyncableWRAM{
						Name:      fmt.Sprintf("wram[$%04x]", wramOffs),
						Size:      1,
						Timestamp: timestampFromTime(rightMeow) + 2,
						Value:     uint16(remoteValue),
					}
				},
				wantAsm: true,
				preAsmLocal: []wramSetValue{
					{wramOffs, preAsmValue},
				},
				postAsmLocal: []wramTestValue{
					{wramOffs, expectedValue},
					{0xf36f, 7}, // must remain unchanged
				},
				wantNotifications: []string{
					fmt.Sprintf("update %s small keys to %d from remote", dungeonNames[uint16(wramOffs)-smallKeyFirst], remoteValue),
				},
			},
		},
	}

	if variant.allowed {
		// verify HC<->sewer key sync:
		if wramOffs == uint32(smallKeyFirst) {
			test.frames[0].preAsmLocal = append(test.frames[0].preAsmLocal, wramSetValue{uint32(smallKeyFirst + 1), preAsmValue})
			test.frames[0].postAsmLocal = append(test.frames[0].postAsmLocal, wramTestValue{uint32(smallKeyFirst + 1), expectedValue})
		} else if wramOffs == uint32(smallKeyFirst+1) {
			test.frames[0].preAsmLocal = append(test.frames[0].preAsmLocal, wramSetValue{uint32(smallKeyFirst), preAsmValue})
			test.frames[0].postAsmLocal = append(test.frames[0].postAsmLocal, wramTestValue{uint32(smallKeyFirst), expectedValue})
		}
	} else {
		test.frames[0].wantAsm = false
		test.frames[0].wantNotifications = nil
		test.frames[0].postAsmLocal[0].value = preAsmValue
	}

	return test
}

func caveSmallKeyTest(
	variant moduleVariant,
	wramOffs uint32,
	rightMeow time.Time,
	initialValue, preAsmValue, remoteValue, expectedValue uint8,
) testCase {
	test := testCase{
		name:      fmt.Sprintf("%02x,%02x dungeon=%02x %04x %d->%d", variant.module, variant.submodule, 0xFF, wramOffs, initialValue, remoteValue),
		module:    variant.module,
		subModule: variant.submodule,
		frames: []frame{
			{
				preGenLocal: []wramSetValue{
					{wramOffs, initialValue}, // small key counter for specific dungeon
					{0xf36f, 0xFF},           // current dungeon key counter
					{0x040c, 0xFF},           // current dungeon is cave
				},
				preGenLocalUpdate: func(local *Player) {
					lw := local.WRAM[uint16(wramOffs)]
					lw.Timestamp = timestampFromTime(rightMeow) + 1
				},
				preGenRemoteUpdate: func(remote *Player) {
					if remote.WRAM == nil {
						remote.WRAM = make(map[uint16]*SyncableWRAM)
					}
					remote.WRAM[uint16(wramOffs)] = &SyncableWRAM{
						Name:      fmt.Sprintf("wram[$%04x]", wramOffs),
						Size:      1,
						Timestamp: timestampFromTime(rightMeow) + 2,
						Value:     uint16(remoteValue),
					}
				},
				wantAsm: true,
				preAsmLocal: []wramSetValue{
					{wramOffs, preAsmValue},
				},
				postAsmLocal: []wramTestValue{
					{wramOffs, expectedValue},
					{0xf36f, 0xFF},
				},
				wantNotifications: []string{
					fmt.Sprintf("update %s small keys to %d from remote", dungeonNames[uint16(wramOffs)-smallKeyFirst], remoteValue),
				},
			},
		},
	}

	if variant.allowed {
		// verify HC<->sewer key sync:
		if wramOffs == uint32(smallKeyFirst) {
			test.frames[0].preAsmLocal = append(test.frames[0].preAsmLocal, wramSetValue{uint32(smallKeyFirst + 1), expectedValue})
			test.frames[0].postAsmLocal = append(test.frames[0].postAsmLocal, wramTestValue{uint32(smallKeyFirst + 1), expectedValue})
		} else if wramOffs == uint32(smallKeyFirst+1) {
			test.frames[0].preAsmLocal = append(test.frames[0].preAsmLocal, wramSetValue{uint32(smallKeyFirst), expectedValue})
			test.frames[0].postAsmLocal = append(test.frames[0].postAsmLocal, wramTestValue{uint32(smallKeyFirst), expectedValue})
		}
	} else {
		test.frames[0].wantAsm = false
		test.frames[0].wantNotifications = nil
		test.frames[0].postAsmLocal[0].value = preAsmValue
	}

	return test
}

func TestAsmFrames_Vanilla_SmallKeys(t *testing.T) {
	tests := make([]testCase, 0, len(vanillaItemNames))

	rightMeow := time.Now()

	for wramOffs := uint32(smallKeyFirst); wramOffs <= uint32(smallKeyLast); wramOffs++ {
		for _, variant := range moduleVariants {
			// normal increment from remote:
			tests = append(tests, caveSmallKeyTest(variant, wramOffs, rightMeow, 0, 0, 1, 1))
			// normal decrement from remote:
			tests = append(tests, caveSmallKeyTest(variant, wramOffs, rightMeow, 2, 2, 1, 1))
			// both local and remote decremented:
			tests = append(tests, caveSmallKeyTest(variant, wramOffs, rightMeow, 2, 1, 0, 0))
		}

		for dungeonNumber := uint32(0); dungeonNumber <= uint32(smallKeyLast-smallKeyFirst); dungeonNumber++ {
			var doDiffTest bool
			keyDungeon := wramOffs - uint32(smallKeyFirst)
			if dungeonNumber == keyDungeon {
				doDiffTest = false
			} else if keyDungeon <= 1 && dungeonNumber <= 1 {
				doDiffTest = false
			} else {
				doDiffTest = true
			}

			if doDiffTest {
				for _, variant := range moduleVariants {
					// normal increment from remote:
					tests = append(tests, diffDungeonSmallKeyTest(variant, wramOffs, dungeonNumber, rightMeow, 0, 0, 1, 1))
					// normal decrement from remote:
					tests = append(tests, diffDungeonSmallKeyTest(variant, wramOffs, dungeonNumber, rightMeow, 2, 2, 1, 1))
					// both local and remote decremented:
					tests = append(tests, diffDungeonSmallKeyTest(variant, wramOffs, dungeonNumber, rightMeow, 2, 1, 0, 0))
				}
			} else {
				for _, variant := range moduleVariants {
					// normal increment from remote:
					tests = append(tests, sameDungeonSmallKeyTest(variant, wramOffs, dungeonNumber, rightMeow, 0, 0, 1, 1))
					// normal decrement from remote:
					tests = append(tests, sameDungeonSmallKeyTest(variant, wramOffs, dungeonNumber, rightMeow, 2, 2, 1, 1))
					// both local and remote decremented:
					tests = append(tests, sameDungeonSmallKeyTest(variant, wramOffs, dungeonNumber, rightMeow, 2, 1, 0, 0))
				}
			}
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
		g.lastServerTime = rightMeow
		g.lastServerRecvTime = rightMeow
		t.Run(tt.name, tt.runFrameTest)
	}
}

func TestGameSync_SmallKeys_Ideal(t *testing.T) {
	for dungeonIndex := uint8(0); dungeonIndex <= 1; dungeonIndex++ {
		tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// set both modules to $07, dungeons to $00:
					gs[0].e.WRAM[0x10] = 0x07
					gs[1].e.WRAM[0x10] = 0x07
					gs[0].e.WRAM[0x040C] = dungeonIndex
					gs[1].e.WRAM[0x040C] = dungeonIndex
				},
			},
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// inc current dungeon key counter:
					gs[0].e.WRAM[0xF36F] = 1
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 updated its current small key counter:
					if expected, actual := uint8(1), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(1), gs[1].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(1), gs[1].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 confirmed last update:
					if len(gs[1].n) != 2 {
						t.Errorf("expected %d notifications actual %d", 2, len(gs[1].n))
					}
					if expected, actual := "update Sewer Passage small keys to 1 from g1", gs[1].n[0]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
					if expected, actual := "update Hyrule Castle small keys to 1 from g1", gs[1].n[1]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no redundant notifications:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
		})

		t.Run(fmt.Sprintf("smallkeys_ideal_d%02x", dungeonIndex<<1), tc.runGameSyncTest)
	}

	for dungeonIndex := uint8(2); dungeonIndex < 14; dungeonIndex++ {
		tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// set both modules to $07:
					gs[0].e.WRAM[0x10] = 0x07
					gs[1].e.WRAM[0x10] = 0x07
					gs[0].e.WRAM[0x040C] = dungeonIndex << 1
					gs[1].e.WRAM[0x040C] = dungeonIndex << 1
				},
			},
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// inc current dungeon key counter:
					gs[0].e.WRAM[0xF36F] = 1
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 updated its current small key counter:
					if expected, actual := uint8(1), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					offs := smallKeyFirst + uint16(dungeonIndex)
					if expected, actual := uint8(1), gs[1].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 confirmed last update:
					if len(gs[1].n) != 1 {
						t.Errorf("expected %d notifications actual %d", 1, len(gs[1].n))
					}
					if expected, actual := fmt.Sprintf("update %s small keys to 1 from g1", dungeonNames[dungeonIndex]), gs[1].n[0]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no redundant notifications:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
		})

		t.Run(fmt.Sprintf("smallkeys_ideal_d%02x", dungeonIndex<<1), tc.runGameSyncTest)
	}
}

func TestGameSync_SmallKeys_Delayed(t *testing.T) {
	for dungeonIndex := uint8(0); dungeonIndex <= 1; dungeonIndex++ {
		tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// set both modules to $07, dungeons to $00:
					gs[0].e.WRAM[0x10] = 0x07
					gs[1].e.WRAM[0x10] = 0x07
					gs[0].e.WRAM[0x040C] = dungeonIndex << 1
					gs[1].e.WRAM[0x040C] = dungeonIndex << 1
				},
			},
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// inc g1 current dungeon key counter:
					gs[0].e.WRAM[0xF36F] = 1
					// change g2 submodule to delay receiving sync:
					gs[1].e.WRAM[0x11] = 0x05
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 DID NOT update small keys:
					if expected, actual := uint8(0), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
					}
					if expected, actual := uint8(0), gs[1].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(0), gs[1].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
				},
			},
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// change g2 submodule back to 0 to enable sync:
					gs[1].e.WRAM[0x11] = 0x00
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 updated small keys:
					if expected, actual := uint8(1), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
					}
					if expected, actual := uint8(1), gs[1].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(1), gs[1].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 confirmed last update:
					if len(gs[1].n) != 2 {
						t.Errorf("expected 2 notifications actual %d", len(gs[1].n))
					}
					if expected, actual := "update Sewer Passage small keys to 1 from g1", gs[1].n[0]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
					if expected, actual := "update Hyrule Castle small keys to 1 from g1", gs[1].n[1]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no redundant notifications:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
		})

		t.Run(fmt.Sprintf("smallkeys_delayed_d%02x", dungeonIndex<<1), tc.runGameSyncTest)
	}

	for dungeonIndex := uint8(2); dungeonIndex < 14; dungeonIndex++ {
		tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// set both modules to $07, dungeons to $00:
					gs[0].e.WRAM[0x10] = 0x07
					gs[1].e.WRAM[0x10] = 0x07
					gs[0].e.WRAM[0x040C] = dungeonIndex << 1
					gs[1].e.WRAM[0x040C] = dungeonIndex << 1
				},
			},
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// inc g1 current dungeon key counter:
					gs[0].e.WRAM[0xF36F] = 1
					// change g2 submodule to delay receiving sync:
					gs[1].e.WRAM[0x11] = 0x05
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 DID NOT update small keys:
					if expected, actual := uint8(0), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
					}
					offs := smallKeyFirst + uint16(dungeonIndex)
					if expected, actual := uint8(0), gs[1].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
				},
			},
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// change g2 submodule back to 0 to enable sync:
					gs[1].e.WRAM[0x11] = 0x00
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 updated small keys:
					if expected, actual := uint8(1), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
					}
					offs := smallKeyFirst + uint16(dungeonIndex)
					if expected, actual := uint8(1), gs[1].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g2 confirmed last update:
					if len(gs[1].n) != 1 {
						t.Errorf("expected %d notifications actual %d", 1, len(gs[1].n))
						return
					}
					if expected, actual := fmt.Sprintf("update %s small keys to 1 from g1", dungeonNames[dungeonIndex]), gs[1].n[0]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no redundant notifications:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
		})

		t.Run(fmt.Sprintf("smallkeys_delayed_d%02x", dungeonIndex<<1), tc.runGameSyncTest)
	}
}

func TestGameSync_SmallKeys_DoubleSpend(t *testing.T) {
	tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
		{
			preFrame: func(t testing.TB, gs [2]gameSync) {
				// set both modules to $07, dungeons to $00:
				gs[0].e.WRAM[0x10] = 0x07
				gs[1].e.WRAM[0x10] = 0x07
				gs[0].e.WRAM[0x040C] = 0
				gs[1].e.WRAM[0x040C] = 0
				// start both off with 1 key:
				gs[0].e.WRAM[0xF36F] = 1
				gs[1].e.WRAM[0xF36F] = 1
			},
		},
		{
			preFrame: func(t testing.TB, gs [2]gameSync) {
				// dec g1 current dungeon key counter:
				gs[0].e.WRAM[0xF36F] = 0
				// dec g2 current dungeon key counter:
				gs[1].e.WRAM[0xF36F] = 0
			},
			postFrame: func(t testing.TB, gs [2]gameSync) {
				// verify g2 updated its current small key counter:
				if expected, actual := uint8(0), gs[0].e.WRAM[0xF36F]; expected != actual {
					t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
				}
				// verify g2 updated its current small key counter:
				if expected, actual := uint8(0), gs[1].e.WRAM[0xF36F]; expected != actual {
					t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
				}

				// verify g2 warned of double-spend:
				if len(gs[1].n) != 2 {
					t.Errorf("expected 2 notifications actual %d", len(gs[1].n))
					return
				}
				if expected, actual := "conflict with g1 detected for Sewer Passage small keys", gs[1].n[0]; expected != actual {
					t.Errorf("expected notification %q actual %q", expected, actual)
				}
				if expected, actual := "conflict with g1 detected for Hyrule Castle small keys", gs[1].n[1]; expected != actual {
					t.Errorf("expected notification %q actual %q", expected, actual)
				}

			},
		},
		{
			postFrame: func(t testing.TB, gs [2]gameSync) {
				// verify g2 confirmed last update:
				if len(gs[1].n) != 0 {
					t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					return
				}
			},
		},
		{
			postFrame: func(t testing.TB, gs [2]gameSync) {
				// verify g2 confirmed last update:
				if len(gs[1].n) != 0 {
					t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					return
				}
			},
		},
	})

	tc.runGameSyncTest(t)
}

func TestGameSync_SmallKeys_InitialWithVarying(t *testing.T) {
	for dungeonIndex := uint8(0); dungeonIndex <= 1; dungeonIndex++ {
		tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					// set both modules to $07, dungeons to $00:
					gs[0].e.WRAM[0x10] = 0x07
					gs[1].e.WRAM[0x10] = 0x07
					gs[0].e.WRAM[0x040C] = dungeonIndex << 1
					gs[1].e.WRAM[0x040C] = dungeonIndex << 1
					// set different current dungeon key counter values:
					gs[0].e.WRAM[0xF36F] = 1
					gs[0].e.WRAM[smallKeyFirst] = 1
					gs[0].e.WRAM[smallKeyFirst+1] = 1
					gs[1].e.WRAM[0xF36F] = 2
					gs[1].e.WRAM[smallKeyFirst] = 2
					gs[1].e.WRAM[smallKeyFirst+1] = 2
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g1 no changes:
					if expected, actual := uint8(1), gs[0].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(1), gs[0].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(1), gs[0].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
					// verify g2 no changes:
					if expected, actual := uint8(2), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(2), gs[1].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(2), gs[1].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no notifications from g1:
					if len(gs[0].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[0].n))
					}
					// no notifications from g2:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
					// verify g1 updated to g2's highest key count:
					if expected, actual := uint8(2), gs[0].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(2), gs[0].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(2), gs[0].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
					// verify g2 no changes:
					if expected, actual := uint8(2), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(2), gs[1].e.WRAM[smallKeyFirst]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst, expected, actual)
					}
					if expected, actual := uint8(2), gs[1].e.WRAM[smallKeyFirst+1]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", smallKeyFirst+1, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g1 confirmed its last update with a notification:
					if len(gs[0].n) != 2 {
						t.Errorf("expected %d notifications actual %d", 2, len(gs[0].n))
						return
					}
					if expected, actual := "update Sewer Passage small keys to 2 from g2", gs[0].n[0]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
					if expected, actual := "update Hyrule Castle small keys to 2 from g2", gs[0].n[1]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
					// no notifications from g2:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no notifications from g1:
					if len(gs[0].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[0].n))
					}
					// no notifications from g2:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
		})

		t.Run(fmt.Sprintf("smallkeys_initialwithvarying_d%02x", dungeonIndex<<1), tc.runGameSyncTest)
	}

	for dungeonIndex := uint8(2); dungeonIndex < 14; dungeonIndex++ {
		tc := newGameSyncTestCase("VT test", []gameSyncTestFrame{
			{
				preFrame: func(t testing.TB, gs [2]gameSync) {
					offs := smallKeyFirst + uint16(dungeonIndex)
					// set both modules to $07, dungeons to $00:
					gs[0].e.WRAM[0x10] = 0x07
					gs[1].e.WRAM[0x10] = 0x07
					gs[0].e.WRAM[0x040C] = dungeonIndex << 1
					gs[1].e.WRAM[0x040C] = dungeonIndex << 1
					// set different current dungeon key counter values:
					gs[0].e.WRAM[0xF36F] = 1
					gs[0].e.WRAM[offs] = 1
					gs[1].e.WRAM[0xF36F] = 2
					gs[1].e.WRAM[offs] = 2
				},
				postFrame: func(t testing.TB, gs [2]gameSync) {
					offs := smallKeyFirst + uint16(dungeonIndex)
					// verify g1 no changes:
					if expected, actual := uint8(1), gs[0].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(1), gs[0].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
					// verify g2 no changes:
					if expected, actual := uint8(2), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(2), gs[1].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					offs := smallKeyFirst + uint16(dungeonIndex)
					// no notifications from g1:
					if len(gs[0].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[0].n))
					}
					// no notifications from g2:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
					// verify g1 updated to g2's highest key count:
					if expected, actual := uint8(2), gs[0].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(2), gs[0].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
					// verify g2 no changes:
					if expected, actual := uint8(2), gs[1].e.WRAM[0xF36F]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", 0xF36F, expected, actual)
					}
					if expected, actual := uint8(2), gs[1].e.WRAM[offs]; expected != actual {
						t.Errorf("expected wram[$%04x] == $%02x, got $%02x", offs, expected, actual)
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// verify g1 confirmed its last update with a notification:
					if len(gs[0].n) != 1 {
						t.Errorf("expected %d notifications actual %d", 1, len(gs[0].n))
						return
					}
					if expected, actual := fmt.Sprintf("update %s small keys to 2 from g2", dungeonNames[dungeonIndex]), gs[0].n[0]; expected != actual {
						t.Errorf("expected notification %q actual %q", expected, actual)
					}
					// no notifications from g2:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
			{
				postFrame: func(t testing.TB, gs [2]gameSync) {
					// no notifications from g1:
					if len(gs[0].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[0].n))
					}
					// no notifications from g2:
					if len(gs[1].n) != 0 {
						t.Errorf("expected %d notifications actual %d", 0, len(gs[1].n))
					}
				},
			},
		})

		t.Run(fmt.Sprintf("smallkeys_initialwithvarying_d%02x", dungeonIndex<<1), tc.runGameSyncTest)
	}
}
