package underworldmap

// Where Link can stand in an overworld area
//
// As in the rooms (see walk.go), with the overworld's tile attributes: those of the map8 tiles of the area's map16
// tiles (GetOverworldTileType), classified by what TileDetect_ExecuteInner does with them outdoors. The cells have the
// same flags as a room's (WalkStandable, ...), on one layer. Link is put down at least 16 pixels from the area's
// edges, which would take him to the next area. The flood fill starts from where the exits put Link (UnderworldExitData)
// and from the edges, where he comes in from the next areas; ledges are walked both ways, so it reaches more than Link
// could.

// owAttrClasses classifies the overworld's tile attributes.
var owAttrClasses = func() (c [256]attrClass) {
	// TileBehavior_NothingOW and the others that are floor outdoors: thick grass ($04, $40), shallow water ($09), the
	// short water ladder ($0A), ice ($0E, $0F), diggable ground ($48, $4A), the rupee tile ($60), $6C..$6F:
	floor := []int{0x00, 0x05, 0x06, 0x07, 0x14, 0x15, 0x16, 0x17, 0x21, 0x23, 0x24, 0x25, 0x38, 0x39, 0x3A, 0x3B,
		0x3C, 0x41, 0x45, 0x47, 0x49, 0x5E, 0x5F, 0x61, 0x62, 0x64, 0x65, 0x66, 0xA6, 0xA7, 0xBE, 0xBF,
		0x04, 0x09, 0x0A, 0x0E, 0x0F, 0x40, 0x48, 0x4A, 0x60, 0x6C, 0x6D, 0x6E, 0x6F}
	for _, a := range floor {
		c[a] = attrFloorClass
	}
	for a := 0xD0; a <= 0xEF; a++ {
		c[a] = attrFloorClass
	}
	// walked on but not stood on: slopes, ledges, stairs, the corners of $4C..$4F:
	passable := []int{0x0C, 0x1C, 0x10, 0x11, 0x12, 0x13, 0x18, 0x19, 0x1A, 0x1B, 0x22, 0x4C, 0x4D, 0x4E, 0x4F}
	for a := 0x28; a <= 0x37; a++ {
		passable = append(passable, a)
	}
	for _, a := range passable {
		c[a] = attrPassable
	}
	// deep water, holes, warp tiles, conveyors, spikes and the entrances ($8E, $8F), which take Link inside:
	hazard := []int{0x08, 0x0B, 0x0D, 0x1D, 0x1E, 0x1F, 0x20, 0x3D, 0x3E, 0x3F, 0x44, 0x4B, 0x68, 0x69, 0x6A, 0x6B,
		0x8E, 0x8F}
	for _, a := range hazard {
		c[a] = attrHazard
	}
	for a := 0xB0; a <= 0xBD; a++ {
		c[a] = attrHazard
	}
	return
}()

// owWalkedHazard reports whether Link walks onto a hazard: those that only do something once he is on them.
func owWalkedHazard(a uint8) bool {
	switch a {
	case 0x08, 0x0B, 0x20, 0x44, 0x0D, 0x8E, 0x8F:
		return false
	}
	return !(a >= 0xB0 && a <= 0xBD)
}

// AreaWalk is where Link can be in an area: a byte of Walk* flags per 8x8 cell, Size x Size cells, row by row.
type AreaWalk struct {
	Size  int
	Cells []uint8
}

// At returns the flags of the cell Link is placed on to be at x, y (the area's pixels).
func (w *AreaWalk) At(x, y uint16) uint8 {
	if w == nil || x&7 != 0 || y&7 != 0 {
		return 0
	}
	col, row := int(x/8), int((y+8)/8)
	if col >= w.Size || row >= w.Size {
		return 0
	}
	return w.Cells[row*w.Size+col]
}

type areaSeed struct{ x, y uint16 }

