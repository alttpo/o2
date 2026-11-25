package underworldmap

// Where Link can stand in a room
//
// The debug page's map warp places Link at a point picked on the map, moved to the nearest place where he can stand
// and walk away from. Walk classifies each 8x8 cell of a room's tile attributes ($7F2000, as the room is drawn when it
// is entered) by what Link's tile detection does on it (TileDetect_ExecuteInner in zelda3's tile_detect.c):
//
//   - floor: Link walks and stands on it (plain floor, shallow water, grass, rupee tiles, ..., and ice and spikes,
//     which only make him slide or hurt him);
//   - passable: Link walks onto it but it does something: doorways, staircases, conveyors, ledges and warp tiles.
//     Link can walk through these but isn't put down on them;
//   - hazard: a passable cell next to which Link isn't put down either, as a small step would set it off: pits, deep
//     water, staircases, warp tiles and conveyors;
//   - everything else blocks Link: walls, pots, chests, blocks, torches, closed doors.
//
// Which layer Link is on: the game reads the attributes of BG1, or of BG2 when Link is on the lower level ($EE). In
// rooms with collision on both layers ($046C 1..3), Link can be on either anywhere, except where a layer has $1C. In
// the others, $1C in BG1's attributes marks where Link is on the lower level, BG2 (the passage north of the
// Sanctuary $002 and Tower of Hera's entrance $077 are all $1C, the lower part of room $015 is), and BG1 is where he
// is anywhere else; BG2's attributes there are filler ($00 or $1C). The staircases that swap layers ($1E, $1F,
// $3D..$3F) take the flood fill to the other layer, and so do the ledges ($28..$2F) in rooms with collision on one
// layer, where jumping off the upper level lands on the lower one.
//
// The hookshot pulls Link across gaps to the tiles it grabs (isHookshotTarget), as to the islands with the pots on
// the sides of Swamp Palace's room $036: the flood fill goes from a standable cell to the cells before such a tile, up to 16
// cells away, over anything but walls, and over walls too between two ledges it crosses (onto a raised island).
//
// The lower level: the game keeps whether Link is on the lower level in $0476 as well as in $EE. They are the same
// except in rooms whose in-room staircases only switch $0476 (kind_of_in_room_staircase 2: the water rooms of Swamp
// Palace, like $034), where Link keeps BG1's collision on both levels. The spiral and straight staircases to other
// rooms place Link by both (byte_7E0492 in Dungeon_DetectStaircase), so a warp that gets $0476 wrong leaves him in a
// wall after the next staircase. The walk splits the passable cells into regions separated by the in-room
// staircases ($1D..$1F, $3D..$3F), each of which switches the level between the regions above and below it, and
// starts from where the entrances put Link (with their $0476), from the staircases from other rooms (with the level
// the other room's staircase plane puts Link on: lower in room $034 by the staircase from $054), and from the doors
// (the level of the layer).
//
// A cell is standable when Link's feet, the 16x16 pixels from y+8 of where he is placed, cover only floor, the
// cells around them are not hazards, and he is at least 16 pixels from the room's edges (the edge would start a
// room transition). A standable cell is reachable when a flood fill through passable cells from the room's ways in
// (its doors, the tiles of its exits and where the entrances that start in it put Link) gets to it; the
// others are on top of walls, behind closed shutters or otherwise cut off.

// Walk cell flags, one byte per 8x8 cell of a layer, at row*64 + column:
const (
	// WalkStandable and WalkReachable are where Link can be placed: with x = column*8 and y = row*8 - 8, his feet
	// are on the cell and the cells to its right and below:
	WalkStandable = 1 << 0
	WalkReachable = 1 << 1
	// WalkPassable is a cell Link can walk on; WalkHazard is a passable cell he isn't put down next to:
	WalkPassable = 1 << 2
	WalkHazard   = 1 << 3
	// WalkLower is where Link is on the lower level (link_is_on_lower_level_mirror, $0476), which is not always the
	// layer whose collision he has ($EE): see "The lower level".
	WalkLower = 1 << 4
)

