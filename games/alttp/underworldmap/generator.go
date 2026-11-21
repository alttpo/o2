// Package underworldmap draws a map of every underworld room of A Link to the Past, laid out in a grid of 16 rooms
// per row and 18 rows (rooms $000..$11F), straight from the game's ROM.
//
// The rooms are drawn by the game's own routines, run on an emulated SNES. A room's graphics depend on the entrance
// Link came in by, as the entrance sets the dungeon's main tileset for every room reached from it, and the ROM has no
// table of which entrance each room belongs to. So the map is made by a flood fill from each entrance in turn: the
// entrance's room is drawn, then each room it leads to through a doorway or open walkway on its edges, a bombable
// crack, a staircase, a transport door, a warp tile or a drop through the floor, recursively, with the entrance's
// graphics. A room is drawn once, by the entrance that reaches it with the fewest drops (then the first entrance).
package underworldmap

import (
	"fmt"
	"image"
	"image/color"
	"io"
)

const (
	// GridColumns and GridRows are the size of the map in rooms:
	GridColumns = 0x10
	GridRows    = 0x12
	// RoomCount is the number of rooms in the map, $000..$11F:
	RoomCount = GridColumns * GridRows
	// EntranceCount is the number of underworld entrances:
	EntranceCount = 0x85
)

// Options control the map Generate makes.
type Options struct {
	// Scale divides the size of each room, from RoomPixels: 1 (or 0) for full size, 2 for half, 4 for a quarter, etc.
	Scale int
	// Logger, if set, gets a line for each entrance and room the flood fill visits.
	Logger io.Writer
}

// Room is a room the flood fill reached.
type Room struct {
	Room uint16
	// Entrance is the entrance whose graphics the room was drawn with.
	Entrance uint8
	// From is the room Link came from and Via the way he came, which is ExitEntrance for the entrance's own room.
	From uint16
	Via  Exit
	// Exits are the ways out of the room the flood fill found.
	Exits []Exit
}

// hasEdgeExitTo reports whether the room has a doorway, walkway or bombable crack on its edge into room to.
func (r *Room) hasEdgeExitTo(to uint16) bool {
	for _, x := range r.Exits {
		if x.To == to && (x.Kind == ExitDoorway || x.Kind == ExitWalkway || x.Kind == ExitBombable) {
			return true
		}
	}
	return false
}

// Map is the generated map.
type Map struct {
	Image *image.NRGBA
	// Rooms are the rooms reached, by room number; nil for those never reached.
	Rooms [RoomCount]*Room
	// RoomSize is the size of each room in Image in pixels.
	RoomSize int
}

