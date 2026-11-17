package alttp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

func TestFreeROM_Tables(t *testing.T) {
	for name, spans := range map[string][]romSpan{
		"JP 1.0":     freeROMJP10,
		"US 1.0":     freeROMUS10,
		"randomizer": freeROMRandomizer,
	} {
		t.Run(name, func(t *testing.T) {
			for i, s := range spans {
				if s.start >= s.end {
					t.Errorf("$%06x..$%06x is empty", s.start, s.end)
				}
				if s.start>>16 >= 0x20 || s.start&0xFFFF < 0x8000 || (s.end-1)>>16 != s.start>>16 {
					t.Errorf("$%06x..$%06x is not within the ROM of one bank", s.start, s.end)
				}
				if i > 0 && spans[i-1].end > s.start {
					t.Errorf("$%06x..$%06x is not after $%06x..$%06x", s.start, s.end, spans[i-1].start, spans[i-1].end)
				}
				if len(subtractROMSpans([]romSpan{s}, []romSpan{patchSignatureSpan})) != 1 {
					t.Errorf("$%06x..$%06x overlaps the patch signature", s.start, s.end)
				}
				if name == "randomizer" && len(subtractROMSpans([]romSpan{s}, randomizerUsedROM)) != 1 {
					t.Errorf("$%06x..$%06x overlaps ROM that z3randomizer uses", s.start, s.end)
				}
			}
		})
	}
}

func TestAllocateROM(t *testing.T) {
	free := []romSpan{{0x018000, 0x018100}, {0x028000, 0x028020}, {0x038000, 0x038040}}
	big := &romBlock{name: "big", size: 0x80}
	small := &romBlock{name: "small", size: 0x10}
	mid := &romBlock{name: "mid", size: 0x30}
	tiny := &romBlock{name: "tiny", size: 0x10}
	if err := allocateROM(free, []*romBlock{small, big, tiny, mid}); err != nil {
		t.Fatal(err)
	}
	// largest first, each into the section with the least room left that fits it:
	for _, c := range []struct {
		b    *romBlock
		addr uint32
	}{
		{big, 0x018000},
		{mid, 0x038000},
		{small, 0x038030},
		{tiny, 0x028000},
	} {
		if c.b.addr != c.addr {
			t.Errorf("%s at $%06x; expected $%06x", c.b.name, c.b.addr, c.addr)
		}
	}

	if err := allocateROM(free, []*romBlock{{name: "huge", size: 0x101}}); err == nil {
		t.Error("expected an error for a block larger than any free section")
	}
}

// TestPatcher_WritesOnlyFreeROM patches the real ROMs that are available and checks that the patch changes nothing
// but the free ROM, the patch signature, the code it hooks and the header.
func TestPatcher_WritesOnlyFreeROM(t *testing.T) {
	files := []string{"alttp-jp-vanilla.sfc", "alttp-us.sfc", "alttpr - NoGlitches-standard-ganon_ry08z8Q5y5.sfc"}
	dr, _ := filepath.Glob("DR_*.sfc")
	files = append(files, dr...)

	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			contents, err := os.ReadFile(fileName)
			if err != nil {
				t.Skipf("ROM not available: %v", err)
			}
			original := append([]byte(nil), contents...)
			rom, err := snes.NewROM(fileName, contents)
			if err != nil {
				t.Fatal(err)
			}
			_, layout := freeROMLayout(rom)

			if err = NewPatcher(rom).Patch(); err != nil {
				t.Fatal(err)
			}

			allowed := append([]romSpan{
				patchSignatureSpan,
				{0x00802F, 0x008034},
				{0x008056, 0x00805A},
				{nmiEarlyPatchAddr, nmiEarlyPatchAddr + uint32(len(nmiEarlyPatchExpected))},
				{nmiHookPatchAddr, nmiHookPatchAddr + uint32(len(nmiHookPatchExpected))},
				{0x00FFB0, 0x010000},
			}, layout...)
			fns := make(map[romFunction]uint32)
			fillRomFunctions(rom, fns)
			stage := fns[fnOverworldHandleOverlays]
			allowed = append(allowed, romSpan{stage, stage + uint32(len(map16StageHookExpected))},
				romSpan{manipStageHookAddr, manipStageHookAddr + uint32(len(manipStageHookExpected))})
			isAllowed := func(addr uint32) bool {
				for _, s := range allowed {
					if s.contains(addr) {
						return true
					}
				}
				return false
			}
			for pak := range rom.Contents {
				if rom.Contents[pak] == original[pak] {
					continue
				}
				addr := uint32(pak>>15)<<16 | uint32(pak&0x7FFF) | 0x8000
				if !isAllowed(addr) {
					t.Errorf("patch changed $%06x outside of free ROM: $%02x -> $%02x", addr, original[pak], rom.Contents[pak])
				}
			}

			// the randomizer's custom palettes:
			if len(original) > 0x10_0000 {
				pak, _ := lorom.BusAddressToPak(0x1BB1E0)
				for i := uint32(0); i < 0x80; i++ {
					if rom.Contents[pak+i] != original[pak+i] {
						t.Fatalf("patch changed the randomizer's PalettesCustom at $%06x", 0x1BB1E0+i)
					}
				}
			}

			// a ROM that o2 already patched is refused:
			if err = NewPatcher(rom).Patch(); err == nil {
				t.Error("expected an error patching a patched ROM")
			}
		})
	}
}
