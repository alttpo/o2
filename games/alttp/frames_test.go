package alttp

import (
	"o2/interfaces"
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
						{0xF359, 0}, // no bottle
					},
					preGenRemote: []wramSetValue{
						{0xF359, 2}, // empty bottle
					},
					wantAsm: false,
				},
				{
					preGenLocal: []wramSetValue{
						{0x02E4, 1}, // freeze Link
						{0xF359, 7}, // green bottle
					},
					preGenRemote: []wramSetValue{
						{0xF359, 2}, // empty bottle
					},
					wantAsm: false,
					//preAsmLocal: []wramSetValue{
					//	{0xF359, 0}, // no bottle
					//},
					//postAsmLocal: []wramTestValue{
					//	{0xF359, 0}, // no bottle
					//},
				},
				{
					preGenLocal: []wramSetValue{
						{0x02E4, 0}, // unfreeze Link
					},
					preGenRemote: []wramSetValue{
						{0xF359, 2}, // empty bottle
					},
					wantAsm: false,
					//preAsmLocal: []wramSetValue{
					//	{0xF359, 0}, // no bottle
					//},
					//postAsmLocal: []wramTestValue{
					//	{0xF359, 0}, // no bottle
					//},
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
		frame := &tt.frames[f]

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

		// generate ASM code:
		var code [0x200]byte
		a := asm.NewEmitter(code[:], true)
		updated := g.generateSRAMRoutine(a, 0x707C00)
		if updated != frame.wantAsm {
			t.Errorf("generateUpdateAsm() = %v, want %v", updated, frame.wantAsm)
			return
		}

		// only run the ASM if it is generated:
		if !updated {
			continue
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

		// deploy the SRAM routine:
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
		for _, check := range frame.postAsmLocal {
			if actual, expected := system.WRAM[check.offset], check.value; actual != expected {
				t.Errorf("system.WRAM[%#04x] = $%02x, expected $%02x", check.offset, actual, expected)
			}
		}

		//if lastNotification != tt.wantNotification {
		//	t.Errorf("notification = '%s', expected '%s'", lastNotification, tt.wantNotification)
		//}
	}
}
