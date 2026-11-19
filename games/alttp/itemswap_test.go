package alttp

import (
	"fmt"
	"testing"
)

// randomizer ROM titles whose Y button swaps items in the bow, boomerang, mushroom and flute slots:
var itemSwapRandomizerTitles = []string{
	"VT ry08z8Q5y5",
	"ER101_1_1_288795599",
	"DR101_1_1_288795599",
	"AP250_1_1_16070690178",
	"BM250_1_1_16070690178",
	"BD251_1_1_23654700304",
}

func TestIsVTRandomizerTitle(t *testing.T) {
	for _, title := range itemSwapRandomizerTitles {
		if !isVTRandomizerTitle([]byte(title)) {
			t.Errorf("%q: not recognized as a randomizer", title)
		}
	}
	for _, title := range []string{
		"THE LEGEND OF ZELDA  ",
		"ZELDANODENSETSU      ",
		"LOZ: PARALLEL WORLDS ",
		"ZSM10000000000000000 ",
		"ERROR",
		"",
	} {
		if isVTRandomizerTitle([]byte(title)) {
			t.Errorf("%q: recognized as a randomizer", title)
		}
	}
}

// TestRandomizerItemSwapNotUndone: the local player's choice of item in a slot that the Y button swaps must stay
// when remote players chose a different (higher) item there, as long as the items owned ($7EF38C, $7EF38E) agree.
func TestRandomizerItemSwapNotUndone(t *testing.T) {
	setupTestLogger(t)

	type slot struct {
		offset        uint32
		local, remote uint8
	}
	swaps := []struct {
		name  string
		slots []slot
	}{
		{"shovel over flute", []slot{
			{0xF38C, IS1Shovel | IS1FluteActive, IS1Shovel | IS1FluteActive},
			{0xF34C, 1, 3},
		}},
		{"mushroom over powder", []slot{
			{0xF38C, IS1Mushroom | IS1MagicPowder, IS1Mushroom | IS1MagicPowder},
			{0xF344, 1, 2},
		}},
		{"blue over red boomerang", []slot{
			{0xF38C, IS1BlueBoomerang | IS1RedBoomerang, IS1BlueBoomerang | IS1RedBoomerang},
			{0xF341, 1, 2},
		}},
		{"bow over silver bow", []slot{
			{0xF38E, IS2WoodBow | IS2SilverBow, IS2WoodBow | IS2SilverBow},
			{0xF377, 10, 10},
			{0xF340, 2, 4},
		}},
	}

	for _, title := range itemSwapRandomizerTitles {
		system, rom, err := CreateTestEmulator(title, t)
		if err != nil {
			t.Fatal(err)
		}
		g := CreateTestGame(rom, system)

		for _, sw := range swaps {
			fr := frame{wantAsm: false}
			for _, s := range sw.slots {
				fr.preGenLocal = append(fr.preGenLocal, wramSetValue{s.offset, s.local})
				fr.preGenRemote = append(fr.preGenRemote, wramSetValue{s.offset, s.remote})
				fr.postAsmLocal = append(fr.postAsmLocal, wramTestValue{s.offset, s.local})
			}
			tt := &testCase{
				system:    system,
				g:         g,
				name:      fmt.Sprintf("%s %s", title, sw.name),
				module:    0x07,
				subModule: 0x00,
				frames:    []frame{fr},
			}
			t.Run(tt.name, tt.runFrameTest)
		}

		// items still sync: a remote player's new flute shows up in the local player's empty flute slot:
		tt := &testCase{
			system:    system,
			g:         g,
			name:      fmt.Sprintf("%s flute received", title),
			module:    0x07,
			subModule: 0x00,
			frames: []frame{{
				preGenLocal:       []wramSetValue{{0xF38C, 0}, {0xF34C, 0}},
				preGenRemote:      []wramSetValue{{0xF38C, IS1FluteActive}, {0xF34C, 3}},
				wantAsm:           true,
				postAsmLocal:      []wramTestValue{{0xF38C, IS1FluteActive}, {0xF34C, 3}},
				wantNotifications: []string{"got Flute (active) from remote"},
			}},
		}
		t.Run(tt.name, tt.runFrameTest)
	}
}