// areaAttrs returns the attribute of each 8x8 cell of the area: of the map8 tile of its map16 tile
// (GetOverworldTileType, with the slopes' flip bit).
func areaAttrs(size int, map16 *[0x1000]uint16, defs []uint16, attrs *[0x200]uint8) []uint8 {
	n := size / 8
	out := make([]uint8, n*n)
	for row := 0; row < n; row++ {
		for col := 0; col < n; col++ {
			m := int(map16[(row>>1)*64+col>>1])
			t := defs[(m*4+(row&1)*2+col&1)&0x3FFF]
			a := attrs[t&0x1FF]
			if a >= 0x10 && a < 0x1C {
				a |= uint8(t>>14) & 1
			}
			out[row*n+col] = a
		}
	}
	return out
}

func computeAreaWalk(size int, map16 *[0x1000]uint16, defs []uint16, attrTable *[0x200]uint8, seeds []areaSeed) *AreaWalk {
	n := size / 8
	attrs := areaAttrs(size, map16, defs, attrTable)
	w := &AreaWalk{Size: n, Cells: make([]uint8, n*n)}
	for i, a := range attrs {
		switch owAttrClasses[a] {
		case attrFloorClass, attrPassable:
			w.Cells[i] = WalkPassable
		case attrHazard:
			if owWalkedHazard(a) {
				w.Cells[i] = WalkPassable | WalkHazard
			} else {
				w.Cells[i] = WalkHazard
			}
		}
	}
	// standable: Link's feet on floor, no hazard around them, 16 pixels from the edges:
	for row := 3; row <= n-4; row++ {
		for col := 2; col <= n-4; col++ {
			ok := true
			for dy := 0; dy < 2 && ok; dy++ {
				for dx := 0; dx < 2 && ok; dx++ {
					ok = owAttrClasses[attrs[(row+dy)*n+col+dx]] == attrFloorClass
				}
			}
			for dy := -1; dy <= 2 && ok; dy++ {
				for dx := -1; dx <= 2 && ok; dx++ {
					ok = w.Cells[(row+dy)*n+col+dx]&WalkHazard == 0
				}
			}
			if ok {
				w.Cells[row*n+col] |= WalkStandable
			}
		}
	}

	reached := make([]bool, n*n)
	var stack []int
	seed := func(row, col, radius int) {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				r, c := row+dy, col+dx
				if r >= 0 && r < n && c >= 0 && c < n && w.Cells[r*n+c]&WalkPassable != 0 {
					stack = append(stack, r*n+c)
				}
			}
		}
	}
	// the exits put Link in their doorway, whose tiles may be those of a closed door the game opens as he comes out (the
	// houses' and Hyrule Castle's), from where he walks down: the first passable cells below:
	for _, s := range seeds {
		row, col := int(s.y+8)/8, int(s.x)/8
		for dr := 0; dr < 12 && row+dr < n; dr++ {
			if col+1 < n && (w.Cells[(row+dr)*n+col]|w.Cells[(row+dr)*n+col+1])&WalkPassable != 0 {
				seed(row+dr, col, 2)
				break
			}
		}
	}
	// the edges, where Link comes in from the next areas:
	for i := 0; i < n; i++ {
		for _, rc := range [4][2]int{{0, i}, {n - 1, i}, {i, 0}, {i, n - 1}} {
			if w.Cells[rc[0]*n+rc[1]]&WalkPassable != 0 {
				stack = append(stack, rc[0]*n+rc[1])
			}
		}
	}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[i] || w.Cells[i]&WalkPassable == 0 {
			continue
		}
		reached[i] = true
		row, col := i/n, i%n
		if row > 0 {
			stack = append(stack, i-n)
		}
		if row < n-1 {
			stack = append(stack, i+n)
		}
		if col > 0 {
			stack = append(stack, i-1)
		}
		if col < n-1 {
			stack = append(stack, i+1)
		}
	}
	for i := range w.Cells {
		if w.Cells[i]&WalkStandable != 0 && reached[i] {
			w.Cells[i] |= WalkReachable
		}
	}
	return w
}