// WalkCells is the number of cells of a layer: 64x64.
const WalkCells = 64 * 64

// Walk is where Link can be in a room.
type Walk struct {
	// Layers are the layers with cells where Link can stand (bit 0 BG1, Cells[0]; bit 1 BG2, Cells[1]):
	Layers uint8
	Cells  [2][WalkCells]uint8
}

// StartPos is where an entrance puts Link in its room: in the room's pixels and on the layer ($EE).
type StartPos struct {
	X, Y  uint16
	Layer uint8
	// Lower is whether Link is on the lower level ($0476):
	Lower uint8
}

type attrClass uint8

const (
	attrBlocked attrClass = iota
	attrFloorClass
	attrPassable
	attrHazard
)

var attrClasses = func() (c [256]attrClass) {
	floor := []int{0x00, 0x05, 0x06, 0x07, 0x09, 0x0A, 0x14, 0x15, 0x16, 0x17, 0x21, 0x23, 0x24, 0x25,
		0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x40, 0x41, 0x45, 0x47, 0x48, 0x49, 0x4A, 0x5E, 0x5F, 0x60, 0x61,
		0x64, 0x65, 0x66, 0xA6, 0xA7, 0xBE, 0xBF,
		// the spike floor ($0D) and spikes ($44), and ice ($0E, $0F), as in Ice Palace's $04F:
		0x0D, 0x44, 0x0E, 0x0F}
	for _, a := range floor {
		c[a] = attrFloorClass
	}
	for a := 0xD0; a <= 0xEF; a++ {
		c[a] = attrFloorClass
	}
	passable := []int{0x0C, 0x1C, 0x10, 0x11, 0x12, 0x13, 0x18, 0x19, 0x1A, 0x1B,
		0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F}
	for _, a := range passable {
		c[a] = attrPassable
	}
	// doorways, shutter doors that are open and the entrance doors:
	for a := 0x80; a <= 0xAF; a++ {
		if a != 0xA6 && a != 0xA7 {
			c[a] = attrPassable
		}
	}
	hazard := []int{0x08, 0x1D, 0x1E, 0x1F, 0x20, 0x22, 0x3D, 0x3E, 0x3F, 0x4B, 0x62, 0x68, 0x69, 0x6A, 0x6B}
	for _, a := range hazard {
		c[a] = attrHazard
	}
	for a := 0x30; a <= 0x37; a++ {
		c[a] = attrHazard
	}
	for a := 0xB0; a <= 0xBD; a++ {
		c[a] = attrHazard
	}
	return
}()

// followHookshot has the flood fill follow the hookshot; a test turns it off to check it does something.
var followHookshot = true

