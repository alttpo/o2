package worldmap

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestGenerate draws both maps from the ROMs found next to the repository, if any, and writes them to
// $WORLDMAP_OUT when it is set.
func TestGenerate(t *testing.T) {
	roms := []string{"../../../../alttp-us.sfc", "../../../../alttp-jp.sfc"}
	found := false
	for _, name := range roms {
		rom, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		found = true
		for _, dark := range []bool{false, true} {
			img, err := Generate(rom, dark)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if img.Bounds().Dx() != Width || img.Bounds().Dy() != Height {
				t.Fatalf("%s: size %v", name, img.Bounds())
			}
			if out := os.Getenv("WORLDMAP_OUT"); out != "" {
				suffix := "-light.png"
				if dark {
					suffix = "-dark.png"
				}
				f, err := os.Create(filepath.Join(out, filepath.Base(name)+suffix))
				if err != nil {
					t.Fatal(err)
				}
				_ = png.Encode(f, img)
				_ = f.Close()
			}
		}
	}
	if !found {
		t.Skip("no ROMs found")
	}
}
