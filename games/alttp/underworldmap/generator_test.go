package underworldmap

import (
	"bufio"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testROMs are the vanilla ROMs the tests generate maps from, where present (they aren't in the repository).
var testROMs = []string{
	"../alttp-jp-vanilla.sfc",
	"../../../../alttp-us.sfc",
	// a VT randomizer, based on JP 1.0:
	"../alttpr - NoGlitches-standard-ganon_ry08z8Q5y5.sfc",
}

// TestGenerate generates the map from each vanilla ROM present. Set UWMAP_OUT to a directory to write each map
// there as a PNG with a log of the flood fill.
func TestGenerate(t *testing.T) {
	for _, path := range testROMs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			rom, err := os.ReadFile(path)
			if err != nil {
				t.Skip(err)
			}

			out := os.Getenv("UWMAP_OUT")
			opts := Options{}
			var log strings.Builder
			if out != "" {
				opts.Logger = &log
			}

			m, err := Generate(rom, opts)
			if err != nil {
				t.Fatal(err)
			}

			reached := 0
			for _, r := range m.Rooms {
				if r != nil {
					reached++
				}
			}
			t.Logf("%d rooms reached", reached)
			// the dungeons' entrance rooms and the rooms behind them:
			for _, room := range []uint16{0x012, 0x061, 0x0C9, 0x084, 0x077, 0x028, 0x04A, 0x098, 0x056, 0x0DB, 0x0D6, 0x00E, 0x00C} {
				if m.Rooms[room] == nil {
					t.Errorf("room $%03x was not reached", room)
				}
			}

			if out == "" {
				return
			}
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if err = os.WriteFile(filepath.Join(out, name+".log"), []byte(log.String()), 0644); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(out, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			w := bufio.NewWriter(f)
			if err = png.Encode(w, m.Image); err != nil {
				t.Fatal(err)
			}
			if err = w.Flush(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