// computeWalk classifies the cells of the room's layers. starts are where the entrances that start in the room put
// Link.
func (r *roomData) computeWalk(exits []Exit, starts []StartPos, arrivals []arrival) *Walk {
	dual := r.collision >= 1 && r.collision <= 3
	// whether a cell of a layer is where Link can be on that layer; see "Which layer Link is on":
	valid := func(l, i int) bool {
		if dual {
			return r.attrs[l*0x1000+i] != 0x1C
		}
		return (r.attrs[i] == 0x1C) == (l == 1)
	}

	w := &Walk{}
	for l := 0; l < 2; l++ {
		cells := &w.Cells[l]
		attrs := r.attrs[l*0x1000 : l*0x1000+0x1000]
		for i, a := range attrs {
			if !valid(l, i) {
				continue
			}
			switch attrClasses[a] {
			case attrFloorClass, attrPassable:
				cells[i] = WalkPassable
			case attrHazard:
				// deep water and pits are not walked on, the others are:
				if a == 0x08 || a == 0x20 || a == 0x62 || (a >= 0xB0 && a <= 0xBD) {
					cells[i] = WalkHazard
				} else {
					cells[i] = WalkPassable | WalkHazard
				}
			}
		}

		// standable cells: Link's feet on floor, no hazard around them, away from the edges:
		for row := 3; row <= 60; row++ {
			for col := 2; col <= 60; col++ {
				ok := true
				for dy := 0; dy < 2 && ok; dy++ {
					for dx := 0; dx < 2 && ok; dx++ {
						c := (row+dy)*64 + col + dx
						ok = valid(l, c) && attrClasses[attrs[c]] == attrFloorClass
					}
				}
				for dy := -1; dy <= 2 && ok; dy++ {
					for dx := -1; dx <= 2 && ok; dx++ {
						ok = cells[(row+dy)*64+col+dx]&WalkHazard == 0
					}
				}
				if ok {
					cells[row*64+col] |= WalkStandable
					w.Layers |= 1 << l
				}
			}
		}
	}

	// the flood fill goes through the passable cells of both layers, by node l*WalkCells + cell:
	var reached [2 * WalkCells]bool
	var stack []int
	// seed adds the passable cells within radius cells of pos on the layers in mask:
	seed := func(pos int, mask uint8, radius int) {
		r0, c0 := pos/64, pos%64
		for l := 0; l < 2; l++ {
			if mask&(1<<l) == 0 {
				continue
			}
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					rr, cc := r0+dy, c0+dx
					if rr < 0 || rr >= 64 || cc < 0 || cc >= 64 {
						continue
					}
					if w.Cells[l][rr*64+cc]&WalkPassable != 0 {
						stack = append(stack, l*WalkCells+rr*64+cc)
					}
				}
			}
		}
	}

	// the ways into the room, the passable cells within 4 cells of them: in rooms with collision on both layers, on the layer they are on (doors on both); in
	// the others, a position is on one layer only:
	for _, x := range exits {
		if x.Kind == ExitTagHoles || x.Kind == ExitEntrance {
			continue
		}
		mask := uint8(3)
		if dual {
			mask = 1 << (x.Tile >> 12 & 1)
		}
		seed(int(x.Tile&0x0FFF), mask, 4)
	}
	// a door's position is its top left in the wall, a few cells from the floor:
	for _, d := range r.doors {
		seed(int(d.pos), 3, 4)
	}
	for _, start := range starts {
		mask := uint8(3)
		if dual {
			mask = 1 << (start.Layer & 1)
		}
		// Link's feet:
		seed(int((start.Y+8)/8)*64+int(start.X/8), mask, 2)
	}

	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		l, i := n/WalkCells, n%WalkCells
		if reached[n] || w.Cells[l][i]&WalkPassable == 0 {
			continue
		}
		reached[n] = true
		row, col := i/64, i%64
		if row > 0 {
			stack = append(stack, n-64)
		}
		if row < 63 {
			stack = append(stack, n+64)
		}
		if col > 0 {
			stack = append(stack, n-1)
		}
		if col < 63 {
			stack = append(stack, n+1)
		}

		a := r.attrs[l*0x1000+i]
		// the staircases that take Link to the other layer (TileBehavior_NorthSwapLayerStairs and the southern ones
		// of TileHandlerIndoor_3E) lead to the passable cells next to them there:
		if a == 0x1E || a == 0x1F || (a >= 0x3D && a <= 0x3F) {
			seed(i, 1<<(1-l), 3)
		}
		// in rooms with collision on one layer, jumping off a ledge ($28..$2F) of the upper level lands on the lower
		// one; the lower level's cells don't lead back onto the ledge, which is on the other layer:
		if !dual && a >= 0x28 && a <= 0x2F {
			seed(i, 1<<(1-l), 3)
		}
		// the hookshot pulls Link across pits, water and anything else it flies over to a tile it grabs (see
		// isHookshotTarget), up to about 16 cells away (32 frames of 4 pixels):
		if followHookshot && w.Cells[l][i]&WalkStandable != 0 {
			for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				// crossing a ledge (up and down: $28, $29; sideways: $2A, $2B) takes the hookshot over what is
				// beyond, walls too, until it crosses another (ancilla_L in Ancilla1F_Hookshot):
				ledgeLo, ledgeHi := uint8(0x28), uint8(0x29)
				if d[0] == 0 {
					ledgeLo, ledgeHi = 0x2A, 0x2B
				}
				high, onLedge := false, false
				for k := 1; k <= 16; k++ {
					rr, cc := row+d[0]*k, col+d[1]*k
					if rr < 0 || rr >= 64 || cc < 0 || cc >= 64 {
						break
					}
					c := rr*64 + cc
					a := r.attrs[l*0x1000+c]
					if !valid(l, c) {
						a = 0x01
					}
					if a >= ledgeLo && a <= ledgeHi {
						if !onLedge {
							high = !high
						}
						onLedge = true
						continue
					}
					onLedge = false
					if high {
						continue
					}
					if isHookshotTarget(a) {
						if k > 2 {
							seed((row+d[0]*(k-2))*64+col+d[1]*(k-2), 1<<l, 1)
						}
						break
					}
					if w.Cells[l][c]&(WalkPassable|WalkHazard) == 0 {
						// a wall stops it:
						break
					}
				}
			}
		}
	}
	for l := 0; l < 2; l++ {
		for i := range w.Cells[l] {
			if w.Cells[l][i]&WalkStandable != 0 && reached[l*WalkCells+i] {
				w.Cells[l][i] |= WalkReachable
			}
		}
	}
	r.markLower(w, exits, starts, arrivals)
	return w
}

