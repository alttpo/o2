package alttp

import (
	"o2/interfaces"
	"o2/snes/asm"
	"testing"
)

func TestSyncableMaxU8_Frames(t *testing.T) {
	system, rom, err := CreateTestEmulator(t, "ZELDANODENSETSU")
	if err != nil {
		t.Fatal(err)
		return
	}

	g := CreateTestGame(rom, system)

	type frameTest struct {
		offset   uint16
		t0Local  uint8
		t0Remote uint8
	}

	t.Run("Compare", func(t *testing.T) {
		system.Logger = &testingLogger{t: t}

		lastNotification := ""
		notificationsObserver := interfaces.ObserverImpl(func(object interface{}) {
			lastNotification = object.(string)
			t.Logf("notify: '%s'", lastNotification)
		})

		// subscribe to front-end Notifications from the game:
		observerHandle := g.Notifications.Subscribe(notificationsObserver)
		defer func() {
			g.Notifications.Unsubscribe(observerHandle)
		}()

		// reset memory:
		for i := range system.WRAM {
			system.WRAM[i] = 0x00
			g.wram[i] = 0x00
		}
		// cannot reset system.SRAM here because of the setup code executed in CreateTestEmulator

		// default module/submodule:
		system.WRAM[0x10] = module    // overworld module
		g.wram[0x10] = module         // overworld module
		system.WRAM[0x11] = subModule // player in control
		g.wram[0x11] = subModule      // player in control

		// set up SRAM per each player:
		g.players[1].IndexF = 1
		g.players[1].Ttl = 255
		g.players[1].NameF = "remote"
		for j := range g.players[1].SRAM {
			g.players[1].SRAM[j] = 0
		}
		for _, sram := range tt.sram {
			system.WRAM[0xF000+sram.offset] = sram.localValue
			g.wram[0xF000+sram.offset] = sram.localValue

			g.local.SRAM[sram.offset] = sram.localValue
			g.players[1].SRAM[sram.offset] = sram.remoteValue
		}

		var code [0x200]byte
		a := asm.NewEmitter(code[:], true)
		updated := g.generateSRAMRoutine(a, 0x707C00)
		if updated != tt.wantUpdated {
			t.Errorf("generateUpdateAsm() = %v, want %v", updated, tt.wantUpdated)
			return
		}

		// only run the ASM if it is generated:
		if !updated {
			return
		}

		copy(system.SRAM[0x7C00:0x7D00], a.Bytes())

		// run the CPU until it either runs away or hits the expected stopping point in the ROM code:
		system.CPU.Reset()
		system.SetPC(0x00_8056)
		if !system.RunUntil(testROMBreakPoint, 0x1_000) {
			t.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", testROMBreakPoint, system.CPU.PC)
			return
		}

		// copy SRAM shadow in WRAM into local player copy:
		copy(g.local.SRAM[:], system.WRAM[0xF000:0x1_0000])

		// verify SRAM values:
		for _, sram := range tt.sram {
			if actual, expected := system.WRAM[0xF000+sram.offset], sram.expectedValue; actual != expected {
				t.Errorf("local.SRAM[%#04x] = $%02x, expected $%02x", sram.offset, actual, expected)
			}
		}

		// call generateUpdateAsm() again for next frame to receive notifications:
		a = asm.NewEmitter(make([]byte, 0x200), false)
		a.SetBase(0x707E00)
		a.AssumeSEP(0x30)
		_ = g.generateUpdateAsm(a)

		if lastNotification != tt.wantNotification {
			t.Errorf("notification = '%s', expected '%s'", lastNotification, tt.wantNotification)
		}
	})
}
