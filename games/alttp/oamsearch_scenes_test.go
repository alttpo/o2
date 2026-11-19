package alttp

// Busy gameplay for the OAM search measurements (oamsearch_cycles_test.go), on the vanilla JP ROM:
//
//	OAM_COLLECT=1 go test -run TestOAMSearch_Survey -timeout 90m   # every room, ~75 overworld areas: ~5 min
//	OAM_COLLECT=1 go test -run TestOAMSearch_Busy -timeout 120m    # the busiest of those and the bosses, longer: ~3 min
//	OAM_SNAPS=$TMPDIR/o2-oamsnaps-busy.bin go test -run 'TestOAMSearch_(Scenes|Occupancy|Cycles)' -v
//
// Rooms are loaded through an entrance moved to the room (enterRoom), overworld areas by crossing their edges or
// leaving through random entrances (walkOverworld); Link fights with every projectile and medallion (play).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// the instruction after Module_PreDungeon's `JSR LoadUnderworldEntrance` on the JP ROM (US $02:824D): the entrance's
// room, Link's position, the scroll, the camera triggers and the room bounds are set, the room not yet loaded.
const jpPreDungeonAfterEntrance = 0x02_8157

func (g *realGame) wram16(a int) uint16       { return uint16(g.e.WRAM[a]) | uint16(g.e.WRAM[a+1])<<8 }
func (g *realGame) setWram16(a int, v uint16) { g.e.WRAM[a], g.e.WRAM[a+1] = byte(v), byte(v>>8) }

// enterRoom loads the underworld room through the entrance: everything the entrance placed in its own room is moved
// by the offset from that room to the target room on the underworld's grid of 512 pixel rooms (16 per row), so Link
// stands where the entrance would have put him, in the target room. Returns whether Link can play.
func (g *realGame) enterRoom(entrance uint8, room uint16) bool {
	g.idle()
	w := g.e.WRAM[:]
	w[0x010E], w[0x010F] = entrance, 0
	w[0x1B] = 0
	w[0x10], w[0x11] = 0x06, 0x00
	// run until the entrance is loaded:
	if !g.stepUntil(jpPreDungeonAfterEntrance, 5_000_000) {
		return false
	}
	from := g.wram16(0xA0)
	dx := (int(room&0xF) - int(from&0xF)) * 0x200
	dy := (int(room>>4) - int(from>>4)) * 0x200
	add := func(dv int, addrs ...int) {
		for _, a := range addrs {
			g.setWram16(a, uint16(int(g.wram16(a))+dv))
		}
	}
	g.setWram16(0xA0, room)
	g.setWram16(0x048E, room)
	add(dy, 0x20, 0xE6, 0xE8, 0x0122, 0x0124, 0x0618, 0x061A, 0x0600, 0x0602, 0x0604, 0x0606)
	add(dx, 0x22, 0xE0, 0xE2, 0x011E, 0x0120, 0x061C, 0x061E, 0x0608, 0x060A, 0x060C, 0x060E)
	if !g.settle() {
		return false
	}
	g.placeOnFloor()
	return true
}

// idle waits, with no input, for Link to be in his normal state with the sprites not frozen (a medallion cast
// done), as the game is before it lets him leave the room or area: up to 10 seconds.
func (g *realGame) idle() {
	w := g.e.WRAM[:]
	for f := 0; f < 600 && (w[0x5D] != 0 || w[0x0FC1] != 0) && g.runFrame(0); f++ {
		g.refill()
	}
}

// stepUntil steps the CPU until the PC is at pc; false if the game crashed or n instructions ran first.
func (g *realGame) stepUntil(pc uint32, n int) (ok bool) {
	if g.crashed {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			g.t.Logf("crashed: %v: %s", r, g.where())
			g.crashed, ok = true, false
		}
	}()
	for i := 0; g.e.GetPC() != pc; i++ {
		if i > n {
			g.crashed = true
			return false
		}
		g.e.Step()
	}
	return true
}

