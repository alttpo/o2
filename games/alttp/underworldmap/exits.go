package underworldmap

import (
	"encoding/binary"
	"fmt"
)

// ExitKind is how Link leaves a room for another.
type ExitKind uint8

const (
	ExitEntrance      ExitKind = iota // the room an entrance starts in
	ExitDoorway                       // a doorway or door on the edge of the room
	ExitWalkway                       // an open edge of the room with no door
	ExitBombable                      // a bombable crack or wall on the edge of the room
	ExitStairs                        // an inter-room staircase
	ExitTransport                     // an east or west transport door ($89 tiles)
	ExitWarp                          // a warp tile
	ExitPit                           // a pit dropping to the room below
	ExitBombableFloor                 // a floor bombed open to drop to the room below
	ExitTagHoles                      // holes a room tag opens in the floor, dropping to the room below
	ExitBlockHole                     // the hole under a large block Link lifts, dropping to the room below
)

// isDrop reports whether the exit drops Link through the floor to the room below.
func (k ExitKind) isDrop() bool {
	switch k {
	case ExitPit, ExitBombableFloor, ExitTagHoles, ExitBlockHole:
		return true
	}
	return false
}

func (k ExitKind) String() string {
	switch k {
	case ExitEntrance:
		return "entrance"
	case ExitDoorway:
		return "doorway"
	case ExitWalkway:
		return "walkway"
	case ExitBombable:
		return "bombable"
	case ExitStairs:
		return "stairs"
	case ExitTransport:
		return "transport"
	case ExitWarp:
		return "warp"
	case ExitPit:
		return "pit"
	case ExitBombableFloor:
		return "bombable floor"
	case ExitTagHoles:
		return "tag holes"
	case ExitBlockHole:
		return "block hole"
	}
	return fmt.Sprintf("ExitKind(%d)", uint8(k))
}

// Exit is a way out of a room into room To.
type Exit struct {
	Kind ExitKind
	To   uint16
	// Tile is the tile attribute map position (0..$1FFF; $1000 and up is BG2) the exit was found at.
	Tile uint16
}

func (x Exit) String() string {
	return fmt.Sprintf("%s to $%03x at $%04x", x.Kind, x.To, x.Tile)
}

type direction uint8

const (
	dirNorth direction = iota
	dirSouth
	dirWest
	dirEast
)

func (d direction) String() string {
	return [...]string{"north", "south", "west", "east"}[d&3]
}

// neighbor returns the room next to room in direction d. Rooms are laid out 16 to a row, and Link can't cross from
// one row or column to the next past the edges of the grid, nor from the first $100 rooms to the next.
func neighbor(room uint16, d direction) (uint16, bool) {
	hi, lo := room&0xFF00, room&0xFF
	switch d {
	case dirNorth:
		return hi | (lo - 0x10), lo&0xF0 != 0
	case dirSouth:
		return hi | (lo + 0x10), lo&0xF0 != 0xF0
	case dirWest:
		return hi | (lo - 1), lo&0x0F != 0
	case dirEast:
		return hi | (lo + 1), lo&0x0F != 0x0F
	}
	return room, false
}

// tile attribute values (see LoadDefaultTileTypes and zelda3's player.c and dungeon.c):
const (
	attrFloor = 0x00
	attrPit   = 0x20
	// a pit in a room whose BG2 property is 4 (see Underworld_LoadHeader):
	attrPitAlt = 0x62
	attrWarp   = 0x4B
)

// doorType is a door's type from the door list in $1980.
type doorType uint8

const (
	doorBreakableWall doorType = 0x28 // bombable crack between rooms
	doorBombableCave  doorType = 0x2A // bombable exit to the overworld
	doorBombable      doorType = 0x2E // bombable crack between rooms
	doorExplodingWall doorType = 0x30 // the big wall blown up with the super bomb or a switch
)

// isExit reports whether a door leads out of the underworld.
func (t doorType) isExit() bool {
	switch {
	case t >= 0x04 && t <= 0x06:
		return true
	case t >= 0x0A && t <= 0x12:
		return true
	case t == doorBombableCave:
		return true
	}
	return false
}

// isStairMask reports whether a door is in front of a staircase (kDoorType_StairMaskLocked0..3), which the stairs
// exits find on their own.
func (t doorType) isStairMask() bool { return t >= 0x20 && t <= 0x26 }