// Generate draws the map from rom, the contents of an unpatched US 1.0 or JP 1.0 ROM (or a randomizer based on it).
func Generate(rom []byte, opts Options) (m *Map, err error) {
	// strip a copier header:
	if len(rom)&0x7FFF == 0x200 {
		rom = rom[0x200:]
	}
	a, err := addressesFor(rom)
	if err != nil {
		return nil, err
	}

	scale := opts.Scale
	if scale <= 0 {
		scale = 1
	}
	if RoomPixels%scale != 0 {
		return nil, fmt.Errorf("underworldmap: scale %d does not divide %d", scale, RoomPixels)
	}
	logf := func(format string, args ...any) {
		if opts.Logger != nil {
			_, _ = fmt.Fprintf(opts.Logger, format, args...)
		}
	}

	e, err := newEmu(rom, a)
	if err != nil {
		return nil, err
	}

	pitsHurt := make(map[uint16]bool)
	for i := 0; i < a.roomsWithPitDamageCount; i++ {
		pitsHurt[e.rom16(a.roomsWithPitDamage+uint32(i)*2)] = true
	}

	size := RoomPixels / scale
	m = &Map{
		Image:    image.NewNRGBA(image.Rect(0, 0, GridColumns*size, GridRows*size)),
		RoomSize: size,
	}
	// opaque black for the rooms never reached:
	for i := 3; i < len(m.Image.Pix); i += 4 {
		m.Image.Pix[i] = 0xFF
	}
	full := image.NewNRGBA(image.Rect(0, 0, RoomPixels, RoomPixels))

	// the flood fill goes in rounds: in each, every entrance in turn floods through the rooms it reaches without
	// dropping through the floor, and the drops it finds start its flood in the next round. So a room is drawn by the
	// entrance that reaches it with the fewest drops, as some rooms have pits Link can't get to, and a drop through
	// one would take the room below from the entrance it belongs to.
	type visit struct {
		room uint16
		from uint16
		via  Exit
	}
	roots := make([][]visit, EntranceCount)
	for round := 0; ; round++ {
		next := make([][]visit, EntranceCount)
		more := false
		for id := 0; id < EntranceCount; id++ {
			stack := roots[id]
			if round == 0 {
				stack = []visit{{via: Exit{Kind: ExitEntrance}}}
			}
			// skip loading the entrance if none of its drops lead anywhere new:
			pending := false
			for _, v := range stack {
				pending = pending || v.via.Kind == ExitEntrance || m.Rooms[v.room] == nil
			}
			if !pending {
				continue
			}

			var start uint16
			if start, err = e.loadEntrance(uint8(id)); err != nil {
				return nil, fmt.Errorf("underworldmap: entrance $%02x: %w", id, err)
			}
			if round == 0 {
				logf("entrance $%02x: room $%03x\n", id, start)
				stack[0].room, stack[0].from, stack[0].via.To = start, start, start
			} else {
				logf("entrance $%02x: round %d\n", id, round)
			}

			// a depth-first flood fill through the rooms the entrance leads to:
			for len(stack) > 0 {
				v := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if v.room >= RoomCount || m.Rooms[v.room] != nil {
					continue
				}

				// the entrance's own room is already loaded:
				if v.via.Kind != ExitEntrance {
					if err = e.loadRoom(v.room); err != nil {
						return nil, fmt.Errorf("underworldmap: entrance $%02x: room $%03x: %w", id, v.room, err)
					}
				}

				data := e.readRoomData(v.room)
				r := &Room{
					Room:     v.room,
					Entrance: uint8(id),
					From:     v.from,
					Via:      v.via,
					Exits:    data.exits(pitsHurt[v.room]),
				}
				// floor running off the edge of a room is only a way through if the next room has a way back, as
				// some rooms' floor runs under their outer walls:
				if v.via.Kind == ExitWalkway && !r.hasEdgeExitTo(v.from) {
					logf("  room $%03x from $%03x by walkway: no way back\n", v.room, v.from)
					continue
				}
				m.Rooms[v.room] = r
				logf("  room $%03x from $%03x by %s\n", v.room, v.from, v.via.Kind)

				e.readRoomGfx().render(full, 0, 0)
				col, row := int(v.room%GridColumns), int(v.room/GridColumns)
				downscale(m.Image, col*size, row*size, full, scale)

				// push in reverse so the first exit found is visited first:
				for i := len(r.Exits) - 1; i >= 0; i-- {
					x := r.Exits[i]
					logf("    %s\n", x)
					if x.To >= RoomCount || m.Rooms[x.To] != nil {
						continue
					}
					w := visit{room: x.To, from: v.room, via: x}
					if x.Kind.isDrop() {
						next[id] = append(next[id], w)
						more = true
						continue
					}
					stack = append(stack, w)
				}
			}
		}
		if !more {
			break
		}
		// the drops found first are followed first:
		for id := range next {
			for i, j := 0, len(next[id])-1; i < j; i, j = i+1, j-1 {
				next[id][i], next[id][j] = next[id][j], next[id][i]
			}
		}
		roots = next
	}

	return m, nil
}

// downscale draws src into dst at (x0, y0), averaging each scale x scale block of pixels.
func downscale(dst *image.NRGBA, x0, y0 int, src *image.NRGBA, scale int) {
	if scale == 1 {
		for y := 0; y < RoomPixels; y++ {
			copy(dst.Pix[dst.PixOffset(x0, y0+y):], src.Pix[src.PixOffset(0, y):src.PixOffset(RoomPixels, y)])
		}
		return
	}
	n := scale * scale
	for y := 0; y < RoomPixels/scale; y++ {
		for x := 0; x < RoomPixels/scale; x++ {
			var r, g, b int
			for yy := 0; yy < scale; yy++ {
				o := src.PixOffset(x*scale, y*scale+yy)
				for xx := 0; xx < scale; xx++ {
					r += int(src.Pix[o])
					g += int(src.Pix[o+1])
					b += int(src.Pix[o+2])
					o += 4
				}
			}
			dst.SetNRGBA(x0+x, y0+y, color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 0xFF})
		}
	}
}
