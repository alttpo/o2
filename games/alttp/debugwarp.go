package alttp

import (
	"fmt"
	"log"
	"o2/games/alttp/underworldmap"
	"o2/snes"
	"time"

	"github.com/alttpo/snes/asm"
)

// Warps of the debug page
//
// A warp goes through the iris like a door does: the game's module $0F closes the spotlight and then runs the module
// in $010C, which loads the place:
//
//   - to an underworld entrance, module $06 (Module_PreDungeon) loads entrance $010E, like walking into a door of the
//     overworld (Overworld_Entrance);
//   - to an overworld exit, module $08 (LoadOverworldFromDungeon) loads the exit of room $A0 from the underworld exit
//     table, like walking out of a dungeon (Dung_HandleExitToOverworld).
//
// The page names the places by their exit's index in the underworld exit table (UnderworldExitData). Randomizers that
// shuffle entrances rewrite the table's exits to where they put the entrances, so the warps follow them. The exit's
// area also tells the world of the place, which the warp sets ($7EF3CA) so that the game loads the right world.

const (
	// UnderworldExitData.room_id, 79 words, followed by .overworld_id, 79 bytes:
	romExitRoomsUS = 0x02DD8A
	romExitRoomsJP = 0x02DAEE
	exitTableCount = 0x4F
	// the last entrance of the entrance table:
	maxEntrance = 0x84
)

func (g *Game) romExitRooms() uint32 {
	if g.rom.Header.DestinationCode == snes.RegionNorthAmerica && g.rom.Header.MaskROMVersion == 0 {
		return romExitRoomsUS
	}
	return romExitRoomsJP
}

// debugExit returns the room and the overworld area of exit i of the ROM's underworld exit table.
func (g *Game) debugExit(i uint8) (room uint16, area uint8, ok bool) {
	base := g.romExitRooms()
	room, ok1 := g.romU16(base + uint32(i)*2)
	area, ok2 := g.romU8(base + exitTableCount*2 + uint32(i))
	return room, area, ok1 && ok2
}

// exitUsesTable reports whether LoadOverworldFromDungeon takes the exit of a room from the exit table; it takes the
// exit of the other rooms (the houses $0100..$017F but Link's house) from where Link entered them.
func exitUsesTable(room uint16) bool {
	return room < 0x100 || room == 0x104
}

// debugCanWarp reports whether Link is on his feet in the overworld or the underworld, where a door could be taken.
// Called with the state lock held.
func (g *Game) debugCanWarp() bool {
	m := g.wram[0x10]
	return (m == 0x07 || m == 0x09) && g.wram[0x11] == 0
}

func (g *Game) emitDebugWarp(a *asm.Emitter, op *debugOp, lbl func(string) string) {
	room, area, ok := g.debugExit(op.V)
	if !ok {
		log.Printf("alttp: debug: warp: exit $%02x is not in the ROM\n", op.V)
		return
	}
	if op.W == "exit" && !exitUsesTable(room) {
		log.Printf("alttp: debug: warp: exit $%02x of room $%03x is not in the exit table\n", op.V, room)
		return
	}
	world := uint8(0)
	if area >= 0x40 {
		world = 0x40
	}

	done := lbl("done")
	emitWarpGuard(a, done, lbl)
	if op.W == "entrance" {
		emitWarpDoor(a, op.E, 0, world, true, lbl)
	} else {
		emitWarpDoor(a, 0, room, world, true, lbl)
	}
	a.Label(done)
}

// emitWarpGuard goes to done unless Link is in the overworld or the underworld with nothing else going on: the update
// routine also runs in the menus.
func emitWarpGuard(a *asm.Emitter, done string, lbl func(string) string) {
	go_, out := lbl("go"), lbl("out")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BEQ(go_)
	a.CMP_imm8_b(0x09)
	a.BEQ(go_)
	a.Label(out)
	a.BRL(done)
	a.Label(go_)
	a.LDA_dp(0x11)
	a.BNE(out)
}

