package alttp

import (
	"o2/games/alttp/underworldmap"
	"os"
	"testing"
	"time"
)

// roomWarpMap returns the underworld map of the vanilla JP ROM, once it has every room, or skips the test without it.
func roomWarpMap(t *testing.T) ([]byte, *underworldmap.Stream) {
	t.Helper()
	rom, err := os.ReadFile("alttp-jp-vanilla.sfc")
	if err != nil {
		t.Skip(err)
	}
	s := underworldmap.Shared(rom)
	deadline := time.After(2 * time.Minute)
	for {
		_, done, err, changed := s.Progress(1 << 30)
		if done {
			if err != nil {
				t.Fatal(err)
			}
			return rom, s
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("the underworld map took too long")
		}
	}
}

// reachablePoint returns a point of the room where Link can stand and is reachable, on layer 0.
func reachablePoint(t *testing.T, r *underworldmap.StreamRoom) (x, y uint16) {
	t.Helper()
	for i, c := range r.Walk.Cells[0] {
		if c&underworldmap.WalkReachable != 0 {
			return uint16(i%64) * 8, uint16(i/64)*8 - 8
		}
	}
	t.Fatalf("room $%03x has no reachable point", r.Room.Room)
	return
}

func TestDebug_RoomWarpFromOverworld(t *testing.T) {
	rom, s := roomWarpMap(t)
	gs := newDebugScenario(t, 0xFF)
	gs.g.ProvideUnpatchedROM(rom)
	e := gs.e

	// the Hyrule Castle entrance hall:
	const room = 0x061
	r := s.Room(room)
	if r == nil {
		t.Fatal("no room $061")
	}
	x, y := reachablePoint(t, r)

	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{{T: debugOpWarp, W: "room", R: room, X: x, Y: y}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	// stage 1: through the entrance that starts in the room, Hyrule Castle's main entrance:
	if m, c := e.WRAM[0x10], e.WRAM[0x010C]; m != 0x0F || c != 0x06 {
		t.Fatalf("expected module $0F then $06; got $%02x then $%02x", m, c)
	}
	if v := e.WRAM[0x010E]; v != 0x04 {
		t.Errorf("entrance: expected $04; got $%02x", v)
	}
	if n := len(gs.g.debug.queue); n != 1 {
		t.Fatalf("expected the warp to stay queued; %d queued", n)
	}

	// the entrance is loaded and Link stands in its room:
	var ent *underworldmap.Entrance
	for _, st := range r.Starts {
		if st.ID == 0x04 {
			ent = st
		}
	}
	if ent == nil {
		t.Fatal("entrance $04 doesn't start in room $061")
	}
	e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0x1B] = 0x07, 0x00, 0x01
	e.WRAM[0x040C], e.WRAM[0x0AA1] = ent.Palace, ent.Blockset
	setWram16(e.WRAM[:], 0xA0, ent.Room)
	// where the entrance puts Link, in the room: that isn't the end of the warp:
	setWram16(e.WRAM[:], 0x22, 0x0200+ent.Start.X)
	setWram16(e.WRAM[:], 0x20, 0x0C00+ent.Start.Y)
	// the room's colors: a BG color, the HUD's and a sprite's, and the auxiliary buffer's:
	setWram16(e.WRAM[:], 0xC540, 0x1234)
	setWram16(e.WRAM[:], 0xC502, 0x2345)
	setWram16(e.WRAM[:], 0xC5DE, 0x3456)
	setWram16(e.WRAM[:], 0xC340, 0x1234)
	gs.g.debug.queue[0].sent = time.Now().Add(-time.Second)
	for i := 0; i < 4; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}

	// stage 2: falling into the room:
	if v := e.WRAM[0x11]; v != 0x07 {
		t.Fatalf("expected submodule $07; got $%02x", v)
	}
	f := roomFallFor(room, x, y, gs.g.debug.queue[0].op.room.fullX, gs.g.debug.queue[0].op.room.fullY)
	for _, c := range []struct {
		name string
		addr uint32
		want uint16
	}{
		{"room", 0xA0, room},
		{"previous room", 0xA2, room},
		{"x", 0x22, f.x},
		{"landing y", 0x51, f.y},
		{"start y", 0x20, f.startY},
		{"BG2 h scroll", 0xE2, f.sx},
		{"BG2 v scroll", 0xE8, f.sy},
		{"room bounds y.a0", 0x0600, f.boundsY[0]},
		{"room bounds x.b1", 0x060E, f.boundsX[3]},
		{"camera y", 0x0618, f.camY},
		{"camera x", 0x061C, f.camX},
	} {
		if v := wram16(e.WRAM[:], c.addr); v != c.want {
			t.Errorf("%s: expected $%04x; got $%04x", c.name, c.want, v)
		}
	}
	if v := e.WRAM[0x5D]; v != 0x01 {
		t.Errorf("Link's state: expected falling; got $%02x", v)
	}
	if v := e.WRAM[0xA4]; int8(v) != ent.Floor+1 {
		t.Errorf("floor: expected %d before the fall; got %d", ent.Floor+1, int8(v))
	}
	// the screen dark at once, as at the end of the fall's fade out, and the fall at the step that loads the room:
	if v := e.WRAM[0xB0]; v != 0x02 {
		t.Errorf("fall step: expected $02; got $%02x", v)
	}
	for _, c := range []struct {
		name string
		addr uint32
		want uint16
	}{
		{"BG color", 0xC540, 0},
		{"sprite color", 0xC5DE, 0},
		{"HUD color", 0xC502, 0x2345},
		{"auxiliary color", 0xC340, 0x1234},
		{"darkening_or_lightening_screen", 0xC009, 2},
		{"palette_filter_countdown", 0xC007, 0},
	} {
		if v := wram16(e.WRAM[:], c.addr); v != c.want {
			t.Errorf("%s: expected $%04x; got $%04x", c.name, c.want, v)
		}
	}

	// Link lands at the point; the warp is done:
	e.WRAM[0x11] = 0
	setWram16(e.WRAM[:], 0x20, f.y)
	gs.g.debug.queue[0].sent = time.Now().Add(-time.Second)
	for i := 0; i < 2; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected the warp done; %d queued", n)
	}
}