// placeOnFloor moves Link, within the quadrant the camera shows, to the open floor nearest its middle (a 4x4 block of
// tiles of attribute 0 in the room's tile attribute map, $7F2000 or $7F3000 on the lower level), out of a doorway.
func (g *realGame) placeOnFloor() {
	w := g.e.WRAM[:]
	attr := 0x12000
	if w[0xEE] != 0 {
		attr = 0x13000
	}
	ly, lx := int(g.wram16(0x20)), int(g.wram16(0x22))
	qy, qx := ly&^0xFF, lx&^0xFF
	best, by, bx := 1<<30, -1, -1
	for ty := 2; ty < 30; ty++ {
		for tx := 2; tx < 30; tx++ {
			ok := true
			for dy := 0; dy < 4 && ok; dy++ {
				for dx := 0; dx < 4 && ok; dx++ {
					y, x := ((qy&0x1FF)>>3)+ty+dy-1, ((qx&0x1FF)>>3)+tx+dx-1
					if y >= 64 || x >= 64 || w[attr+y*64+x] != 0 {
						ok = false
					}
				}
			}
			if !ok {
				continue
			}
			d := (ty-15)*(ty-15) + (tx-15)*(tx-15)
			if d < best {
				best, by, bx = d, ty, tx
			}
		}
	}
	if by < 0 {
		return
	}
	g.setWram16(0x20, uint16(qy+by*8-8))
	g.setWram16(0x22, uint16(qx+bx*8))
	w[0x6C] = 0
}

// moveOnOverworld moves Link within the overworld area, and the camera with him as its scrolling does: centered on
// him within the area's camera bounds ($0600/$0602 y, $0604/$0606 x), with the BG scroll and the camera triggers
// moved by as much, and the map16 streaming positions ($84, $86, $88) by the 16 pixel tiles the camera crossed.
func (g *realGame) moveOnOverworld(x, y uint16) {
	clamp := func(v, lo, hi int) int { return max(lo, min(hi, v)) }
	camX, camY := int(g.wram16(0xE2)), int(g.wram16(0xE8))
	nx := clamp(int(x)-0x80, int(g.wram16(0x0604)), int(g.wram16(0x0606)))
	ny := clamp(int(y)-0x70, int(g.wram16(0x0600)), int(g.wram16(0x0602)))
	tdx, tdy := (nx>>4)-(camX>>4), (ny>>4)-(camY>>4)
	dx, dy := nx-camX, ny-camY
	add := func(dv int, addrs ...int) {
		for _, a := range addrs {
			g.setWram16(a, uint16(int(g.wram16(a))+dv))
		}
	}
	add(dx, 0xE0, 0xE2, 0x011E, 0x0120, 0x061C, 0x061E)
	add(dy, 0xE6, 0xE8, 0x0122, 0x0124, 0x0618, 0x061A)
	add(tdx*2+tdy*0x80, 0x84)
	g.setWram16(0x86, uint16((int(g.wram16(0x86))+tdx)&0x1F))
	g.setWram16(0x88, uint16((int(g.wram16(0x88))+tdy)&0x1F))
	g.setWram16(0x22, x)
	g.setWram16(0x20, y)
}

// crossEdge moves Link next to the edge of the overworld area in the direction (0 up, 1 down, 2 left, 3 right) and
// walks across it, for the game to load the next area with its sprites; where he is along the edge, then a quarter,
// half and three quarters of the way, until one gets across. Returns whether a new area loaded and Link can play.
func (g *realGame) crossEdge(dir int) bool {
	g.idle()
	w := g.e.WRAM[:]
	area := w[0x8A]
	top, bottom := g.wram16(0x0600), g.wram16(0x0602)+0xE0
	left, right := g.wram16(0x0604), g.wram16(0x0606)+0x100
	for try := 0; try < 4; try++ {
		if w[0x10] != 0x09 || w[0x11] != 0 || w[0x8A] != area {
			return false
		}
		x, y := g.wram16(0x22), g.wram16(0x20)
		if try > 0 {
			x = left + (right-left)*uint16(try)/4
			y = top + (bottom-top)*uint16(try)/4
		}
		switch dir {
		case 0:
			y = top + 0x08
		case 1:
			y = bottom - 0x28
		case 2:
			x = left + 0x04
		case 3:
			x = right - 0x18
		}
		g.moveOnOverworld(x, y)
		in := []uint16{0x0800, 0x0400, 0x0200, 0x0100}[dir]
		for f := 0; f < 150; f++ {
			g.refill()
			g.runFrame(in)
			if w[0x8A] != area && w[0x10] == 0x09 && w[0x11] == 0 {
				return true
			}
		}
	}
	return false
}