func (t doorType) isBombable() bool {
	return t == doorBreakableWall || t == doorBombable || t == doorExplodingWall
}

// roomData is what is read out of WRAM after a room is loaded and drawn.
type roomData struct {
	room uint16

	// tile attributes of BG1 ($0000..$0FFF) and BG2 ($1000..$1FFF), 64x64 each ($7F2000):
	attrs [0x2000]uint8
	// the door list: types ($1980), tilemap positions ($19A0) and directions ($19C0):
	doors []door
	// dung_hdr_travel_destinations ($7EC000): the pit and warp destination then the four staircase destinations:
	travel [5]uint8
	// dung_hdr_collision ($046C): 0 and 4 for rooms where only BG1 has collision; 1..3 where Link can also be on the
	// lower level, BG2:
	collision uint8
	// dung_hdr_tag ($AE, $AF): the room's two tags, which run its puzzles:
	tags [2]uint8
	// the tilemap positions of the bombable floors, from the replacement tiles the room drew ($0500, $0540):
	bombableFloors []uint16
	// the tilemap positions of the large blocks Link can lift to reveal a hole, also from the replacement tiles:
	blockHoles []uint16
}

// holeTags are the room tags that open holes in the floor when triggered, by loading an overlay with them
// (RoomTag_Holes0, RoomTag_ChestHoles0, RoomTag_Holes2 and the tags that call Dung_TagRoutine_Func2 or
// Dung_TagRoutine_0x22_0x3B in zelda3's dungeon.c):
var holeTags = map[uint8]bool{
	0x21: true, 0x22: true, 0x23: true, 0x24: true,
	0x34: true, 0x35: true, 0x36: true, 0x37: true, 0x39: true, 0x3A: true, 0x3B: true,
}

// walkwayLayers returns the offsets in attrs of the layers whose open edges lead to the next room: BG1, and BG2 in
// rooms with collision on both layers. Other rooms' BG2 attributes are mostly floor, edges included, even where
// there is nothing on BG2.
func (r *roomData) walkwayLayers() []uint16 {
	if r.collision >= 1 && r.collision <= 3 {
		return []uint16{0x0000, 0x1000}
	}
	return []uint16{0x0000}
}

type door struct {
	typ doorType
	// tile position in the 64x64 tilemap:
	pos uint16
	dir direction
}

func (d door) row() uint16 { return d.pos >> 6 & 0x3F }
func (d door) col() uint16 { return d.pos & 0x3F }

// onEdge reports whether the door is in the outer wall of the room on its own side, so leads to the next room that
// way rather than to the other quadrant of this room.
func (d door) onEdge() bool {
	const margin = 8
	switch d.dir {
	case dirNorth:
		return d.row() <= margin
	case dirSouth:
		return d.row() >= 0x3F-margin
	case dirWest:
		return d.col() <= margin
	case dirEast:
		return d.col() >= 0x3F-margin
	}
	return false
}

func (e *emu) readRoomData(room uint16) *roomData {
	w := e.sys.WRAM[:]
	r := &roomData{room: room}
	copy(r.attrs[:], w[0x12000:0x14000])
	for m := uint32(0); m < 16; m++ {
		pos := binary.LittleEndian.Uint16(w[0x19A0+m*2:])
		if pos == 0 {
			break
		}
		r.doors = append(r.doors, door{
			typ: doorType(binary.LittleEndian.Uint16(w[0x1980+m*2:])),
			pos: pos >> 1 & 0x0FFF,
			dir: direction(binary.LittleEndian.Uint16(w[0x19C0+m*2:]) & 3),
		})
	}
	copy(r.travel[:], w[0xC000:0xC005])
	r.collision = w[0x046C]
	r.tags = [2]uint8{w[0xAE], w[0xAF]}
	// dung_misc_objs_index ($042C) counts the replacement tiles; RoomDraw_BombableFloor draws each bombable floor
	// as four with states $3030..$3333, and the large pick up block (object $2C) with $2020..$2323, which reveals
	// a hole when lifted (ThievesAttic_DrawLightenedHole):
	n := uint32(binary.LittleEndian.Uint16(w[0x042C:]) / 2)
	for i := uint32(0); i < n && i < 0x20; i++ {
		pos := binary.LittleEndian.Uint16(w[0x0540+i*2:]) >> 1
		switch binary.LittleEndian.Uint16(w[0x0500+i*2:]) {
		case 0x3030, 0x3131, 0x3232, 0x3333:
			r.bombableFloors = append(r.bombableFloors, pos)
		case 0x2020, 0x2121, 0x2222, 0x2323:
			r.blockHoles = append(r.blockHoles, pos)
		}
	}
	return r
}