// emitWarpDoor sends Link through the iris to underworld entrance (when not 0) or out of the underworld exit of room
// exitRoom, in world (if setWorld).
func emitWarpDoor(a *asm.Emitter, entrance uint8, exitRoom uint16, world uint8, setWorld bool, lbl func(string) string) {
	// keep the small keys of the dungeon Link leaves, like SaveDungeonKeys:
	keys := lbl("keys")
	a.LDA_abs(0x040C)
	a.CMP_imm8_b(0xFF)
	a.BEQ(keys)
	a.CMP_imm8_b(0x02)
	a.BNE(lbl("hc"))
	a.LDA_imm8_b(0x00)
	a.Label(lbl("hc"))
	a.LSR()
	a.TAX()
	a.LDA_long(0x7EF36F)
	a.STA_long_x(0x7EF37C)
	a.Label(keys)

	if setWorld {
		a.LDA_imm8_b(world)
		a.STA_long(0x7EF3CA)
	}

	if entrance != 0 {
		// Dungeon_LoadEntrance keeps where Link entered from the overworld, for the exits of the houses, unless $010A
		// is set; from the underworld, keep the last one instead of the underworld's coordinates:
		over := lbl("over")
		a.LDA_dp(0x1B)
		a.BEQ(over)
		a.LDA_imm8_b(0x01)
		a.STA_abs(0x010A)
		a.Label(over)
		a.LDA_imm8_b(entrance)
		a.STA_abs(0x010E)
		a.STZ_abs(0x010F)
		a.LDA_imm8_b(0x06)
		a.STA_abs(0x010C)
	} else {
		a.LDA_imm8_b(uint8(exitRoom))
		a.STA_dp(0xA0)
		a.LDA_imm8_b(uint8(exitRoom >> 8))
		a.STA_dp(0xA1)
		a.LDA_imm8_b(0x08)
		a.STA_abs(0x010C)
	}

	// Link stands, like Overworld_Entrance does before the iris:
	a.STZ_dp(0x4D)
	a.STZ_dp(0x46)
	a.STZ_dp(0x5D)
	a.LDA_imm8_b(0x0F)
	a.STA_dp(0x10)
	a.STZ_dp(0x11)
	a.STZ_dp(0xB0)
}

// Warps to a point of an underworld room
//
// The debug page's map picks a room and where Link is placed in it (x, y in the room's pixels and the layer), moved
// by the page to where the room's walk data (underworldmap.Walk) says he can stand. The room's graphics depend on the
// entrance Link came in by (the dungeon's main tileset $0AA1 and the dungeon $040C), so the warp goes in two stages:
//
//  1. Unless Link is already in the underworld with the tileset and dungeon of the entrance the map drew the room
//     from, he goes through that entrance like the entrance warp does.
//  2. Then he drops into the room as if he had fallen through a pit above it (submodule $07, Module07_07_
//     FallingTransition): the game loads the room he is in ($A0) and lands him at the Y in tiledetect_which_y_pos
//     ($51), from the top of the screen. The fall keeps his position in the room and the camera (it only moves them by
//     the distance between the rooms, nothing here, as $A2 is set to the room too), so the warp places him, the
//     scrolls, the camera's triggers and the room's bounds in the quadrant he lands in, like an entrance does
//     (EntranceData's camera_scroll_boundaries, horizontal_scroll, ...), with the screen centered on him as far as
//     the room's layout lets it scroll (underworldmap.Room.FullSize).
//
// When an entrance starts in the room, the warp goes in through it (the one the map drew the room from, if it is
// one of them), so a warp to a dungeon's entrance room takes the dungeon's own entrance.
//
// The same routine does both stages: it checks the tileset and dungeon when it runs. The warp stays queued until Link
// stands at the point (debugRoomWarpDone), and its routine is sent again every debugRoomWarpEvery until then; it does
// nothing while Link is not standing in the overworld or underworld (the iris, the fall).

const (
	debugRoomWarpTTL   = 20 * time.Second
	debugRoomWarpEvery = 400 * time.Millisecond
)

// roomWarp is what a warp to a point of a room needs from the map, looked up when the warp is asked for.
type roomWarp struct {
	entrance uint8
	blockset uint8
	palace   uint8
	// the world of the entrance, if it is known from the exit table:
	world      uint8
	worldKnown bool
	floor      int8
	floorKnown bool
	// whether the screen scrolls over the whole room in the quadrant Link lands in, horizontally and vertically:
	fullX, fullY bool
	// whether Link is on the lower level at the point ($0476), which isn't always the layer ($EE):
	lower uint8
}