// outOfEntrance goes through the entrance and walks back out of its door, onto the overworld outside it.
func (g *realGame) outOfEntrance(e uint8) bool {
	g.idle()
	if !g.enterEntrance(e) {
		return false
	}
	g.walk(0x0400, 90)
	return g.e.WRAM[0x10] == 0x09 && g.settle()
}

// fullInventory gives file 1 everything that throws projectiles or draws much: the golden sword (beams at full
// health), every Y item with its upgrades, full health, magic, bombs and arrows.
func fullInventory(f []byte) {
	f[0x340] = 3  // bow and arrows
	f[0x341] = 2  // red boomerang
	f[0x342] = 1  // hookshot
	f[0x343] = 30 // bombs
	f[0x344] = 2  // powder
	f[0x345] = 1  // fire rod
	f[0x346] = 1  // ice rod
	f[0x347] = 1  // bombos
	f[0x348] = 1  // ether
	f[0x349] = 1  // quake
	f[0x34A] = 1  // lamp
	f[0x34B] = 1  // hammer
	f[0x350] = 1  // cane of somaria
	f[0x351] = 1  // cane of byrna
	f[0x353] = 2  // mirror
	f[0x354] = 2  // titan's mitt
	f[0x355] = 1  // boots
	f[0x356] = 1  // flippers
	f[0x357] = 1  // moon pearl
	f[0x359] = 4  // golden sword
	f[0x35A] = 3  // mirror shield
	f[0x35B] = 2  // red mail
	f[0x36C], f[0x36D] = 0xA0, 0xA0
	f[0x36E] = 0x80 // magic
	f[0x370], f[0x371] = 0x07, 0x07
	f[0x377] = 50 // arrows
	f[0x37B] = 1  // half magic
}

// the Y items the fights use: the menu slot ($0202) and item code ($0303) of each.
var fightItems = []struct {
	name       string
	slot, code uint8
}{
	{"bow", 1, 3}, {"boomerang", 2, 2}, {"hookshot", 3, 14}, {"bombs", 4, 1}, {"powder", 5, 10},
	{"fire rod", 6, 5}, {"ice rod", 7, 6}, {"bombos", 8, 15}, {"ether", 9, 16}, {"quake", 10, 17},
	{"hammer", 12, 4}, {"somaria", 17, 18}, {"byrna", 18, 13},
}

// refill keeps health, magic, bombs and arrows full: sword beams need full health.
func (g *realGame) refill() {
	w := g.e.WRAM[:]
	w[0xF36D] = w[0xF36C]
	w[0xF36E] = 0x80
	w[0xF343] = 30
	w[0xF377] = 50
}

// nearestEnemy returns the direction input towards the nearest live sprite, or 0 if there is none.
func (g *realGame) nearestEnemy() uint16 {
	w := g.e.WRAM[:]
	lx, ly := int(g.wram16(0x22))+8, int(g.wram16(0x20))+12
	best, bdx, bdy := 1<<30, 0, 0
	for i := 0; i < 16; i++ {
		if w[0x0DD0+i] < 9 {
			continue
		}
		x := int(w[0x0D10+i]) | int(w[0x0D30+i])<<8
		y := int(w[0x0D00+i]) | int(w[0x0D20+i])<<8
		dx, dy := x+8-lx, y+8-ly
		if d := dx*dx + dy*dy; d < best {
			best, bdx, bdy = d, dx, dy
		}
	}
	if best == 1<<30 {
		return 0
	}
	var in uint16
	if bdx > 12 {
		in |= 0x0100
	} else if bdx < -12 {
		in |= 0x0200
	}
	if bdy > 12 {
		in |= 0x0400
	} else if bdy < -12 {
		in |= 0x0800
	}
	return in
}

