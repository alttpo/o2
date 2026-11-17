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
		"JP 1.0": freeROMJP10,
		"US 1.0": freeROMUS10,
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
			}
		})
	}
	if patchRegion.start&0xFFFF != 0x8000 || (patchRegion.end-1)>>16 != patchRegion.start>>16 {
		t.Errorf("patchRegion $%06x..$%06x is not the ROM of one bank", patchRegion.start, patchRegion.end)
	}
	if pak, _ := lorom.BusAddressToPak(patchRegion.end - 1); pak >= patchedROMSize || pak < 0x10_0000 {
		t.Errorf("patchRegion is not in the second MiB of a patched ROM")
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
// but patchRegion, the patch signature, the code it hooks and the header; a 1 MiB ROM is expanded to 2 MiB with the
// ROM mirrored in the second MiB.
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

			if err = NewPatcher(rom).Patch(); err != nil {
				t.Fatal(err)
			}
			if len(rom.Contents) != patchedROMSize {
				t.Fatalf("patched ROM is $%x bytes; expected $%x", len(rom.Contents), patchedROMSize)
			}
			if rom.Header.ROMSize != 0x0B {
				t.Errorf("header ROM size $%02x; expected $0B", rom.Header.ROMSize)
			}
			if hdr, _ := snes.NewROM(fileName, rom.Contents); hdr.Header.ROMSize != 0x0B {
				t.Errorf("written header ROM size $%02x; expected $0B", hdr.Header.ROMSize)
			}

			allowed := []romSpan{
				patchRegion,
				patchSignatureSpan,
				{0x00802F, 0x008034},
				{0x008056, 0x00805A},
				{nmiEarlyPatchAddr, nmiEarlyPatchAddr + uint32(len(nmiEarlyPatchExpected))},
				{nmiHookPatchAddr, nmiHookPatchAddr + uint32(len(nmiHookPatchExpected))},
				{0x00FFB0, 0x010000},
				{hudHookAddr, hudHookAddr + uint32(len(hudHookExpected))},
			}
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
				// the original ROM, or the mirror of it in the added space:
				orig := original[pak%len(original)]
				if rom.Contents[pak] == orig {
					continue
				}
				addr := uint32(pak>>15)<<16 | uint32(pak&0x7FFF) | 0x8000
				if !isAllowed(addr) {
					t.Errorf("patch changed $%06x outside of the patch region: $%02x -> $%02x", addr, orig, rom.Contents[pak])
				}
			}

			// every routine is in patchRegion: the hooks call into it:
			for _, hook := range []uint32{0x00802F, 0x008056, nmiEarlyPatchAddr, nmiHookPatchAddr} {
				pak, _ := lorom.BusAddressToPak(hook)
				target := uint32(rom.Contents[pak+1]) | uint32(rom.Contents[pak+2])<<8 | uint32(rom.Contents[pak+3])<<16
				if !patchRegion.contains(target &^ 0x800000) {
					t.Errorf("hook at $%06x calls $%06x, outside of the patch region", hook, target)
				}
			}

			// a ROM that o2 already patched is refused:
			if err = NewPatcher(rom).Patch(); err == nil {
				t.Error("expected an error patching a patched ROM")
			}
		})
	}
}

// TestPatcher_RefusesUsedPatchRegion checks that a 2 MiB ROM with something in patchRegion is not patched.
func TestPatcher_RefusesUsedPatchRegion(t *testing.T) {
	contents, err := os.ReadFile("alttp-jp-vanilla.sfc")
	if err != nil {
		t.Skipf("ROM not available: %v", err)
	}
	expanded := make([]byte, patchedROMSize)
	copy(expanded, contents)
	pak, _ := lorom.BusAddressToPak(patchRegion.start + 0x123)
	expanded[pak] = 0x42
	rom, err := snes.NewROM("hack.sfc", expanded)
	if err != nil {
		t.Fatal(err)
	}
	if err = NewPatcher(rom).Patch(); err == nil {
		t.Error("expected an error patching a ROM that uses the patch region")
	}
}
