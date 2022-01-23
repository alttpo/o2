package alttp

import (
	"o2/interfaces"
	"o2/snes"
	"o2/snes/asm"
	"o2/snes/emulator"
	"testing"
)

type testCase struct {
	system *emulator.System
	g      *Game

	name string

	module    uint8
	subModule uint8

	frames []frame
}

type frame struct {
	// values set before ASM generation:
	preGenLocal  []wramSetValue
	preGenRemote []wramSetValue
	// do we want ASM generated?
	wantAsm bool
	// values set after ASM generation but before ASM execution:
	preAsmLocal []wramSetValue
	// values to test after ASM execution:
	postAsmLocal []wramTestValue

	wantNotifications []string
}

type wramSetValue struct {
	// offset is relative to $7E0000, e.g. $F340 for bow
	offset uint16
	value  uint8
}

type wramTestValue struct {
	// offset is relative to $7E0000, e.g. $F340 for bow
	offset uint16
	value  uint8
}

func Test_Frames(t *testing.T) {
	tests := []testCase{
		{
			name:      "wishing well bottle fill",
			module:    0x07,
			subModule: 0x00,
			frames: []frame{
				// while Link is frozen (e.g. during wishing well), do not sync in remote values:
				{
					preGenLocal: []wramSetValue{
						{0x02E4, 1}, // freeze Link
						{0xF35C, 0}, // no bottle
					},
					preGenRemote: []wramSetValue{
						{0xF35C, 2}, // empty bottle
					},
					wantAsm: false,
				},
				{
					preGenLocal: []wramSetValue{
						{0x02E4, 1}, // freeze Link
						{0xF35C, 4}, // green potion
					},
					preGenRemote: []wramSetValue{
						{0xF35C, 2}, // empty bottle
					},
					wantAsm: false,
				},
				{
					preGenLocal: []wramSetValue{
						{0x02E4, 0}, // unfreeze Link
					},
					preGenRemote: []wramSetValue{
						{0xF35C, 2}, // empty bottle
					},
					wantAsm: false,
					postAsmLocal: []wramTestValue{
						{0xF35C, 4}, // green potion
					},
				},
			},
		},
		{
			name:      "boots",
			module:    0x09,
			subModule: 0x00,
			frames: []frame{
				{
					// 0xF355
					preGenLocal: []wramSetValue{
						{0xF355, 0},          // no boots
						{0xF379, 0b11111000}, // no dash
					},
					preGenRemote: []wramSetValue{
						{0xF355, 1},          // boots
						{0xF379, 0b11111100}, // dash
					},
					wantAsm: true,
					postAsmLocal: []wramTestValue{
						{0xF355, 1},          // boots
						{0xF379, 0b11111100}, // dash
					},
					wantNotifications: []string{
						"got Pegasus Boots from remote",
						"got Dash Ability from remote",
					},
				},
			},
		},
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

func (tt *testCase) runFrameTest(t *testing.T) {
	system, g := tt.system, tt.g

	system.Logger = &testingLogger{t: t}

	notifications := make([]string, 0, 10)
	notificationsObserver := interfaces.ObserverImpl(func(object interface{}) {
		notification := object.(string)
		notifications = append(notifications, notification)
		t.Logf("notification[%d]: '%s'", len(notifications)-1, notification)
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
	system.WRAM[0x10] = tt.module
	g.wram[0x10] = tt.module
	system.WRAM[0x11] = tt.subModule
	g.wram[0x11] = tt.subModule

	// reset remote player:
	g.players[1].IndexF = 1
	g.players[1].Ttl = 255
	g.players[1].NameF = "remote"
	for j := range g.players[1].SRAM {
		g.players[1].SRAM[j] = 0
	}

	// iterate through frames of test:
	for f := range tt.frames {
		t.Logf("frame %d", f)

		frame := &tt.frames[f]

		// clear notifications slice:
		notifications = notifications[:0]

		// set pre-generation local values:
		for j := range frame.preGenLocal {
			set := &frame.preGenLocal[j]

			system.WRAM[set.offset] = set.value
			g.wram[set.offset] = set.value

			if set.offset >= 0xF000 {
				g.local.SRAM[set.offset-0xF000] = set.value
			}
		}

		// set pre-generation remote values:
		for j := range frame.preGenRemote {
			set := &frame.preGenRemote[j]
			if set.offset < 0xF000 {
				continue
			}
			g.players[1].SRAM[set.offset-0xF000] = set.value
		}

		// since Queue is nil, this method will not call generateSRAMRoutine:
		g.readMainComplete([]snes.Response{
			// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
			{Address: 0xF50010, Size: 0xF0, Data: system.WRAM[0x10 : 0x10+0xF0]},
			{Address: 0xF50100, Size: 0x36, Data: system.WRAM[0x0100 : 0x0100+0x36]}, // [$0100..$0135]
			{Address: 0xF502E0, Size: 0x08, Data: system.WRAM[0x02E0 : 0x02E0+0x08]}, // [$02E0..$02E7]
			{Address: 0xF50400, Size: 0x20, Data: system.WRAM[0x0400 : 0x0400+0x20]}, // [$0400..$041F]
			// $1980..19E9 for reading underworld door state
			{Address: 0xF51980, Size: 0x6A, Data: system.WRAM[0x1980 : 0x1980+0x6A]}, // [$1980..$19E9]
			// ALTTP's SRAM copy in WRAM:
			{Address: 0xF5F340, Size: 0xFF, Data: system.WRAM[0xF340 : 0xF340+0xFF]}, // [$F340..$F43E]
			// Link's palette:
			{Address: 0xF5C6E0, Size: 0x20, Data: system.WRAM[0xC6E0 : 0xC6E0+0x20]},
		})

		// generate ASM code:
		var code [0x200]byte
		a := asm.NewEmitter(code[:], true)
		updated := g.generateSRAMRoutine(a, 0x707C00)
		if updated != frame.wantAsm {
			t.Errorf("generateUpdateAsm() = %v, want %v", updated, frame.wantAsm)
			return
		}

		// modify local WRAM before ASM execution:
		for j := range frame.preAsmLocal {
			set := &frame.preAsmLocal[j]

			system.WRAM[set.offset] = set.value
			g.wram[set.offset] = set.value

			if set.offset >= 0xF000 {
				g.local.SRAM[set.offset-0xF000] = set.value
			}
		}

		if updated {
			// deploy the SRAM routine if generated:
			copy(system.SRAM[0x7C00:0x7D00], a.Bytes())
		}

		// execute a frame of ASM:
		// run the CPU until it either runs away or hits the expected stopping point in the ROM code:
		system.CPU.Reset()
		system.SetPC(testROMMainGameLoop)
		if !system.RunUntil(testROMBreakPoint, 0x1_000) {
			t.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", testROMBreakPoint, system.CPU.PC)
			return
		}

		// copy SRAM shadow in WRAM into local player copy:
		copy(g.local.SRAM[:], system.WRAM[0xF000:0x1_0000])
		copy(g.wram[:], system.WRAM[:])

		// verify values:
		for _, check := range frame.postAsmLocal {
			if actual, expected := system.WRAM[check.offset], check.value; actual != expected {
				t.Errorf("system.WRAM[%#04x] = $%02x, expected $%02x", check.offset, actual, expected)
			}
		}

		if updated {
			// invoke asm confirmations to get notifications:
			for i, generator := range g.updateGenerators {
				generator.ConfirmAsmExecuted(uint32(i), system.SRAM[0x7C00+0x02+i])
			}
		}

		if len(notifications) != len(frame.wantNotifications) {
			t.Errorf("notifications = %#v, expected %#v", notifications, frame.wantNotifications)
		}
		if len(notifications) > 0 {
			for i := range notifications {
				if notifications[i] != frame.wantNotifications[i] {
					t.Errorf("notification[%d] = '%s', expected '%s'", i, notifications[i], frame.wantNotifications[i])
				}
			}
		}
	}
}
