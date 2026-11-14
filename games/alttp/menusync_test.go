package alttp

import (
	"fmt"
	"io"
	"testing"
	"time"
)

// TestNoSyncInMenuModules verifies that no game state is synchronized in either direction while a player is in
// modules $00-$05 (startup screens, file select, copy/erase/name player, loading game).
func TestNoSyncInMenuModules(t *testing.T) {
	for module := uint8(0x00); module <= 0x05; module++ {
		module := module
		t.Run(fmt.Sprintf("module_%02x", module), func(t *testing.T) {
			testMenuModuleSync(t, module, false)
		})
	}

	// control: verify the test setup does sync when both players are in game:
	t.Run("control_module_07", func(t *testing.T) {
		testMenuModuleSync(t, 0x07, true)
	})
}

func testMenuModuleSync(t *testing.T, menuModule uint8, expectSync bool) {
	var gs [2]gameSync
	var err error
	for i, name := range []string{"ingame", "menu"} {
		gs[i], err = createTestGameSync("VT test", name, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := &testServer{Clients: []*testClient{gs[0].c, gs[1].c}, Now: time.Now()}
	s.HandleAllClients(nil)
	for i := range gs {
		gameHandleNet(gs[i].g)
	}

	ingame, menu := gs[0], gs[1]

	// in-game player has a sword and has opened a chest in a dungeon room:
	ingame.e.WRAM[0x10] = 0x07
	ingame.e.WRAM[0x11] = 0x00
	ingame.e.WRAM[0xF359] = 0x02 // sword
	ingame.e.WRAM[0xF000] = 0x10 // underworld room $00 chest bit

	// menu player still has stale data in its WRAM SRAM copy (e.g. from a previous game before save & quit):
	menu.e.WRAM[0x10] = menuModule
	menu.e.WRAM[0x11] = 0x00
	menu.e.WRAM[0xF35A] = 0x02 // shield
	menu.e.WRAM[0xF280] = 0x20 // overworld area $00 overlay bit
	menu.e.WRAM[0xF002] = 0x10 // underworld room $01 chest bit

	menuIndex := menu.g.local.Index()

	// run enough frames to cover every periodic send (every 16th and 32nd frame):
	for f := 0; f < 80; f++ {
		for i := range gs {
			gs[i].e.WRAM[0x1A] = uint8(f)
			gs[i].runFrame(t)
			s.HandleAllClients(nil)
		}
		s.AdvanceTime(time.Millisecond * 16)
	}
	gameHandleNet(ingame.g)
	gameHandleNet(menu.g)

	rp := &ingame.g.players[menuIndex]
	if expectSync {
		if rp.SRAM.data[0x35A] != 0x02 || ingame.e.WRAM[0xF35A] != 0x02 || menu.e.WRAM[0xF359] != 0x02 {
			t.Errorf("expected items to sync between in-game players")
		}
		return
	}

	// outbound: the in-game player must not have received any state from the menu player:
	if rp.SRAM.data[0x35A] != 0 || rp.SRAM.data[0x280] != 0 || rp.SRAM.data[0x002] != 0 {
		t.Errorf("in-game player received SRAM from player in module $%02x", menuModule)
	}
	if ingame.e.WRAM[0xF35A] != 0 || ingame.e.WRAM[0xF280] != 0 || ingame.e.WRAM[0xF002] != 0 {
		t.Errorf("in-game player's SNES was updated with state from player in module $%02x", menuModule)
	}

	// inbound: the menu player's SNES must not be written to:
	if menu.e.WRAM[0xF359] != 0 || menu.e.WRAM[0xF000] != 0 {
		t.Errorf("SNES of player in module $%02x was updated with remote state", menuModule)
	}
}