// exits finds the ways out of the room into other rooms. pitsHurt is whether the room's pits hurt Link instead of
// dropping him to the room below.
func (r *roomData) exits(pitsHurt bool) (exits []Exit) {
	seen := make(map[Exit]bool)
	add := func(kind ExitKind, to uint16, tile uint16) {
		x := Exit{Kind: kind, To: to}
		if seen[x] {
			return
		}
		seen[x] = true
		x.Tile = tile
		exits = append(exits, x)
	}
	// the destinations in the room header are in the same $100 rooms as the room:
	dest := func(i int) (uint16, bool) {
		if r.travel[i] == 0 {
			return 0, false
		}
		return r.room&0xFF00 | uint16(r.travel[i]), true
	}

	// doors on the edges of the room into the rooms next to it:
	for _, d := range r.doors {
		if d.typ.isExit() || d.typ.isStairMask() || !d.onEdge() {
			continue
		}
		to, ok := neighbor(r.room, d.dir)
		if !ok {
			continue
		}
		kind := ExitDoorway
		if d.typ.isBombable() {
			kind = ExitBombable
		}
		add(kind, to, d.pos)
	}

	// open edges of the room without doors, where floor or doorway tiles run off the edge:
	for _, layer := range r.walkwayLayers() {
		// not the corners, which are on two edges:
		for i := uint16(2); i < 0x3E; i++ {
			for _, edge := range [...]struct {
				d    direction
				pos  uint16
				door uint8 // the doorway attribute for this edge
			}{
				{dirNorth, 1<<6 | i, 0x80},
				{dirSouth, 0x3E<<6 | i, 0x80},
				{dirWest, i<<6 | 1, 0x81},
				{dirEast, i<<6 | 0x3E, 0x81},
			} {
				v := r.attrs[layer|edge.pos]
				if !(v == attrFloor || v&^0x04 == edge.door) {
					continue
				}
				if to, ok := neighbor(r.room, edge.d); ok {
					add(ExitWalkway, to, layer|edge.pos)
				}
			}
		}
	}

	// staircases, transport doors, warp tiles and pits, on either layer as Link may be on either level:
	{
		for t := uint16(0); t < 0x2000; t++ {
			v := r.attrs[t]
			switch {
			case v&0xF8 == 0x30:
				// a staircase's destination is picked by the attribute under the tile Link steps on
				// (Dungeon_DetectStaircase):
				if to, ok := dest(int(v&3) + 1); ok {
					add(ExitStairs, to, t)
				}
			case (v == 0x89 || v == 0x99) && (t&0x3F < 4 || t&0x3F >= 0x3C):
				// transport doors at the west and east edges go to the rooms in the third and fourth staircase
				// destinations (Dungeon_StartInterRoomTrans_Left and _Right, which check the attribute under Link
				// masked with $CF; other attributes matching that mask, like the $B9 of the dark maze objects, are
				// not doors):
				col := t & 0x3F
				if col < 0x20 {
					if to, ok := dest(3); ok {
						add(ExitTransport, to, t)
					}
				} else if to, ok := dest(4); ok {
					add(ExitTransport, to, t)
				}
			case v == attrWarp:
				if to, ok := dest(0); ok {
					add(ExitWarp, to, t)
				}
			case v == attrPit || v == attrPitAlt:
				if pitsHurt {
					continue
				}
				if to, ok := dest(0); ok {
					add(ExitPit, to, t)
				}
			}
		}
	}

	// floors bombed open and holes opened by room tags drop to the room below too:
	if to, ok := dest(0); ok && !pitsHurt {
		for _, pos := range r.bombableFloors {
			add(ExitBombableFloor, to, pos)
		}
		for _, pos := range r.blockHoles {
			add(ExitBlockHole, to, pos)
		}
		for _, tag := range r.tags {
			if holeTags[tag] {
				add(ExitTagHoles, to, 0)
			}
		}
	}
	return
}
