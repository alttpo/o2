package alttp

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"o2/snes"
	"os"
	"strings"
	"testing"
	"time"
)

// newDebugScenario makes a player in the overworld (or in Eastern Palace) with the debug page's operations to run.
func newDebugScenario(t *testing.T, dungeon uint8) (gs gameSync) {
	t.Helper()
	gs, err := createTestGameSync("VT test", "g1", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	e := gs.e
	e.WRAM[0x10] = 0x09
	e.WRAM[0x11] = 0x00
	e.WRAM[0x040C] = 0xFF
	e.WRAM[0xF36F] = 0xFF
	if dungeon != 0xFF {
		e.WRAM[0x10] = 0x07
		e.WRAM[0x040C] = dungeon
		e.WRAM[0xF36F] = 1
	}
	return
}

func debugRun(t *testing.T, gs *gameSync, ops ...debugOp) {
	t.Helper()
	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	gs.g.debug.lock.Lock()
	left := len(gs.g.debug.queue)
	gs.g.debug.lock.Unlock()
	if left != 0 {
		t.Fatalf("%d debug operations were not run", left)
	}
}

func TestDebug_SetAndBits(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e
	e.WRAM[0xF379] = 0x68
	e.WRAM[0xF374] = 0x02

	debugRun(t, &gs,
		debugOp{T: debugOpSet, A: 0xF345, V: 1},
		debugOp{T: debugOpOr, A: 0xF379, M: 0x04},
		debugOp{T: debugOpAnd, A: 0xF374, M: 0xFD},
		debugOp{T: debugOpOr, A: 0xF374, M: 0x05},
		debugOp{T: debugOpGfx, W: "sword"},
	)
	if v := e.WRAM[0xF345]; v != 1 {
		t.Errorf("fire rod: expected 1; got %d", v)
	}
	if v := e.WRAM[0xF379]; v != 0x6C {
		t.Errorf("abilities: expected $6C; got $%02x", v)
	}
	if v := e.WRAM[0xF374]; v != 0x05 {
		t.Errorf("pendants: expected $05; got $%02x", v)
	}
}

func TestDebug_Rejects(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{
		{T: debugOpSet, A: 0x0010, V: 0},
		{T: debugOpSet, A: 0xF400, V: 0},
		{T: "bogus"},
		{T: debugOpKeys, D: 2},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected no operation to be queued; got %d", n)
	}
}

func TestDebug_SmallKeys(t *testing.T) {
	gs := newDebugScenario(t, 0x04)
	e := gs.e

	debugRun(t, &gs, debugOp{T: debugOpKeys, D: 1}, debugOp{T: debugOpKeys, D: 1})
	if v := e.WRAM[0xF36F]; v != 3 {
		t.Errorf("expected 3 keys; got %d", v)
	}
	debugRun(t, &gs, debugOp{T: debugOpKeys, D: -1})
	if v := e.WRAM[0xF36F]; v != 2 {
		t.Errorf("expected 2 keys; got %d", v)
	}
	e.WRAM[0xF36F] = 0
	debugRun(t, &gs, debugOp{T: debugOpKeys, D: -1})
	if v := e.WRAM[0xF36F]; v != 0 {
		t.Errorf("expected to stay at 0 keys; got %d", v)
	}

	// not in a dungeon: nothing happens
	e.WRAM[0x040C] = 0xFF
	e.WRAM[0xF36F] = 0xFF
	e.WRAM[0xF37E] = 5
	debugRun(t, &gs, debugOp{T: debugOpKeys, D: 1})
	if v, s := e.WRAM[0xF36F], e.WRAM[0xF37E]; v != 0xFF || s != 5 {
		t.Errorf("expected no change outside a dungeon; got live %d, slot %d", v, s)
	}
}

func TestDebug_Hearts(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e
	e.WRAM[0xF36C], e.WRAM[0xF36D], e.WRAM[0xF36B] = 0x18, 0x18, 0

	expect := func(when string, max, cur, pieces uint8) {
		t.Helper()
		if e.WRAM[0xF36C] != max || e.WRAM[0xF36D] != cur || e.WRAM[0xF36B] != pieces {
			t.Errorf("%s: expected max $%02x, current $%02x, pieces %d; got $%02x, $%02x, %d",
				when, max, cur, pieces, e.WRAM[0xF36C], e.WRAM[0xF36D], e.WRAM[0xF36B])
		}
	}

	debugRun(t, &gs, debugOp{T: debugOpHearts, C: 1})
	expect("container +1", 0x20, 0x18, 0)
	for i := 0; i < 3; i++ {
		debugRun(t, &gs, debugOp{T: debugOpHearts, P: 1})
	}
	expect("pieces +3", 0x20, 0x18, 3)
	debugRun(t, &gs, debugOp{T: debugOpHearts, P: 1})
	expect("piece +1 makes a container", 0x28, 0x18, 0)
	debugRun(t, &gs, debugOp{T: debugOpHearts, P: -1})
	expect("piece -1 takes a container", 0x20, 0x18, 3)
	debugRun(t, &gs, debugOp{T: debugOpFill, W: "hearts"})
	expect("fill", 0x20, 0x20, 3)
	debugRun(t, &gs, debugOp{T: debugOpHearts, C: -1})
	expect("container -1 keeps the health in the capacity", 0x18, 0x18, 3)

	e.WRAM[0xF36C], e.WRAM[0xF36D] = 0x08, 0x08
	debugRun(t, &gs, debugOp{T: debugOpHearts, C: -1})
	expect("keeps one heart", 0x08, 0x08, 3)
	e.WRAM[0xF36C], e.WRAM[0xF36B] = debugMaxHealth, 0
	debugRun(t, &gs, debugOp{T: debugOpHearts, C: 1})
	expect("keeps 20 hearts", debugMaxHealth, 0x08, 0)
}

func TestDebug_Fill(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e
	e.WRAM[0xF370], e.WRAM[0xF371] = 2, 3

	debugRun(t, &gs,
		debugOp{T: debugOpFill, W: "magic"},
		debugOp{T: debugOpFill, W: "rupees"},
		debugOp{T: debugOpFill, W: "arrows"},
		debugOp{T: debugOpFill, W: "bombs"},
	)
	if v := e.WRAM[0xF36E]; v != 0x80 {
		t.Errorf("magic: expected $80; got $%02x", v)
	}
	if v := uint16(e.WRAM[0xF360]) | uint16(e.WRAM[0xF361])<<8; v != 999 {
		t.Errorf("rupees: expected 999; got %d", v)
	}
	if v := e.WRAM[0xF377]; v != 45 {
		t.Errorf("arrows: expected 45; got %d", v)
	}
	if v := e.WRAM[0xF343]; v != 20 {
		t.Errorf("bombs: expected 20; got %d", v)
	}
}

func TestDebug_KillEnemies(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e

	slot := func(k int, state, typ, health uint8) {
		e.WRAM[0x0DD0+k], e.WRAM[0x0E20+k], e.WRAM[0x0E50+k] = state, typ, health
		e.WRAM[0x0DF0+k] = 0
	}
	slot(0, 0x09, 0x01, 4)  // an enemy
	slot(1, 0x0B, 0x08, 2)  // a stunned enemy
	slot(2, 0x09, 0xD9, 0)  // an item: a green rupee
	slot(3, 0x09, 0x1D, 0)  // no health: left alone
	slot(4, 0x00, 0x01, 4)  // not in use
	slot(5, 0x06, 0x01, 4)  // already dying
	slot(15, 0x09, 0x41, 1) // the last slot

	debugRun(t, &gs, debugOp{T: debugOpKill})

	for _, c := range []struct {
		k          int
		state, tmr uint8
	}{{0, 0x06, 0x1F}, {1, 0x06, 0x1F}, {2, 0x09, 0}, {3, 0x09, 0}, {4, 0x00, 0}, {5, 0x06, 0}, {15, 0x06, 0x1F}} {
		if e.WRAM[0x0DD0+c.k] != c.state || e.WRAM[0x0DF0+c.k] != c.tmr {
			t.Errorf("slot %d: expected state $%02x, timer $%02x; got $%02x, $%02x",
				c.k, c.state, c.tmr, e.WRAM[0x0DD0+c.k], e.WRAM[0x0DF0+c.k])
		}
	}
}

func TestDebug_NotifiesState(t *testing.T) {
	gs := newDebugScenario(t, 0x04)
	g := gs.g
	vms := &debugTestViews{}
	g.viewModels = vms
	g.notFirstFrame = true

	g.notifyDebugState()
	g.notifyDebugState()
	if n := len(vms.updates["game/debug"]); n != 1 {
		t.Fatalf("expected one update of an unchanged state; got %d", n)
	}
	gs.e.WRAM[0xF345] = 1
	gs.runFrame(t)
	g.notifyDebugState()
	updates := vms.updates["game/debug"]
	if len(updates) != 2 {
		t.Fatalf("expected a second update after a change; got %d", len(updates))
	}
	s := updates[1].(*debugState)
	if b, err := json.Marshal(s); err != nil || !strings.Contains(string(b), `"sram":[`) {
		t.Errorf("expected the SRAM bytes as a JSON array: %v %.60s", err, b)
	}
	if s.SRAM[0xF345-debugSRAMFirst] != 1 || s.Dungeon != 0x04 {
		t.Errorf("unexpected state: fire rod %d, dungeon $%02x", s.SRAM[0xF345-debugSRAMFirst], s.Dungeon)
	}
}

type debugTestViews struct {
	updates map[string][]interface{}
}

func (v *debugTestViews) NotifyView(view string, viewModel interface{}) {
	if v.updates == nil {
		v.updates = make(map[string][]interface{})
	}
	v.updates[view] = append(v.updates[view], viewModel)
}
func (v *debugTestViews) SetViewModel(view string, viewModel interface{}) {}
func (v *debugTestViews) GetViewModel(view string) (interface{}, bool)    { return nil, false }

// TestDebug_Gfx builds the HUD graphics from real ROMs when they are around:
func TestDebug_Gfx(t *testing.T) {
	roms := []string{"alttp-us.sfc", "alttp-jp.sfc", "alttp-jp-opt.sfc", "LTTPHack-v14.3.5.sfc"}
	found := 0
	for _, name := range roms {
		b, err := os.ReadFile("../../../" + name)
		if err != nil {
			continue
		}
		found++
		t.Run(name, func(t *testing.T) {
			rom, err := snes.NewROM(name, b)
			if err != nil {
				t.Fatal(err)
			}
			g := NewGame(rom)
			gfx, err := g.debugGfx()
			if err != nil {
				t.Fatal(err)
			}
			tiles, err := base64.StdEncoding.DecodeString(gfx.Tiles)
			if err != nil {
				t.Fatal(err)
			}
			if len(tiles) != 3*hudPackSize {
				t.Errorf("expected %d bytes of tiles; got %d", 3*hudPackSize, len(tiles))
			}
			if len(gfx.Palette) != hudPaletteSize {
				t.Errorf("expected %d colors; got %d", hudPaletteSize, len(gfx.Palette))
			}
			// the HUD's filler tile $F5 is solid color 3, and the full heart $A0 is not:
			solid, heart := tiles[0xF5*16:0xF5*16+16], tiles[0xA0*16:0xA0*16+16]
			for _, b := range solid {
				if b != 0xFF {
					t.Fatalf("expected the filler tile $F5 to be solid: %x", solid)
				}
			}
			if string(heart) == string(solid) || string(heart) == string(make([]byte, 16)) {
				t.Errorf("expected the heart tile $A0 to have pixels and clear pixels: %x", heart)
			}
			if _, err := json.Marshal(gfx); err != nil {
				t.Fatal(err)
			}
		})
	}
	if found == 0 {
		t.Skip("no ROMs found")
	}
}

func TestDebug_Progress(t *testing.T) {
	gs := newDebugScenario(t, 0xFF)
	e := gs.e
	e.WRAM[0xF3C5] = 1
	debugRun(t, &gs, debugOp{T: debugOpProgress, V: 3})
	if v := e.WRAM[0xF3C5]; v != 3 {
		t.Errorf("expected progress 3; got %d", v)
	}
	cmd := &debugCmd{gs.g}
	_ = cmd.Execute(&debugArgs{Ops: []debugOp{{T: debugOpProgress, V: 9}}})
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected a bad level to be rejected; %d queued", n)
	}
}
