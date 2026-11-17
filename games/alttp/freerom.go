package alttp

import (
	"fmt"
	"log"
	"sort"

	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Free ROM
//
// The patcher writes its routines to sections of the ROM that the game never reads. They come from the `FREE ROM`
// comments and their `NULL_xxxxxx` (all $FF) and `GARBAGE_xxxxxx` labels in the JP 1.0 and US 1.0 disassemblies
// (jpdasm, usdasm), with these corrections, checked against the ROMs themselves:
//
//   - the GARBAGE sections start with a few leftover bytes (e.g. `$C0, $00, $00, $00, $00, $01, $FF, $00, $00` at
//     $1B:B1D7); only the $FF bytes after them are listed.
//   - usdasm's `FREE ROM: 0x16` at $03:B3F0, $03:E7F0, $04:B3F0 and $04:E7F0 are 16 bytes, and `FREE ROM: 0x19B` at
//     $03:EB05 is $9B bytes, as the US ROM has them.
//   - the room headers of the unused rooms $136..$13F ($04:FCCF in JP, $04:FFEF in US) are left out, since ROM hacks
//     use those rooms.
//
// Every section lies within one bank. Sections are bus addresses in banks $00..$1F, [start, end).
type romSpan struct {
	start, end uint32
}

func (s romSpan) size() uint32 { return s.end - s.start }

func (s romSpan) contains(addr uint32) bool { return addr >= s.start && addr < s.end }

// patchSignatureSpan is where the patch signature is, at the same address for all ROMs so that o2 can read it from
// any patched ROM; the last 16 bytes of a section that is free in all of them. The tables below leave it out:
var patchSignatureSpan = romSpan{patchSignatureROMAddr, patchSignatureROMAddr + patchSignatureSize}

// freeROMJP10 are the free sections of the JP 1.0 ROM:
var freeROMJP10 = []romSpan{
	{0x0089C2, 0x0089E0}, {0x0098AB, 0x0098C0}, {0x00CF46, 0x00CFC0}, {0x00E892, 0x00E8B0}, {0x00F7E1, 0x00F800},
	{0x00FFB7, 0x00FFC0},
	{0x01B53C, 0x01B560}, {0x01FEAA, 0x01FEB0}, {0x01FFFD, 0x020000},
	{0x02C23A, 0x02C270}, {0x02FCD2, 0x02FD00}, {0x02FFC7, 0x030000},
	{0x03EB8F, 0x03EB90}, {0x03FFE5, 0x040000},
	{0x04E96F, 0x04E9A0}, {0x04FCDD, 0x04FCE0}, {0x04FFEC, 0x050000},
	{0x05FFFF, 0x060000},
	{0x06FFFE, 0x070000},
	{0x07F877, 0x07F890}, {0x07FFF4, 0x080000},
	{0x08FFEC, 0x090000},
	{0x09AE2A, 0x09AE40}, {0x09F253, 0x09F270}, {0x09F7BD, 0x09F7DE}, {0x09FCAD, 0x09FCAE},
	{0x0AB72A, 0x0AB750}, {0x0AE0B9, 0x0AE0BF}, {0x0AFCCA, 0x0AFD00}, {0x0AFE38, 0x0AFE70}, {0x0AFFFB, 0x0B0000},
	{0x0BFE49, 0x0BFE70}, {0x0BFFF6, 0x0C0000},
	{0x0CC0AB, 0x0CC100}, {0x0CFD67, 0x0CFDD0}, {0x0CFFF6, 0x0D0000},
	{0x0DAFDD, 0x0DB080}, {0x0DDB07, 0x0DDB40}, {0x0DFFE1, 0x0E0000},
	{0x0EC244, 0x0EC250}, {0x0EEDFB, 0x0EEE10}, {0x0EFD7E, 0x0EFFA0}, {0x0EFFE5, 0x0F0000},
	{0x0FF4F0, 0x0FF780}, {0x0FFF94, 0x100000},
	{0x18BAE1, 0x18BC00}, {0x18BFF1, 0x18C000},
	{0x1AD340, 0x1AD380}, {0x1AF430, 0x1AF500}, {0x1AFFFE, 0x1B0000},
	{0x1BB1E0, 0x1BB800}, {0x1BD1D8, 0x1BD208}, {0x1BF108, 0x1BF110}, {0x1BFFB0, 0x1C0000},
	{0x1CF3D5, 0x1CF500}, {0x1CFD8E, 0x1D0000},
	{0x1DFFF8, 0x1E0000},
	{0x1EFFD2, 0x1F0000},
	{0x1FFF4E, 0x200000},
}

// freeROMUS10 are the free sections of the US 1.0 ROM:
var freeROMUS10 = []romSpan{
	{0x0089C2, 0x0089E0}, {0x0098AB, 0x0098C0}, {0x00CF46, 0x00CF80}, {0x00E852, 0x00E880}, {0x00F7E0, 0x00F800},
	{0x00FFB7, 0x00FFC0},
	{0x01B53C, 0x01B560}, {0x01FEAC, 0x01FEB0}, {0x01FFFD, 0x020000},
	{0x02C4C0, 0x02C500}, {0x02FF6E, 0x030000},
	{0x03B3F0, 0x03B400}, {0x03E7F0, 0x03E800}, {0x03EB05, 0x03EBA0}, {0x03FFF5, 0x040000},
	{0x04B3F0, 0x04B400}, {0x04E7F0, 0x04E800}, {0x04EC1C, 0x04ECC0},
	{0x05FFFF, 0x060000},
	{0x06FFF8, 0x070000},
	{0x07F89D, 0x080000},
	{0x08FFDA, 0x090000},
	{0x09AE3E, 0x09AE40}, {0x09F253, 0x09F270}, {0x09F7C0, 0x09F7DE}, {0x09FCAD, 0x09FCAE},
	{0x0AB72A, 0x0AB730}, {0x0AE0A7, 0x0AE0B0}, {0x0AFCBA, 0x0AFCE0}, {0x0AFE18, 0x0AFE20}, {0x0AFE65, 0x0AFE70},
	{0x0AFFFB, 0x0B0000},
	{0x0BFE5E, 0x0BFE70}, {0x0BFFF6, 0x0C0000},
	{0x0CC10C, 0x0CC120}, {0x0CFFB1, 0x0D0000},
	{0x0DDB17, 0x0DDB40}, {0x0DFFC1, 0x0E0000},
	{0x0E9814, 0x0E9820}, {0x0EC41C, 0x0EC440}, {0x0ED44B, 0x0ED460}, {0x0ED91A, 0x0ED940}, {0x0EDD31, 0x0EDD40},
	{0x0EDF2F, 0x0EDF40}, {0x0EEE21, 0x0EF400}, {0x0EFB64, 0x0F0000},
	{0x0FF520, 0x0FF540}, {0x0FFE72, 0x100000},
	{0x18BFB5, 0x18C000},
	{0x1AD340, 0x1AD380}, {0x1AF430, 0x1AF4FD}, {0x1AFFFE, 0x1B0000},
	{0x1BB1E0, 0x1BB800}, {0x1BD1D8, 0x1BD208}, {0x1BF108, 0x1BF110}, {0x1BFFC0, 0x1C0000},
	{0x1CFF2A, 0x1D0000},
	{0x1DFFF8, 0x1E0000},
	{0x1EFFDE, 0x1F0000},
	{0x1FFF4E, 0x200000},
}

// randomizerUsedROM are the free sections of the JP 1.0 ROM that z3randomizer (the master, v32.0 and DRVolatile
// branches, which alttpr, ALttPEntranceRandomizer and ALttPDoorRandomizer build their base ROMs from) writes to. Each
// one starts at an `org` and runs to the end of the free section, so that a randomizer whose code there grows does
// not run into o2's:
var randomizerUsedROM = []romSpan{
	{0x0089C2, 0x0089E0}, // hooks.asm
	{0x0098AB, 0x0098C0}, // hooks.asm
	{0x00FFB7, 0x00FFC0}, // hooks.asm
	{0x02FFC7, 0x030000}, // hooks.asm
	{0x04E96F, 0x04E9A0}, // hooks.asm
	{0x07FFF4, 0x080000}, // hooks.asm
	{0x09F7B2, 0x09F7DE}, // hooks.asm at $09F7BD; doorrando/drhooks.asm at $09F7B2
	{0x0DFFE1, 0x0E0000}, // hooks.asm
	{0x1BB1D7, 0x1BB880}, // custompalettes.asm (PalettesCustom) at $1BB1E0, `warnpc $9BB880`
	{0x1FFFF8, 0x200000}, // the ROM timestamp
}

// freeROMRandomizer are the free sections of a randomized ROM: JP 1.0's without the ones z3randomizer uses.
var freeROMRandomizer = subtractROMSpans(freeROMJP10, randomizerUsedROM)

// subtractROMSpans returns the parts of spans that are not in any of used.
func subtractROMSpans(spans, used []romSpan) (free []romSpan) {
	for _, s := range spans {
		parts := []romSpan{s}
		for _, u := range used {
			var next []romSpan
			for _, p := range parts {
				if u.end <= p.start || u.start >= p.end {
					next = append(next, p)
					continue
				}
				if u.start > p.start {
					next = append(next, romSpan{p.start, u.start})
				}
				if u.end < p.end {
					next = append(next, romSpan{u.end, p.end})
				}
			}
			parts = next
		}
		free = append(free, parts...)
	}
	return
}

// freeROMLayout picks the free sections for the ROM. Randomizers are built from the JP 1.0 ROM and expand it to 2 MiB
// or more.
func freeROMLayout(rom *snes.ROM) (name string, spans []romSpan) {
	if rom.Header.DestinationCode == snes.RegionNorthAmerica && rom.Header.MaskROMVersion == 0 {
		return "US 1.0", freeROMUS10
	}
	if len(rom.Contents) > 0x10_0000 {
		return "JP 1.0 randomizer", freeROMRandomizer
	}
	return "JP 1.0", freeROMJP10
}

// isFreeROM reports whether addr is in a free section of any supported ROM, which is where o2's routines are.
func isFreeROM(addr uint32) bool {
	for _, spans := range [][]romSpan{freeROMJP10, freeROMUS10} {
		for _, s := range spans {
			if s.contains(addr) {
				return true
			}
		}
	}
	return false
}

// isROMSpanFree reports whether all the bytes of s are $FF in the ROM.
func isROMSpanFree(rom *snes.ROM, s romSpan) bool {
	pak, err := lorom.BusAddressToPak(s.start)
	if err != nil || int(pak+s.size()) > len(rom.Contents) {
		return false
	}
	for _, b := range rom.Contents[pak : pak+s.size()] {
		if b != 0xFF {
			return false
		}
	}
	return true
}

// verifiedFreeROM returns the spans whose bytes are all $FF in the ROM. Others are left out, since an unknown ROM
// hack may have put something there.
func verifiedFreeROM(rom *snes.ROM, spans []romSpan) (free []romSpan) {
	if rom.Header.HeaderVersion() >= 2 {
		// the expanded header at $00FFB0..$00FFBF, which ROM.WriteHeader writes:
		spans = subtractROMSpans(spans, []romSpan{{0x00FFB0, 0x00FFC0}})
	}
	free = make([]romSpan, 0, len(spans))
	for _, s := range spans {
		if !isROMSpanFree(rom, s) {
			log.Printf("alttp: patch: free ROM $%06x..$%06x is in use; not using it\n", s.start, s.end-1)
			continue
		}
		free = append(free, s)
	}
	return
}

// romBlock is a routine or table the patcher writes to free ROM.
type romBlock struct {
	name string
	size uint32
	// addr is assigned by allocateROM:
	addr uint32
}

// allocateROM assigns each block an address in free, best fit decreasing: from the largest block to the smallest,
// each goes into the free space with the least room left that it fits in (the lowest address of those that leave the
// same room), after the blocks already placed there.
func allocateROM(free []romSpan, blocks []*romBlock) error {
	room := append([]romSpan(nil), free...)

	order := append([]*romBlock(nil), blocks...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].size > order[j].size })

	for _, b := range order {
		best := -1
		for i, s := range room {
			if s.size() < b.size {
				continue
			}
			if best < 0 || s.size() < room[best].size() || (s.size() == room[best].size() && s.start < room[best].start) {
				best = i
			}
		}
		if best < 0 {
			return fmt.Errorf("no free ROM left for %s ($%x bytes)", b.name, b.size)
		}
		b.addr = room[best].start
		room[best].start += b.size
	}
	return nil
}