// play fights for n frames and returns the snapshots of the scene: walks towards the nearest enemy (or explores in
// a random direction when there is none, or when stuck), taps the sword (beams), charges and releases spin attacks,
// and uses the Y items in fightItems (the medallions now and then), with health, magic and ammo kept full.
func (g *realGame) play(seed uint32, n int, scene uint16) (snaps []oamSnap) {
	r := rng(seed | 1)
	dirs := []uint16{0x0800, 0x0400, 0x0200, 0x0100, 0x0900, 0x0A00, 0x0500, 0x0600}
	var dir, held uint16
	explore, stuck, action := 0, 0, 30
	var lastPos uint32
	var lastMask [16]byte
	same, inSub := 0, 0
	w := g.e.WRAM[:]
	for f := 0; f < n; f++ {
		pos := uint32(g.wram16(0x22)) | uint32(g.wram16(0x20))<<16
		if pos == lastPos {
			stuck++
		} else {
			stuck = 0
		}
		lastPos = pos
		if stuck >= 12 {
			dir, explore, stuck = dirs[r.next()%8], 40+int(r.next()%60), 0
		}
		var in uint16
		if explore > 0 {
			explore--
			in = dir
		} else if in = g.nearestEnemy(); in == 0 {
			if r.next()%90 == 0 || dir == 0 {
				dir = dirs[r.next()%8]
			}
			in = dir
		}
		// the action held: B (a tap, or a charge), Y (an item):
		if held != 0 {
			in |= held
			action--
			if action <= 0 {
				held, action = 0, 10+int(r.next()%40)
			}
		} else if action--; action <= 0 {
			switch x := r.next() % 10; {
			case x < 4:
				held, action = 0x8000, 2
			case x < 6:
				held, action = 0x8000, 70
			case w[0x5D] != 0:
				// the game only lets the Y item change while Link is in his normal state (the menu); a medallion
				// whose item changes while it is cast never finishes:
				held, action = 0, 10
			default:
				it := fightItems[r.next()%uint32(len(fightItems))]
				if it.code >= 15 && it.code <= 17 && r.next()%3 != 0 {
					// the medallions take seconds; use them less:
					it = fightItems[r.next()%7]
				}
				w[0x0202], w[0x0303] = it.slot, it.code
				held, action = 0x4000, 6
			}
		}
		m := w[0x10]
		if m != 0x07 && m != 0x09 {
			in = 0
			if f%8 == 0 {
				in = 0x8080
			}
		}
		g.refill()
		if !g.runFrame(in) {
			return
		}
		m = w[0x10]
		if m == 0x07 || m == 0x09 {
			s := oamSnap{kind: m >> 3 & 1, module: m, sub: w[0x11], scene: scene}
			if m == 0x07 {
				s.kind = 0
			}
			for i := range s.y {
				s.y[i] = w[0x0801+i*4]
			}
			snaps = append(snaps, s)
			// a game whose sprites and Link all stand still is stuck; drop the frames since it last changed:
			// a transition (a submodule) that never ends is stuck too:
			if s.sub != 0 {
				if inSub++; inSub >= 600 {
					g.t.Logf("stuck in submodule %02x for %d frames: %s", s.sub, inSub, g.where())
					g.crashed = true
					return snaps[:len(snaps)-inSub]
				}
			} else {
				inSub = 0
			}
			if m := s.usedMask(); m == lastMask {
				if same++; same >= 300 {
					g.t.Logf("frozen for %d frames: %s 5D=%02x 303=%02x FC1=%02x 112=%02x 02E4=%02x 0C4A=% x", same,
						g.where(), w[0x5D], w[0x303], w[0x0FC1], w[0x0112], w[0x02E4], w[0x0C4A:0x0C54])
					g.crashed = true
					return snaps[:len(snaps)-same]
				}
			} else {
				lastMask, same = m, 0
			}
		}
	}
	return
}

