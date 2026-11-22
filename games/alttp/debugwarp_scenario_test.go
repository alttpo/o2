package alttp

import (
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
)

// setDebugExit writes exit i of the underworld exit table into the test ROM.
func setDebugExit(t *testing.T, gs *gameSync, i uint8, room uint16, area uint8) {
	t.Helper()
	base := gs.g.romExitRooms()
	put := func(addr uint32, v uint8) {
		pak, err := lorom.BusAddressToPak(addr)
		if err != nil || int(pak) >= len(gs.g.rom.Contents) {
			t.Fatalf("$%06x is not in the ROM", addr)
		}
		gs.g.rom.Contents[pak] = v
	}
	put(base+uint32(i)*2, uint8(room))
	put(base+uint32(i)*2+1, uint8(room>>8))
	put(base+exitTableCount*2+uint32(i), area)
}

func TestDebug_WarpToEntranceFromDungeon(t *testing.T) {
	// in Eastern Palace with 2 keys:
	gs := newDebugScenario(t, 0x04)
	e := gs.e
	e.WRAM[0x1B] = 1
	e.WRAM[0xF36F] = 2
	e.WRAM[0x5D] = 0x04
	// Palace of Darkness, exit $27 in the dark world:
	setDebugExit(t, &gs, 0x27, 0x004A, 0x5E)

	debugRun(t, &gs, debugOp{T: debugOpWarp, W: "entrance", V: 0x27, E: 0x26})
	if m, c := e.WRAM[0x10], e.WRAM[0x010C]; m != 0x0F || c != 0x06 {
		t.Errorf("expected module $0F then $06; got $%02x then $%02x", m, c)
	}
	if v := e.WRAM[0x010E]; v != 0x26 {
		t.Errorf("entrance: expected $26; got $%02x", v)
	}
	if v := e.WRAM[0x010A]; v != 1 {
		t.Errorf("expected the overworld exit to be kept from the underworld; got $010A = %d", v)
	}
	if v := e.WRAM[0xF3CA]; v != 0x40 {
		t.Errorf("world: expected $40; got $%02x", v)
	}
	if v := e.WRAM[0xF37E]; v != 2 {
		t.Errorf("Eastern Palace keys: expected 2 saved; got %d", v)
	}
	if v := e.WRAM[0x5D]; v != 0 {
		t.Errorf("Link's state: expected 0; got $%02x", v)
	}
}

func TestDebug_WarpToExitFromOverworld(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e
	e.WRAM[0xF3CA] = 0x40
	// Eastern Palace, exit $09 in the light world:
	setDebugExit(t, &gs, 0x09, 0x00C9, 0x1E)

	debugRun(t, &gs, debugOp{T: debugOpWarp, W: "exit", V: 0x09})
	if m, c := e.WRAM[0x10], e.WRAM[0x010C]; m != 0x0F || c != 0x08 {
		t.Errorf("expected module $0F then $08; got $%02x then $%02x", m, c)
	}
	if r := uint16(e.WRAM[0xA0]) | uint16(e.WRAM[0xA1])<<8; r != 0x00C9 {
		t.Errorf("room: expected $0C9; got $%03x", r)
	}
	if v := e.WRAM[0xF3CA]; v != 0 {
		t.Errorf("world: expected 0; got $%02x", v)
	}
	if v := e.WRAM[0x010A]; v != 0 {
		t.Errorf("expected $010A untouched in the overworld; got %d", v)
	}
}

func TestDebug_WarpWaitsAndRejects(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e
	// a house's exit comes from where Link entered it, not from the table:
	setDebugExit(t, &gs, 0x10, 0x0123, 0x28)
	debugRun(t, &gs, debugOp{T: debugOpWarp, W: "exit", V: 0x10})
	if m := e.WRAM[0x10]; m != 0x09 {
		t.Errorf("expected no warp to a house's exit; got module $%02x", m)
	}

	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{
		{T: debugOpWarp, W: "nowhere"},
		{T: debugOpWarp, W: "exit", V: exitTableCount},
		{T: debugOpWarp, W: "entrance", V: 1, E: 0},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected bad warps to be rejected; %d queued", n)
	}

	// in a transition, the warp waits for Link to stand:
	setDebugExit(t, &gs, 0x09, 0x00C9, 0x1E)
	e.WRAM[0x11] = 0x23
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{{T: debugOpWarp, W: "exit", V: 0x09}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		gs.runFrame(t)
	}
	if n := len(gs.g.debug.queue); n != 1 || e.WRAM[0x10] != 0x09 {
		t.Errorf("expected the warp to wait; %d queued, module $%02x", n, e.WRAM[0x10])
	}
	e.WRAM[0x11] = 0
	debugRun(t, &gs)
	if m := e.WRAM[0x10]; m != 0x0F {
		t.Errorf("expected the warp after the transition; got module $%02x", m)
	}
}
