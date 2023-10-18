package alttp

import (
	"github.com/alttpo/snes/emulator"
	"log"
	"o2/interfaces"
	"o2/snes"
	"testing"
	"time"
)

type moduleVariant struct {
	module    uint8
	submodule uint8
	allowed   bool
}

var moduleVariants = []moduleVariant{
	{
		module:    0x07,
		submodule: 0x00,
		allowed:   true,
	},
	{
		module:    0x07,
		submodule: 0x02,
		allowed:   false,
	},
	{
		module:    0x09,
		submodule: 0x00,
		allowed:   true,
	},
	{
		module:    0x09,
		submodule: 0x02,
		allowed:   false,
	},
	{
		module:    0x0B,
		submodule: 0x00,
		allowed:   true,
	},
	{
		module:    0x0B,
		submodule: 0x02,
		allowed:   false,
	},
}

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
	preGenLocal        []wramSetValue
	preGenRemote       []wramSetValue
	preGenLocalUpdate  func(local *Player)
	preGenRemoteUpdate func(remote *Player)
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
	offset uint32
	value  uint8
}

type wramTestValue struct {
	// offset is relative to $7E0000, e.g. $F340 for bow
	offset uint32
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
					wantNotifications: []string{
						"picked up Green Potion",
					},
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

func (tt *testCase) runFrameTest(t *testing.T) {
	system, g := tt.system, tt.g

	notifications := make([]string, 0, 10)
	notificationsObserver := interfaces.ObserverImpl(func(object interface{}) {
		notification := object.(string)
		notifications = append(notifications, notification)
		log.Printf("notification[%d]: '%s'\n", len(notifications)-1, notification)
	})

	// subscribe to front-end Notifications from the game:
	observerHandle := g.Notifications.Subscribe(notificationsObserver)
	defer func() {
		g.Notifications.Unsubscribe(observerHandle)
	}()

	g.FirstFrame()

	// reset memory:
	for i := range system.WRAM {
		system.WRAM[i] = 0x00
	}
	// cannot reset system.SRAM here because of the setup code executed in CreateTestEmulator

	// default module/submodule:
	system.WRAM[0x10] = tt.module
	system.WRAM[0x11] = tt.subModule

	// reset local player:
	for _, w := range g.local.WRAM {
		w.Timestamp = 0
		w.IsWriting = false
		w.Value = 0
		w.ValueExpected = 0
	}

	// reset remote player:
	g.players[1].IndexF = 1
	g.players[1].Ttl = 255
	g.players[1].NameF = "remote"
	g.players[1].WRAM = make(WRAMReadable)
	for j := range g.players[1].SRAM.data {
		g.players[1].SRAM.data[j] = 0
		g.players[1].SRAM.fresh[j] = false
	}

	// iterate through frames of test:
	for f := range tt.frames {
		log.Printf("frame %d\n", f)

		frame := &tt.frames[f]

		// clear notifications slice:
		notifications = notifications[:0]

		// set pre-generation local values:
		for j := range frame.preGenLocal {
			set := &frame.preGenLocal[j]

			system.WRAM[set.offset] = set.value
		}

		// set pre-generation remote values:
		for j := range frame.preGenRemote {
			set := &frame.preGenRemote[j]
			if set.offset < 0xF000 {
				continue
			}
			g.players[1].SRAM.data[set.offset-0xF000] = set.value
			g.players[1].SRAM.fresh[set.offset-0xF000] = true
		}

		if u := frame.preGenLocalUpdate; u != nil {
			u(g.local)
		}
		if u := frame.preGenRemoteUpdate; u != nil {
			u(&g.players[1])
		}

		// use enqueueSRAMRead and enqueueWRAMReads and enqueueMainRead to perform the SNES reads:
		q := make([]snes.Read, 0, 20)
		q = g.enqueueSRAMRead(q)
		q = g.enqueueWRAMReads(q)
		q = g.enqueueMainRead(q)
		rsps := make([]snes.Response, 0, len(q))
		for i := range q {
			address := q[i].Address
			offs := address - 0xF50000
			rsps = append(rsps, snes.Response{
				IsWrite: false,
				Address: address,
				Size:    q[i].Size,
				Data:    system.WRAM[offs : offs+uint32(q[i].Size)],
				Extra:   nil,
			})
		}
		// force to write update in SRAM to 0x7D00
		g.nextUpdateA = true
		g.readMainComplete(rsps)

		// check if asm was generated:
		updated := g.updateStage > 0
		if frame.wantAsm != updated {
			t.Errorf("generateUpdateAsm() = %v, want %v", updated, frame.wantAsm)
			return
		}

		// modify local WRAM before ASM execution:
		for j := range frame.preAsmLocal {
			set := &frame.preAsmLocal[j]

			system.WRAM[set.offset] = set.value

			if set.offset >= 0xF000 {
				g.local.SRAM.data[set.offset-0xF000] = set.value
				g.local.SRAM.fresh[set.offset-0xF000] = true
			}
		}

		// execute a frame of ASM:
		// run the CPU until it either runs away or hits the expected stopping point in the ROM code:
		system.CPU.Reset()
		system.SetPC(testROMMainGameLoop)
		if !system.RunUntil(testROMBreakPoint, 0x1_000) {
			t.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x", testROMBreakPoint, system.CPU.PC)
			return
		}

		// verify values:
		for _, check := range frame.postAsmLocal {
			if actual, expected := system.WRAM[check.offset], check.value; actual != expected {
				t.Errorf("system.WRAM[%#04x] = $%02x, expected $%02x", check.offset, actual, expected)
			}
		}

		if updated {
			// invoke asm confirmations to get notifications:
			execCheck := uint16(g.lastUpdateTarget&0xFFFF) + 0x02
			log.Printf("alttp: update: states = %v\n", system.SRAM[execCheck:execCheck+uint16(len(g.updateGenerators))])
			for i, generator := range g.updateGenerators {
				generator.ConfirmAsmExecuted(uint32(i), system.SRAM[execCheck+uint16(i)])
			}
		}

		// ignore notifications if nil
		if frame.wantNotifications != nil {
			if len(notifications) != len(frame.wantNotifications) {
				t.Errorf("notifications = %#v, expected %#v", notifications, frame.wantNotifications)
			} else if len(notifications) > 0 {
				for i := range notifications {
					if notifications[i] != frame.wantNotifications[i] {
						t.Errorf("notification[%d] = '%s', expected '%s'", i, notifications[i], frame.wantNotifications[i])
					}
				}
			}
		}

		// advance server time by 1 frame (ideal):
		g.lastServerRecvTime = g.lastServerRecvTime.Add(17 * time.Millisecond)
		g.lastServerTime = g.lastServerTime.Add(17 * time.Millisecond)
	}
}
