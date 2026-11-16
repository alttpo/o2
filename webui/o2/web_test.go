package main

import (
	"o2/snes"
	"testing"
)

func TestPatchedROMFilename(t *testing.T) {
	tests := []struct {
		name, version, expected string
	}{
		{"zelda3.sfc", "1a2b3c4d", "o2-zelda3-1a2b3c4d.sfc"},
		{"/roms/Zelda no Densetsu.smc", "00ff00ff", "o2-Zelda no Densetsu-00ff00ff.smc"},
		{"zelda3.sfc", "", "o2-zelda3.sfc"},
	}
	for _, tt := range tests {
		if got := patchedROMFilename(&snes.ROM{Name: tt.name, PatchVersion: tt.version}); got != tt.expected {
			t.Errorf("patchedROMFilename(%q, %q) = %q; expected %q", tt.name, tt.version, got, tt.expected)
		}
	}
}
