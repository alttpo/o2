package underworldmap

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWalk checks where Link can stand in the rooms of each vanilla ROM present: next to where every entrance puts
// him, and somewhere in every dungeon room the tests know.
func TestWalk(t *testing.T) {
	for _, path := range testROMs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			rom, err := os.ReadFile(path)
			if err != nil {
				t.Skip(err)
			}
			m, err := Generate(rom, Options{NoImage: true})
			if err != nil {
				t.Fatal(err)
			}

			for id, ent := range m.Entrances {
				if ent == nil || ent.Room >= RoomCount {
					continue
				}
				r := m.Rooms[ent.Room]
				if r == nil || r.Walk == nil {
					t.Errorf("entrance $%02x: room $%03x has no walk", id, ent.Room)
					continue
				}
				l := int(ent.Start.Layer) & 1
				if r.Walk.Layers&(1<<l) == 0 {
					l ^= 1
				}
				// a reachable cell within 5 cells of the start (entrances put Link in doorways, which are not floor),
				// unless the start is on something the room as drawn blocks (some entrances that drop into a room
				// land where the room, drawn from another entrance, has something):
				found := false
				col, row := int(ent.Start.X/8), int((ent.Start.Y+8)/8)
				if row < 64 && r.Walk.Cells[l][row*64+col]&WalkPassable == 0 {
					continue
				}
				for dy := -5; dy <= 5 && !found; dy++ {
					for dx := -5; dx <= 5 && !found; dx++ {
						rr, cc := row+dy, col+dx
						if rr >= 0 && rr < 64 && cc >= 0 && cc < 64 {
							found = r.Walk.Cells[l][rr*64+cc]&WalkReachable != 0
						}
					}
				}
				if !found {
					t.Errorf("entrance $%02x: no reachable cell near the start ($%03x, $%03x) in room $%03x", id, ent.Start.X, ent.Start.Y, ent.Room)
				}
			}

			for _, room := range []uint16{0x012, 0x061, 0x0C9, 0x084, 0x077, 0x028, 0x04A, 0x098, 0x056, 0x0DB, 0x0D6, 0x00E, 0x00C} {
				r := m.Rooms[room]
				if r == nil || r.Walk == nil {
					t.Errorf("room $%03x: no walk", room)
					continue
				}
				n := 0
				for l := 0; l < 2; l++ {
					for _, c := range r.Walk.Cells[l] {
						if c&WalkReachable != 0 {
							n++
						}
					}
				}
				if n < 50 {
					t.Errorf("room $%03x: only %d reachable cells", room, n)
				}
			}

			// Link is on BG2 in the passage north of the Sanctuary and Tower of Hera's entrance, and on BG1 in Hyrule
			// Castle's entrance hall:
			// and on both in room $015, the upper level with the doorways on BG1, the lower one on BG2:
			for room, layers := range map[uint16]uint8{0x002: 2, 0x077: 2, 0x061: 1, 0x015: 3} {
				if r := m.Rooms[room]; r != nil && r.Walk.Layers != layers {
					t.Errorf("room $%03x: expected layers %d; got %d", room, layers, r.Walk.Layers)
				}
			}
			// the pull switches of the passage north of the Sanctuary block Link on BG2:
			if r := m.Rooms[0x002]; r != nil && r.Walk.Cells[1][0x0C*64+0x28]&WalkPassable != 0 {
				t.Errorf("room $002: expected a pull switch")
			}

			// the upper level of room $015 by its north west doorway is on BG1, and reachable:
			if r := m.Rooms[0x015]; r != nil && r.Walk.Cells[0][0x08*64+0x0E]&WalkReachable == 0 {
				t.Errorf("room $015: expected the upper level reachable on BG1")
			}

			// Swamp Palace's room $034: BG1's collision everywhere, the water channel on the lower level and the walkway
			// around it on the upper level (its in-room staircases only switch $0476):
			if r := m.Rooms[0x034]; r != nil {
				if c := r.Walk.Cells[0][46*64+15]; c&WalkStandable == 0 || c&WalkLower == 0 {
					t.Errorf("room $034: expected the water channel on the lower level; got $%02x", c)
				}
				if c := r.Walk.Cells[0][40*64+10]; c&WalkStandable == 0 || c&WalkLower != 0 {
					t.Errorf("room $034: expected the walkway on the upper level; got $%02x", c)
				}
			}

			// the walls of the Hyrule Castle entrance hall's top left corner block Link:
			if r := m.Rooms[0x061]; r != nil && r.Walk.Cells[0][1*64+1]&WalkPassable != 0 {
				t.Errorf("room $061: expected a wall in the corner")
			}
		})
	}
}

// TestStream checks a stream gets every room the map has, with their images.
func TestStream(t *testing.T) {
	rom, err := os.ReadFile(testROMs[0])
	if err != nil {
		t.Skip(err)
	}
	s := Shared(rom)
	if Shared(rom) != s {
		t.Fatal("expected the same stream for the same ROM")
	}
	n := 0
	deadline := time.After(2 * time.Minute)
	for {
		rooms, done, err, changed := s.Progress(n)
		for _, room := range rooms {
			r := s.Room(room)
			if r == nil || len(r.PNG) == 0 || len(r.Thumb) == 0 || r.Walk == nil || r.EntranceState == nil {
				t.Fatalf("room $%03x is incomplete", room)
			}
		}
		n += len(rooms)
		if done {
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("the stream took too long")
		}
	}
	if n < 250 {
		t.Errorf("only %d rooms streamed", n)
	}
}

// TestWalkHookshot checks the hookshot gets Link to more of Swamp Palace's room $036: the islands with the pots on
// its left and right, over the islands' walls between their ledges.
func TestWalkHookshot(t *testing.T) {
	rom, err := os.ReadFile(testROMs[0])
	if err != nil {
		t.Skip(err)
	}
	island := false
	count := func() int {
		m, err := Generate(rom, Options{NoImage: true})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range m.Rooms[0x036].Walk.Cells[0] {
			if c&WalkReachable != 0 {
				n++
			}
		}
		island = m.Rooms[0x036].Walk.Cells[0][0x20*64+0x0A]&WalkReachable != 0 &&
			m.Rooms[0x036].Walk.Cells[0][0x20*64+0x34]&WalkReachable != 0
		return n
	}
	defer func() { followHookshot = true }()
	followHookshot = false
	without := count()
	followHookshot = true
	with := count()
	t.Logf("room $036: %d reachable cells without the hookshot, %d with", without, with)
	if with <= without || !island {
		t.Errorf("expected the hookshot to reach more of room $036, the islands with the pots too")
	}
}
