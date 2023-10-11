package alttp

import (
	"testing"
)

func TestGameSync_SmallKeysIdeal(t *testing.T) {
	tc, err := newGameSyncTestCase([]gameSyncTestFrame{
		{
			preFrame: func(t *testing.T, gs [2]gameSync) {
				// set both modules to $07, dungeons to $00:
				gs[0].e.WRAM[0x10] = 0x07
				gs[1].e.WRAM[0x10] = 0x07
				gs[0].e.WRAM[0x040C] = 0
				gs[1].e.WRAM[0x040C] = 0
			},
		},
		{
			preFrame: func(t *testing.T, gs [2]gameSync) {
				// inc current dungeon key counter:
				gs[0].e.WRAM[0xF36F] = 1
			},
			postFrame: func(t *testing.T, gs [2]gameSync) {
				// verify g2 updated its current small key counter:
				if expected, actual := uint8(1), gs[1].e.WRAM[0xF36F]; expected != actual {
					t.Errorf("expected wram[$f63f] == $%02x, got $%02x", expected, actual)
				}
			},
		},
		{
			postFrame: func(t *testing.T, gs [2]gameSync) {
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
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("smallkeysideal", tc.runGameSyncTest)
}