// scenes: an underworld room r is r; an overworld area a is $1000 | a (the dark world's are $40..$7F).
func sceneName(sc uint16) string {
	if sc == 0xFFFF {
		return "?"
	}
	if sc&0x1000 != 0 {
		a := sc & 0xFF
		if a >= 0x40 {
			return fmt.Sprintf("overworld DW $%02x", a)
		}
		return fmt.Sprintf("overworld LW $%02x", a)
	}
	if name, ok := bossRooms[sc]; ok {
		return fmt.Sprintf("room $%03x (%s)", sc, name)
	}
	return fmt.Sprintf("room $%03x", sc)
}

var bossRooms = map[uint16]string{
	0xC8: "Armos Knights", 0x33: "Lanmolas", 0x07: "Moldorm", 0x20: "Agahnim", 0x5A: "Helmasaur King",
	0x06: "Arrghus", 0x29: "Mothula", 0xAC: "Blind", 0xDE: "Kholdstare", 0x90: "Vitreous", 0xA4: "Trinexx",
	0x0D: "Agahnim 2", 0x1C: "GT Armos Knights", 0x6C: "GT Lanmolas", 0x4D: "GT Moldorm", 0x00: "Ganon",
}

// newFightGame starts file 1 after Agahnim (the overworld's most enemies), in the world, with fullInventory.
func newFightGame(t *testing.T, world uint8) *realGame {
	g := newRealGame(t)
	g.editSave(func(f []byte) {
		f[0x3C5] = 3
		f[0x3CA] = world
		f[0x3C8] = 0
		fullInventory(f)
	})
	g.startGame()
	return g
}

type sceneStat struct {
	scene  uint16
	frames int
	used   weighted
}

func statsOf(scene uint16, snaps []oamSnap) *sceneStat {
	st := &sceneStat{scene: scene, frames: len(snaps)}
	for i := range snaps {
		st.used.add(uint64(snaps[i].usedCount()), 1)
	}
	return st
}

