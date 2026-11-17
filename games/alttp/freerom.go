package alttp

import (
	"fmt"
	"sort"

	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Patch ROM
//
// The patcher writes all of its routines and tables to patchRegion, the first half of bank $3C ($3C:8000..$3C:FFFF),
// in a ROM of 2 MiB. Vanilla US 1.0 and JP 1.0 ROMs are 1 MiB; the patcher expands them to 2 MiB (expandROM), the way
// randomizers do. Randomizers are built from the JP 1.0 ROM and are already 2 MiB.
//
// Bank $3C is in a gap of banks ($38..$3E) that no z3randomizer variant writes to, checked against the sources of
// z3randomizer (master, v32.0), its door randomizer branches (DRMain, DRVolatile, DRUnstable), cassidoxa's fork and
// Archipelago's (main, doors), the `org`s in their asm and the addresses the generators (ALttPEntranceRandomizer,
// ALttPDoorRandomizer, Archipelago) write to, and against randomized ROMs, which have only $00 in those banks. The
// randomizers use banks $20..$24, $27..$29, $2B, $2F..$35 (z3randomizer's code and tables, door randomizer's tables,
// key drop shuffle), $26, $36 and $37 (Enemizer) and $3F (sprite swap, door randomizer's pot shuffle); new code tends to
// be added after those, so $3C has room on either side. The patcher refuses a ROM whose patchRegion holds anything but
// $00 or $FF bytes.
//
// The free sections of the JP 1.0 and US 1.0 ROMs below are where earlier versions of the patch put their routines;
// they are only used to recognize ROMs that those versions patched. They come from the `FREE ROM` comments and their
// `NULL_xxxxxx` (all $FF) and `GARBAGE_xxxxxx` labels in the JP 1.0 and US 1.0 disassemblies (jpdasm, usdasm).
//
// Sections are bus addresses, [start, end).
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

// patchRegion is where the patcher writes its routines and tables; see the comment above:
var patchRegion = romSpan{0x3C8000, 0x3D0000}

// fastROMBank is ORed into the bus addresses of the routines in patchRegion: they run from its mirror in banks $80+,
// which MEMSEL ($420D) makes FastROM.
const fastROMBank = 0x80_0000

// patchedROMSize is the size of a patched ROM: patchRegion is in its second MiB.
const patchedROMSize = 0x20_0000

// expandROM expands a ROM smaller than patchedROMSize to patchedROMSize, with the ROM repeated in the added space as
// the SNES mirrors it, and patchRegion filled with $FF. It sets the ROM size in the header (which it doesn't write).
func expandROM(rom *snes.ROM) error {
	n := len(rom.Contents)
	if n >= patchedROMSize {
		return nil
	}
	if n == 0 || patchedROMSize%n != 0 {
		return fmt.Errorf("cannot expand a ROM of $%x bytes to $%x bytes", n, patchedROMSize)
	}
	contents := make([]byte, patchedROMSize)
	for o := 0; o < patchedROMSize; o += n {
		copy(contents[o:], rom.Contents)
	}
	rom.Contents = contents
	pak, err := lorom.BusAddressToPak(patchRegion.start)
	if err != nil {
		return err
	}
	for i := range rom.Contents[pak : pak+patchRegion.size()] {
		rom.Contents[pak+uint32(i)] = 0xFF
	}
	// 1024 << $0B = 2 MiB:
	rom.Header.ROMSize = 0x0B
	return nil
}

// isPatchRegionEmpty reports whether patchRegion is in the ROM and holds only $00 or $FF bytes.
func isPatchRegionEmpty(rom *snes.ROM) bool {
	pak, err := lorom.BusAddressToPak(patchRegion.start)
	if err != nil || int(pak+patchRegion.size()) > len(rom.Contents) {
		return false
	}
	for _, b := range rom.Contents[pak : pak+patchRegion.size()] {
		if b != 0x00 && b != 0xFF {
			return false
		}
	}
	return true
}

// isFreeROM reports whether addr is where o2's routines are: in patchRegion, or in a free section of the JP 1.0 or US
// 1.0 ROM, where earlier versions of the patch put them.
func isFreeROM(addr uint32) bool {
	addr &^= fastROMBank
	if patchRegion.contains(addr) {
		return true
	}
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

// romBlock is a routine or table the patcher writes to patchRegion.
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