// debugUnderworldMap returns the map of the unpatched ROM, which is generated the first time it is asked for.
func (g *Game) debugUnderworldMap() *underworldmap.Stream {
	if g.unpatchedROM == nil {
		return nil
	}
	return underworldmap.Shared(g.unpatchedROM)
}

// ProvideUnpatchedROM keeps the contents of the ROM before it was patched, which the underworld map is made from.
func (g *Game) ProvideUnpatchedROM(rom []byte) {
	g.unpatchedROM = rom
}

// prepareRoomWarp checks the point of a room warp is where Link can stand and looks up what the warp needs.
func (g *Game) prepareRoomWarp(op *debugOp) error {
	s := g.debugUnderworldMap()
	if s == nil {
		return fmt.Errorf("warp: no underworld map without the ROM")
	}
	r := s.Room(op.R)
	if r == nil || r.EntranceState == nil {
		return fmt.Errorf("warp: room $%03x is not on the map (yet)", op.R)
	}
	if r.Walk.At(op.X, op.Y, int(op.L))&underworldmap.WalkStandable == 0 {
		return fmt.Errorf("warp: Link can't stand at ($%03x, $%03x) on layer %d of room $%03x", op.X, op.Y, op.L, op.R)
	}
	// the entrance the map drew the room from, or one that starts in the room, the one the map drew it from if it
	// does: the warp then goes in where the player would, with that entrance's dungeon and floor:
	ent := r.EntranceState
	w := &roomWarp{
		entrance:   r.Entrance,
		blockset:   ent.Blockset,
		palace:     r.Palace,
		floor:      r.Floor,
		floorKnown: r.FloorKnown,
	}
	if len(r.Starts) > 0 {
		start := r.Starts[0]
		for _, st := range r.Starts {
			if st.ID == r.Entrance {
				start = st
			}
		}
		ent = start
		w.entrance, w.blockset, w.palace = start.ID, start.Blockset, start.Palace
		w.floor, w.floorKnown = start.Floor, true
	}
	q := 0
	if op.X >= 0x100 {
		q |= 1
	}
	if op.Y >= 0x100 {
		q |= 2
	}
	w.fullX, w.fullY = r.FullSize[q]&1 != 0, r.FullSize[q]&2 != 0
	if r.Walk.At(op.X, op.Y, int(op.L))&underworldmap.WalkLower != 0 {
		w.lower = 1
	}
	// the world of the entrance's room from the exit table:
	for i := uint8(0); i < exitTableCount; i++ {
		if room, area, ok := g.debugExit(i); ok && room == ent.Room {
			w.world, w.worldKnown = area&0x40, true
			break
		}
	}
	op.room = w
	return nil
}

// debugRoomWarpDone reports whether Link stands where the warp put him: in its room, at its point (where the fall
// lands him; the first check after the fall comes before he could move). Being in the room is not enough, as the
// entrance of stage 1 may start in it. Called with the state lock held.
func (g *Game) debugRoomWarpDone(op *debugOp) bool {
	if g.wram[0x10] != 0x07 || g.wram[0x11] != 0 || g.wram[0x1B] == 0 || g.wramU16(0xA0) != op.R {
		return false
	}
	f := roomFallFor(op.R, op.X, op.Y, false, false)
	return g.wramU16(0x22) == f.x && g.wramU16(0x20) == f.y
}

func (g *Game) emitDebugRoomWarp(a *asm.Emitter, op *debugOp, lbl func(string) string) {
	w := op.room
	if w == nil {
		return
	}
	done, stage1, stage2 := lbl("done"), lbl("stage1"), lbl("stage2")
	emitWarpGuard(a, done, lbl)

	a.LDA_dp(0x1B)
	a.BEQ(stage1)
	a.LDA_abs(0x040C)
	a.CMP_imm8_b(w.palace)
	a.BNE(stage1)
	a.LDA_abs(0x0AA1)
	a.CMP_imm8_b(w.blockset)
	a.BNE(stage1)
	// stage 1 may be too long to branch over:
	a.BRL(stage2)

	a.Label(stage1)
	emitWarpDoor(a, w.entrance, 0, w.world, w.worldKnown, lbl)
	a.BRL(done)

	a.Label(stage2)
	g.emitRoomFall(a, op, lbl)
	a.Label(done)
}