func TestDebug_RoomWarpRejects(t *testing.T) {
	rom, s := roomWarpMap(t)
	gs := newDebugScenario(t, 0xFF)
	gs.g.ProvideUnpatchedROM(rom)
	x, y := reachablePoint(t, s.Room(0x061))
	x2, y2 := uint16(0), uint16(0)
	for i, c := range s.Room(0x002).Walk.Cells[1] {
		if c&underworldmap.WalkReachable != 0 {
			x2, y2 = uint16(i%64)*8, uint16(i/64)*8-8
			break
		}
	}
	if s.Room(0x002).Walk.At(x2, y2, 1)&underworldmap.WalkReachable == 0 {
		t.Fatal("room $002 has no reachable point on BG2")
	}

	cmd := &debugCmd{gs.g}
	if err := cmd.Execute(&debugArgs{Ops: []debugOp{
		// in the wall:
		{T: debugOpWarp, W: "room", R: 0x061, X: 0, Y: 0},
		// not a multiple of 8:
		{T: debugOpWarp, W: "room", R: 0x061, X: x + 1, Y: y},
		// no such room:
		{T: debugOpWarp, W: "room", R: 0x128, X: x, Y: y},
		// no BG2 in the room:
		{T: debugOpWarp, W: "room", R: 0x061, X: x, Y: y, L: 1},
		// the passage north of the Sanctuary is on BG2, not BG1:
		{T: debugOpWarp, W: "room", R: 0x002, X: x2, Y: y2, L: 0},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(gs.g.debug.queue); n != 0 {
		t.Errorf("expected bad room warps to be rejected; %d queued", n)
	}
}

// TestRoomFallFor checks the room's bounds and camera against an entrance of the ROM's entrance table: entrance $02,
// room $012 at ($04F8, $03C0), whose bounds are $03, $02, $03, $03 and $04, $04, $04, $05 and whose camera's y
// trigger is $0187 at the scroll of $0310.
func TestRoomFallFor(t *testing.T) {
	f := roomFallFor(0x012, 0x0F8, 0x1C0, false, false)
	if f.x != 0x04F8 || f.y != 0x03C0 {
		t.Errorf("position: got ($%04x, $%04x)", f.x, f.y)
	}
	if f.boundsY != [4]uint16{0x0300, 0x0200, 0x0310, 0x0310} {
		t.Errorf("bounds y: got %04x", f.boundsY)
	}
	if f.boundsX != [4]uint16{0x0400, 0x0400, 0x0400, 0x0500} {
		t.Errorf("bounds x: got %04x", f.boundsX)
	}
	if f.sy != 0x0310 || f.camY != 0x0187 {
		t.Errorf("v scroll and camera: got $%04x, $%04x", f.sy, f.camY)
	}
	if f.sx != 0x0400 || f.camX != 0x007F {
		t.Errorf("h scroll and camera: got $%04x, $%04x", f.sx, f.camX)
	}
	if f.qx != 0 || f.qy != 2 {
		t.Errorf("quadrant: got %d, %d", f.qx, f.qy)
	}
	// Link's house, room $104, is below the dungeons' rooms:
	if f := roomFallFor(0x104, 0x178, 0x178, false, false); f.y != 0x2178 || f.boundsY[1] != 0x2000 {
		t.Errorf("room $104: got y $%04x, bounds y.b0 $%04x", f.y, f.boundsY[1])
	}
}

// TestRoomFallMatchesEntrances compares where the fall puts the screen and the camera's triggers with the US ROM's
// entrance table (EntranceData: .room_id, .horizontal_scroll, .vertical_scroll, .y_coordinate, .x_coordinate,
// .camera_trigger_y, .camera_trigger_x), for Link where each entrance puts him.
func TestRoomFallMatchesEntrances(t *testing.T) {
	rom, err := os.ReadFile("../../../alttp-us.sfc")
	if err != nil {
		t.Skip(err)
	}
	if len(rom)&0x7FFF == 0x200 {
		rom = rom[0x200:]
	}
	m, err := underworldmap.Generate(rom, underworldmap.Options{NoImage: true})
	if err != nil {
		t.Fatal(err)
	}
	w := func(table uint32, i int) uint16 {
		o := (table>>16&0x7F)<<15 | table&0x7FFF + uint32(i)*2
		return uint16(rom[o]) | uint16(rom[o+1])<<8
	}
	n, bad := 0, 0
	for i := 0; i < underworldmap.EntranceCount; i++ {
		room := w(0x02C813, i)
		if room >= underworldmap.RoomCount || m.Rooms[room] == nil {
			continue
		}
		sx, sy, y, x, camY, camX := w(0x02CD45, i), w(0x02CE4F, i), w(0x02CF59, i), w(0x02D063, i), w(0x02D16D, i), w(0x02D277, i)
		rx, ry := x-(room&0x0F)*0x200, y-(room>>4)*0x200
		if rx >= 0x200 || ry >= 0x200 {
			continue
		}
		q := 0
		if rx >= 0x100 {
			q |= 1
		}
		if ry >= 0x100 {
			q |= 2
		}
		fs := m.Rooms[room].FullSize[q]
		f := roomFallFor(room, rx, ry, fs&1 != 0, fs&2 != 0)
		n++
		// the entrances that drop Link into the room have the camera's y trigger one pixel lower, and an odd Y can
		// round the other way: a pixel doesn't matter:
		near := func(a, b uint16) bool { return a-b+1 <= 2 }
		if !near(f.sx, sx) || !near(f.sy, sy) || !near(f.camX, camX) || !near(f.camY, camY) {
			bad++
			t.Logf("entrance $%02x room $%03x at ($%04x, $%04x): scroll ($%04x, $%04x) camera ($%04x, $%04x); expected ($%04x, $%04x) ($%04x, $%04x)",
				i, room, x, y, f.sx, f.sy, f.camX, f.camY, sx, sy, camX, camY)
		}
	}
	t.Logf("%d of %d entrances differ", bad, n)
	// Link's house (entrance $00) starts him in bed and entrance $43 (room $103) near the top, where the screen is not
	// centered on him:
	if bad > 2 {
		t.Errorf("%d of %d entrances differ", bad, n)
	}
}

// TestDebug_RoomWarpLowerLevel warps to Swamp Palace's water channel in room $034, which is on the lower level with
// BG1's collision: $EE = 0 and $0476 = 1.
func TestDebug_RoomWarpLowerLevel(t *testing.T) {
	rom, s := roomWarpMap(t)
	gs := newDebugScenario(t, 0x0C)
	gs.g.ProvideUnpatchedROM(rom)
	e := gs.e
	r := s.Room(0x034)
	if r == nil {
		t.Fatal("no room $034")
	}
	// already in Swamp Palace:
	ent := r.EntranceState
	e.WRAM[0x10], e.WRAM[0x11], e.WRAM[0x1B] = 0x07, 0x00, 0x01
	e.WRAM[0x040C], e.WRAM[0x0AA1] = r.Palace, ent.Blockset
	e.WRAM[0x0476] = 0

	debugCmd := &debugCmd{gs.g}
	if err := debugCmd.Execute(&debugArgs{Ops: []debugOp{{T: debugOpWarp, W: "room", R: 0x034, X: 15 * 8, Y: 46*8 - 8}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		gs.g.cooldownTime = time.Time{}
		gs.runFrame(t)
	}
	if v := e.WRAM[0x11]; v != 0x07 {
		t.Fatalf("expected the fall (submodule $07); got $%02x", v)
	}
	if ee, m := e.WRAM[0xEE], e.WRAM[0x0476]; ee != 0 || m != 1 {
		t.Errorf("expected $EE = 0 and $0476 = 1; got %d and %d", ee, m)
	}
}