func writeSnaps(t *testing.T, path string, snaps []oamSnap) {
	out := []byte("OAM2")
	for _, s := range snaps {
		out = append(out, s.kind, s.module, s.sub, byte(s.scene), byte(s.scene>>8))
		out = append(out, s.y[:]...)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
}

// surveyRooms plays every underworld room for n frames (through entrance $08) and returns the snapshots by room.
func surveyRooms(t *testing.T, rooms []uint16, n int) map[uint16][]oamSnap {
	out := map[uint16][]oamSnap{}
	g := newFightGame(t, 0x00)
	for _, r := range rooms {
		if !g.enterRoom(0x08, r) {
			t.Logf("room $%03x: did not load: %s", r, g.where())
			g = newFightGame(t, 0x00)
			continue
		}
		out[r] = append(out[r], g.play(uint32(r)*977+uint32(n), n, r)...)
		if g.crashed {
			t.Logf("room $%03x: crashed after %d frames", r, len(out[r]))
			g = newFightGame(t, 0x00)
		}
	}
	return out
}

// surveyOverworld visits the overworld areas of the world by crossing their edges (in a sweep that prefers areas not
// seen yet), playing n frames in each, and returns the snapshots by area.
func surveyOverworld(t *testing.T, world uint8, visits, n int) map[uint16][]oamSnap {
	return walkOverworld(t, world, visits, func(uint8) int { return n }, nil)
}

// walkOverworld visits overworld areas of the world by crossing their edges, preferring areas not seen yet, playing
// stay(area) frames in each (and keeping their snapshots if it is above 60), until visits or until done says so.
func walkOverworld(t *testing.T, world uint8, visits int, stay func(area uint8) int, done func(seen map[uint8]int) bool) map[uint16][]oamSnap {
	n := 0
	out := map[uint16][]oamSnap{}
	g := newFightGame(t, world)
	if world == 0 {
		// out of Link's house:
		g.walk(0x0400, 120)
	}
	r := rng(uint32(world) + 99)
	seen := map[uint8]int{}
	for v := 0; v < visits; v++ {
		if g.crashed || g.e.WRAM[0x10] != 0x09 {
			g = newFightGame(t, world)
			if world == 0 {
				g.walk(0x0400, 120)
			}
			if g.e.WRAM[0x10] != 0x09 {
				t.Logf("not on the overworld: %s", g.where())
				return out
			}
		}
		area := g.e.WRAM[0x8A]
		seen[area]++
		sc := 0x1000 | uint16(area)
		if n = stay(area); n > 60 {
			out[sc] = append(out[sc], g.play(uint32(v)*131+uint32(world), n, sc)...)
		} else {
			g.play(uint32(v)*131+uint32(world), n, sc)
		}
		if done != nil && done(seen) {
			return out
		}
		// cross towards the least seen neighbor that the edge lets through; try others when blocked:
		order := []int{0, 1, 2, 3}
		for i := range order {
			j := i + int(r.next())%(4-i)
			order[i], order[j] = order[j], order[i]
		}
		neighbor := func(d int) uint8 {
			a := int(area & 0x3F)
			switch d {
			case 0:
				a -= 8
			case 1:
				a += 8
			case 2:
				a--
			case 3:
				a++
			}
			return uint8(a&0x3F) | area&0x40
		}
		sort.SliceStable(order, func(i, j int) bool { return seen[neighbor(order[i])] < seen[neighbor(order[j])] })
		crossed := false
		if r.next()%5 != 0 {
			for _, d := range order {
				if g.e.WRAM[0x10] == 0x09 && g.crossEdge(d) {
					crossed = true
					break
				}
			}
		}
		// somewhere else: out of a random entrance's door (in either world), until one leads out:
		for i := 0; !crossed && i < 8; i++ {
			crossed = g.outOfEntrance(uint8(1 + r.next()%0x84))
		}
	}
	return out
}

func allRooms() (rooms []uint16) {
	for r := uint16(0); r < 0x128; r++ {
		rooms = append(rooms, r)
	}
	return
}

// TestOAMSearch_Survey plays every underworld room and walks the overworld in both worlds, fighting, and lists the
// scenes by how many OAM entries they use. Saves the snapshots to $OAM_SNAPS (or oamsnaps-survey.bin in the temporary
// directory).
func TestOAMSearch_Survey(t *testing.T) {
	if os.Getenv("OAM_COLLECT") == "" {
		t.Skip("set OAM_COLLECT=1")
	}
	var all []oamSnap
	var stats []*sceneStat
	addAll := func(m map[uint16][]oamSnap) {
		for sc, snaps := range m {
			all = append(all, snaps...)
			stats = append(stats, statsOf(sc, snaps))
		}
	}
	addAll(surveyRooms(t, allRooms(), 300))
	addAll(surveyOverworld(t, 0x00, 250, 300))
	addAll(surveyOverworld(t, 0x40, 250, 300))
	sort.Slice(stats, func(i, j int) bool {
		_, _, a, _, _ := stats[i].used.stats()
		_, _, b, _, _ := stats[j].used.stats()
		return a > b
	})
	for i, st := range stats {
		if i >= 60 {
			break
		}
		mean, p50, p90, _, mx := st.used.stats()
		t.Logf("%-32s %5d frames: entries used mean %5.1f p50 %3d p90 %3d max %3d", sceneName(st.scene), st.frames, mean,
			p50, p90, mx)
	}
	path := os.Getenv("OAM_SNAPS")
	if path == "" {
		path = filepath.Join(os.TempDir(), "o2-oamsnaps-survey.bin")
	}
	writeSnaps(t, path, all)
	t.Logf("%d snapshots in %d scenes to %s", len(all), len(stats), path)
}

// TestOAMSearch_Busy plays the busiest scenes of the survey (TestOAMSearch_Survey; $OAM_SURVEY or
// o2-oamsnaps-survey.bin in the temporary directory) for longer: the 30 underworld rooms and 20 overworld areas that
// use the most OAM entries (by p90), and every boss room. Saves to $OAM_SNAPS (or o2-oamsnaps-busy.bin).
func TestOAMSearch_Busy(t *testing.T) {
	if os.Getenv("OAM_COLLECT") == "" {
		t.Skip("set OAM_COLLECT=1")
	}
	survey := os.Getenv("OAM_SURVEY")
	if survey == "" {
		survey = filepath.Join(os.TempDir(), "o2-oamsnaps-survey.bin")
	}
	t.Setenv("OAM_SNAPS", survey)
	byScene := map[uint16][]oamSnap{}
	for _, s := range loadOAMSnaps(t) {
		byScene[s.scene] = append(byScene[s.scene], s)
	}
	var rooms, areas []*sceneStat
	for sc, snaps := range byScene {
		st := statsOf(sc, snaps)
		if sc&0x1000 != 0 {
			areas = append(areas, st)
		} else {
			rooms = append(rooms, st)
		}
	}
	byP90 := func(l []*sceneStat) {
		sort.Slice(l, func(i, j int) bool {
			_, _, a, _, _ := l[i].used.stats()
			_, _, b, _, _ := l[j].used.stats()
			return a > b
		})
	}
	byP90(rooms)
	byP90(areas)
	var roomList []uint16
	inList := map[uint16]bool{}
	for i := 0; i < 30 && i < len(rooms); i++ {
		roomList, inList[rooms[i].scene] = append(roomList, rooms[i].scene), true
	}
	for r := range bossRooms {
		if !inList[r] {
			roomList = append(roomList, r)
		}
	}
	sort.Slice(roomList, func(i, j int) bool { return roomList[i] < roomList[j] })
	busyArea := map[uint8]bool{}
	for i := 0; i < 20 && i < len(areas); i++ {
		busyArea[uint8(areas[i].scene)] = true
	}

	var all []oamSnap
	var stats []*sceneStat
	addAll := func(m map[uint16][]oamSnap) {
		for sc, snaps := range m {
			all = append(all, snaps...)
			stats = append(stats, statsOf(sc, snaps))
		}
	}
	addAll(surveyRooms(t, roomList, 3000))
	for _, world := range []uint8{0x00, 0x40} {
		played := map[uint8]bool{}
		want := 0
		for a := range busyArea {
			if a&0x40 == world {
				want++
			}
		}
		addAll(walkOverworld(t, world, 400, func(area uint8) int {
			if busyArea[area] && !played[area] {
				played[area] = true
				return 1500
			}
			return 30
		}, func(map[uint8]int) bool { return len(played) >= want }))
		t.Logf("world $%02x: played %d of %d busy areas", world, len(played), want)
	}
	sort.Slice(stats, func(i, j int) bool {
		_, _, a, _, _ := stats[i].used.stats()
		_, _, b, _, _ := stats[j].used.stats()
		return a > b
	})
	for _, st := range stats {
		mean, p50, p90, _, mx := st.used.stats()
		t.Logf("%-32s %5d frames: entries used mean %5.1f p50 %3d p90 %3d max %3d", sceneName(st.scene), st.frames, mean,
			p50, p90, mx)
	}
	path := os.Getenv("OAM_BUSY")
	if path == "" {
		path = filepath.Join(os.TempDir(), "o2-oamsnaps-busy.bin")
	}
	writeSnaps(t, path, all)
	t.Logf("%d snapshots in %d scenes to %s", len(all), len(stats), path)
}

// TestOAMSearch_Scenes lists the scenes of the snapshots ($OAM_SNAPS) by how many OAM entries they use (by p90).
func TestOAMSearch_Scenes(t *testing.T) {
	byScene := map[uint16][]oamSnap{}
	for _, s := range loadOAMSnaps(t) {
		byScene[s.scene] = append(byScene[s.scene], s)
	}
	var stats []*sceneStat
	for sc, snaps := range byScene {
		stats = append(stats, statsOf(sc, snaps))
	}
	sort.Slice(stats, func(i, j int) bool {
		_, _, a, _, _ := stats[i].used.stats()
		_, _, b, _, _ := stats[j].used.stats()
		return a > b
	})
	for _, st := range stats {
		mean, p50, p90, _, mx := st.used.stats()
		t.Logf("%-32s %5d frames: entries used mean %5.1f p50 %3d p90 %3d max %3d", sceneName(st.scene), st.frames, mean,
			p50, p90, mx)
	}
}