// arrival is a staircase from another room, at tile, and the level Link comes into the room on by it.
type arrival struct {
	tile  uint16
	lower uint8
}

// isInRoomStairs reports whether the attribute is an in-room staircase, which switches the level Link is on.
func isInRoomStairs(a uint8) bool {
	return (a >= 0x1D && a <= 0x1F) || (a >= 0x3D && a <= 0x3F)
}

// markLower sets WalkLower on the cells where Link is on the lower level; see "The lower level".
func (r *roomData) markLower(w *Walk, exits []Exit, starts []StartPos, arrivals []arrival) {
	const nodes = 2 * WalkCells
	stairs := func(n int) bool { return isInRoomStairs(r.attrs[(n/WalkCells)*0x1000+n%WalkCells]) }
	open := func(n int) bool { return w.Cells[n/WalkCells][n%WalkCells]&WalkPassable != 0 && !stairs(n) }

	// the regions: passable cells but staircases, connected on their layer:
	region := make([]int, nodes)
	for n := range region {
		region[n] = -1
	}
	count := 0
	for n := 0; n < nodes; n++ {
		if region[n] >= 0 || !open(n) {
			continue
		}
		stack := []int{n}
		region[n] = count
		for len(stack) > 0 {
			m := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			i := m % WalkCells
			row, col := i/64, i%64
			for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				rr, cc := row+d[0], col+d[1]
				if rr < 0 || rr >= 64 || cc < 0 || cc >= 64 {
					continue
				}
				o := m - i + rr*64 + cc
				if region[o] < 0 && open(o) {
					region[o] = count
					stack = append(stack, o)
				}
			}
		}
		count++
	}
	if count == 0 {
		return
	}

	// the staircases join the regions above and below them, on either layer, with the level switched:
	switched := make([][]int, count)
	nearest := func(l, row, col, dir int) int {
		for k := 1; k <= 4; k++ {
			rr := row + dir*k
			if rr < 0 || rr >= 64 {
				break
			}
			for _, ll := range []int{l, 1 - l} {
				if o := ll*WalkCells + rr*64 + col; open(o) {
					return region[o]
				}
			}
		}
		return -1
	}
	for n := 0; n < nodes; n++ {
		if !stairs(n) || w.Cells[n/WalkCells][n%WalkCells]&WalkPassable == 0 {
			continue
		}
		l, i := n/WalkCells, n%WalkCells
		a, b := nearest(l, i/64, i%64, -1), nearest(l, i/64, i%64, 1)
		if a >= 0 && b >= 0 && a != b {
			switched[a] = append(switched[a], b)
			switched[b] = append(switched[b], a)
		}
	}

	// the level of each region, from the entrances' starts first, then from the doors and exits:
	level := make([]int, count)
	for c := range level {
		level[c] = -1
	}
	assign := func(c, lower int) {
		if c < 0 || level[c] >= 0 {
			return
		}
		level[c] = lower
		queue := []int{c}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			for _, y := range switched[x] {
				if level[y] < 0 {
					level[y] = 1 - level[x]
					queue = append(queue, y)
				}
			}
		}
	}
	// the region of the nearest open cell within 4 cells of pos, on layer l:
	near := func(pos, l int) int {
		r0, c0 := pos/64, pos%64
		for dist := 0; dist <= 4; dist++ {
			for dy := -dist; dy <= dist; dy++ {
				for dx := -dist; dx <= dist; dx++ {
					rr, cc := r0+dy, c0+dx
					if rr < 0 || rr >= 64 || cc < 0 || cc >= 64 {
						continue
					}
					if o := l*WalkCells + rr*64 + cc; open(o) {
						return region[o]
					}
				}
			}
		}
		return -1
	}
	for _, st := range starts {
		pos := int((st.Y+8)/8)*64 + int(st.X/8)
		for l := 0; l < 2; l++ {
			if c := near(pos, l); c >= 0 {
				assign(c, int(st.Lower&1))
				break
			}
		}
	}
	for _, a := range arrivals {
		assign(near(int(a.tile&0x0FFF), int(a.tile>>12&1)), int(a.lower))
		for l := 0; l < 2; l++ {
			assign(near(int(a.tile&0x0FFF), l), int(a.lower))
		}
	}
	for _, x := range exits {
		if x.Kind == ExitTagHoles || x.Kind == ExitEntrance {
			continue
		}
		for l := 0; l < 2; l++ {
			assign(near(int(x.Tile&0x0FFF), l), l)
		}
	}
	for _, d := range r.doors {
		for l := 0; l < 2; l++ {
			assign(near(int(d.pos), l), l)
		}
	}
	// the rest are on the level of their layer:
	for n := 0; n < nodes; n++ {
		if c := region[n]; c >= 0 {
			assign(c, n/WalkCells)
		}
	}

	for n := 0; n < nodes; n++ {
		if c := region[n]; c >= 0 && level[c] == 1 {
			w.Cells[n/WalkCells][n%WalkCells] |= WalkLower
		}
	}
}

// At returns the flags of the cell Link is placed on to be at x, y (room pixels) on layer l.
func (w *Walk) At(x, y uint16, l int) uint8 {
	if w == nil || l < 0 || l > 1 || w.Layers&(1<<l) == 0 || x&7 != 0 || y&7 != 0 || x >= RoomPixels || y+8 >= RoomPixels {
		return 0
	}
	return w.Cells[l][int((y+8)/8)*64+int(x/8)]
}

// isHookshotTarget reports whether the hookshot grabs a tile with the attribute: those whose tile detection sets
// the low bits of tiledetect_misc_tiles (Ancilla1F_Hookshot): $27 (TileBehavior_Hookshottables), what Link lifts,
// chests, crystal pegs, pots and blocks ($70..$7F) and torches.
func isHookshotTarget(a uint8) bool {
	switch {
	case a == 0x27, a >= 0x50 && a <= 0x56, a >= 0x58 && a <= 0x5D, a == 0x63, a == 0x67:
		return true
	case a >= 0x70 && a <= 0x7F, a >= 0xC0 && a <= 0xCF:
		return true
	}
	return false
}