// roomFall is where the fall of a room warp puts Link and the camera.
type roomFall struct {
	x, y       uint16 // Link's landing coordinates
	startY     uint16 // where he falls from
	qx, qy     uint8  // link_quadrant_x and _y
	boundsY    [4]uint16
	boundsX    [4]uint16
	sx, sy     uint16 // BG scrolls
	camY, camX uint16 // the camera's triggers, camera_y_coord_scroll_low and camera_x_coord_scroll_low
}

func roomFallFor(room, x, y uint16, fullX, fullY bool) (f roomFall) {
	bx := (room & 0x0F) * 0x200
	by := (room >> 4) * 0x200
	f.x, f.y = bx+x, by+y

	// the room's bounds, in the quadrant Link lands in (a0, b0, a1, b1: the quadrant's and the room's):
	ax, ay := bx, by
	if x >= 0x100 {
		f.qx = 1
		ax += 0x100
	}
	if y >= 0x100 {
		f.qy = 2
		ay += 0x100
	}
	f.boundsX = [4]uint16{ax, bx, ax, bx + 0x100}
	f.boundsY = [4]uint16{ay, by, ay + 0x10, by + 0x110}

	// the screen centers Link as the entrances do (EntranceData: the scroll is $78 left of and $6D above Link where
	// the room's bounds allow), within the room where its layout scrolls over the whole room in this quadrant
	// (b0..b1), and within the quadrant otherwise (a0..a1):
	clamp := func(v, lo, hi int) uint16 {
		if v < lo {
			v = lo
		} else if v > hi {
			v = hi
		}
		return uint16(v)
	}
	if fullX {
		f.sx = clamp(int(f.x)-0x78, int(f.boundsX[1]), int(f.boundsX[3]))
	} else {
		f.sx = clamp(int(f.x)-0x78, int(f.boundsX[0]), int(f.boundsX[2]))
	}
	if fullY {
		f.sy = clamp(int(f.y)-0x6D, int(f.boundsY[1]), int(f.boundsY[3]))
	} else {
		f.sy = clamp(int(f.y)-0x6D, int(f.boundsY[0]), int(f.boundsY[2]))
	}
	f.camX = f.sx - bx + 0x7F
	f.camY = f.sy - by + 0x77
	// Link falls from above the top of the screen, like the game does (LinkState_Pits):
	f.startY = f.sy - 0x10
	return
}

