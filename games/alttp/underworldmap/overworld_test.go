package underworldmap

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestGenerateOverworld draws every area from each vanilla ROM present: the game loads each where the warp's state
// asks, and there is somewhere to stand. Set OWMAP_OUT to a directory to write each area there as a PNG.
func TestGenerateOverworld(t *testing.T) {
	for _, path := range testROMs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			rom, err := os.ReadFile(path)
			if err != nil {
				t.Skip(err)
			}
			out := os.Getenv("OWMAP_OUT")
			var log strings.Builder
			var lock sync.Mutex
			dark := map[uint8]bool{}
			areas, err := GenerateOverworld(rom, OverworldOptions{
				Logger: &log,
				OnArea: func(a *Area, img *image.NRGBA) {
					// an area drawn all in one color didn't load its graphics:
					first := img.Pix[:4]
					same := true
					for i := 0; i < len(img.Pix) && same; i += 4 * 97 {
						same = bytes.Equal(img.Pix[i:i+4], first)
					}
					lock.Lock()
					dark[a.Area] = same
					lock.Unlock()
					if out == "" {
						return
					}
					// the walk cells over the image: blocked darkened, hazards red, reachable standable green and
					// standable cut off yellow:
					shade := image.NewNRGBA(img.Rect)
					copy(shade.Pix, img.Pix)
					n := a.Walk.Size
					for i, c := range a.Walk.Cells {
						var tint [3]int
						switch {
						case c&WalkReachable != 0:
							tint = [3]int{0, 255, 0}
						case c&WalkStandable != 0:
							tint = [3]int{255, 200, 0}
						case c&WalkHazard != 0:
							tint = [3]int{255, 0, 0}
						case c&WalkPassable == 0:
							tint = [3]int{0, 0, 0}
						default:
							continue
						}
						for y := 0; y < 8; y++ {
							for x := 0; x < 8; x++ {
								o := shade.PixOffset(i%n*8+x, i/n*8+y)
								for k := 0; k < 3; k++ {
									shade.Pix[o+k] = uint8((int(shade.Pix[o+k]) + tint[k]) / 2)
								}
							}
						}
					}
					var b bytes.Buffer
					_ = png.Encode(&b, shade)
					_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("%s-area%02x-walk.png", strings.TrimSuffix(filepath.Base(path), ".sfc"), a.Area)), b.Bytes(), 0o644)
					b.Reset()
					_ = png.Encode(&b, img)
					name := fmt.Sprintf("%s-area%02x.png", strings.TrimSuffix(filepath.Base(path), ".sfc"), a.Area)
					_ = os.WriteFile(filepath.Join(out, name), b.Bytes(), 0o644)
				},
			})
			if out != "" {
				_ = os.WriteFile(filepath.Join(out, strings.TrimSuffix(filepath.Base(path), ".sfc")+"-overworld.log"), []byte(log.String()), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(areas) != 80 {
				t.Errorf("expected 80 areas; got %d", len(areas))
			}
			for _, a := range areas {
				if a.Loaded != a.Placed {
					t.Errorf("area $%02x: placed at %+v; the game loaded %+v", a.Area, a.Placed, a.Loaded)
				}
				stand, reach := 0, 0
				for _, c := range a.Walk.Cells {
					if c&WalkStandable != 0 {
						stand++
					}
					if c&WalkReachable != 0 {
						reach++
					}
				}
				if reach == 0 {
					t.Errorf("area $%02x: nowhere reachable to stand (%d standable)", a.Area, stand)
				}
				if dark[a.Area] {
					t.Errorf("area $%02x: drawn in one color", a.Area)
				}
			}
		})
	}
}