// emitRoomFall starts the fall into the room, like LinkState_Pits does when Link falls through a pit.
//
// The fall's first steps fade the screen out on the room Link is in (Module07_07_00 and _01). The warp has already
// moved the screen to where it is in the room he falls into, which would show the room he is in shifted while it
// fades out. So the screen goes dark at once instead: the warp leaves the palettes as the fade out does (the colors
// it darkens are black, the room's are kept in the auxiliary buffer) with what step 00 resets, and starts the fall
// at step 02, which loads the room.
func (g *Game) emitRoomFall(a *asm.Emitter, op *debugOp, lbl func(string) string) {
	w := op.room
	f := roomFallFor(op.R, op.X, op.Y, w.fullX, w.fullY)

	// the floor, which the fall's fade in takes one from, except in rooms $89 and $4F, and $A7 which sets it:
	if w.floorKnown {
		floor := uint8(w.floor + 1)
		if lo := uint8(op.R); lo == 0x89 || lo == 0x4F {
			floor = uint8(w.floor)
		}
		a.LDA_imm8_b(floor)
		a.STA_dp(0xA4)
	}

	a.REP(0x20)
	set16 := func(addr uint16, v uint16) {
		a.LDA_imm16_w(v)
		if addr < 0x100 {
			a.STA_dp(uint8(addr))
		} else {
			a.STA_abs(addr)
		}
	}
	set16(0xA0, op.R)
	set16(0xA2, op.R)
	set16(0x048E, op.R)
	set16(0x22, f.x)
	set16(0x20, f.startY)
	set16(0x51, f.y)
	for i, v := range f.boundsY {
		set16(0x0600+uint16(i)*2, v)
	}
	for i, v := range f.boundsX {
		set16(0x0608+uint16(i)*2, v)
	}
	set16(0xE2, f.sx)
	set16(0xE0, f.sx)
	set16(0x011E, f.sx)
	set16(0x0120, f.sx)
	set16(0xE8, f.sy)
	set16(0xE6, f.sy)
	set16(0x0122, f.sy)
	set16(0x0124, f.sy)
	set16(0x0618, f.camY)
	set16(0x061A, f.camY+2)
	set16(0x061C, f.camX)
	set16(0x061E, f.camX+2)
	// Link's height above the floor:
	a.STZ_dp(0x24)
	a.SEP(0x20)

	a.LDA_imm8_b(f.qx)
	a.STA_dp(0xA9)
	a.LDA_imm8_b(f.qy)
	a.STA_dp(0xAA)
	// the layer whose collision Link has, and whether he is on the lower level, which the staircases to other rooms
	// place him by (see underworldmap's "The lower level"):
	a.LDA_imm8_b(op.L)
	a.STA_dp(0xEE)
	a.LDA_imm8_b(w.lower)
	a.STA_abs(0x0476)

	// Link falling (LinkState_Pits):
	a.LDA_imm8_b(0x01)
	a.STA_dp(0x5D)
	a.STA_abs(0x037B)
	a.LDA_imm8_b(0x06)
	a.STA_dp(0x5A)
	a.LDA_imm8_b(0x03)
	a.STA_dp(0x5B)
	a.LDA_imm8_b(0x0C)
	a.STA_dp(0x4B)
	a.LDA_imm8_b(0x10)
	a.STA_dp(0x57)
	a.LDA_imm8_b(0x09)
	a.STA_dp(0x5C)
	for _, dp := range []uint8{0x29, 0x2E, 0x3A, 0x3C, 0x46, 0x4D, 0x50, 0x5E, 0x67, 0x6C} {
		a.STZ_dp(dp)
	}
	for _, abs := range []uint16{0x02E1, 0x0301, 0x0308, 0x0309, 0x030B, 0x0345, 0x0373, 0x0376, 0x037A} {
		a.STZ_abs(abs)
	}

	emitFallFadedOut(a, op.R, lbl)
	a.LDA_imm8_b(0x07)
	a.STA_dp(0x11)
	a.LDA_imm8_b(0x02)
	a.STA_dp(0xB0)
}

// emitFallFadedOut leaves the screen as the fall's first two steps do (Module07_07_00_HandleMusicAndResetRoom, then
// ApplyPaletteFilter_bounce until dark) in the fall into room, but at once.
func emitFallFadedOut(a *asm.Emitter, room uint16, lbl func(string) string) {
	// the music of the rooms with Ganon and Moldorm's:
	if room == 0x10 || room == 0x07 || room == 0x17 {
		a.LDA_imm8_b(0xF1)
		a.STA_abs(0x012C)
	}
	// ResetTransitionPropsAndAdvance_ResetInterface:
	a.STZ_abs(0x0200)
	a.LDA_imm8_b(0x00)
	a.STA_long(0x7EC011)
	a.STA_long(0x7EC012)
	a.LDA_imm8_b(0x1F)
	a.STA_long(0x7EC00B)
	a.STZ_abs(0x045A)
	lantern := lbl("lantern")
	a.LDA_abs(0x0458)
	a.BEQ(lantern)
	a.LDA_imm8_b(0x02)
	a.STA_dp(0x99)
	a.LDA_imm8_b(0xB3)
	a.STA_dp(0x9A)
	a.Label(lantern)
	a.STZ_abs(0x0458)

	// the fade out's end: the colors it darkens (0, $20..$D7 and $E0..$EF) black, darkening_or_lightening_screen 2
	// for the fade in, the countdown 0:
	a.REP(0x30)
	a.LDA_imm16_w(0)
	a.STA_long(0x7EC500)
	for _, rng := range [][2]uint16{{0x20, 0xD8}, {0xE0, 0xF0}} {
		loop := lbl(fmt.Sprintf("black%02x", rng[0]))
		a.LDX_imm16_w(rng[0] * 2)
		a.Label(loop)
		a.STA_long_x(0x7EC500)
		a.INX()
		a.INX()
		a.CPX_imm16_w(rng[1] * 2)
		a.BNE(loop)
	}
	a.STA_long(0x7EC007)
	a.LDA_imm16_w(2)
	a.STA_long(0x7EC009)
	a.SEP(0x30)
	a.INC_dp(0x15)
}
