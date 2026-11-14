package alttp

import (
	"fmt"
	"github.com/alttpo/snes/mapping/lorom"
	"log"
	"o2/snes"
)

// Sword and shield graphics
//
// Unlike Link's body, the sword and shield graphics are not DMA'd straight from ROM. When the game loads or the
// player's sword or shield changes, DecompressSwordGraphics and DecompressShieldGraphics:
//
//  1. decompress sprite graphics sheets $5F and $5E (3bpp) into WRAM $7F4600 and $7F4000;
//  2. convert the tiles for the current sword ($7EF359) or shield ($7EF35A) level from 3bpp to 4bpp into WRAM
//     $7E9000 (sword, 24 tiles) and $7E9300 (shield, 12 tiles), using palette colors 8-15 (Do3bppToWRAM4bpp_RightPal).
//
// Each frame the NMI routine DMAs the tiles for the sword and shield poses from $7E:[$0AC0], [$0AC2] (sword) and
// [$0AC4], [$0AC6] (shield) into VRAM chrs $05-$08 and $15-$18.
//
// o2 performs the same decompression and conversion from its in-memory copy of the ROM. The tiles are sent to alttpo
// clients as unique tiles $380-$3A3, the indices alttpo reserves for them: $380 + (WRAM address - $9000) / 32.

const (
	swordShieldTileFirst = linkUniqTileCount // $380
	swordTileCount       = 0x18              // $7E9000..$7E92FF
	shieldTileCount      = 0x0C              // $7E9300..$7E947F
	swordShieldTileCount = swordTileCount + shieldTileCount
	swordShieldTileEnd   = swordShieldTileFirst + swordShieldTileCount // $3A4

	swordShieldWRAMStart = 0x9000
	swordShieldWRAMEnd   = swordShieldWRAMStart + swordShieldTileCount<<5

	spriteSheetSwordShieldLow  = 0x5E
	spriteSheetSwordShieldHigh = 0x5F
)

// romSwordShieldAddresses locates the routines' data tables in ROM by the instructions that reference them.
type romSwordShieldAddresses struct {
	// `LDA.w GFXSheetPointers_sprite_bank,Y` at the start of Decompress_sprite_arbitrary:
	spriteSheetLookup uint32
	// `ADC.w .offset,Y` in DecompressSwordGraphics and DecompressShieldGraphics:
	swordOffsetADC  uint32
	shieldOffsetADC uint32
}

var (
	romSwordShieldUS10 = romSwordShieldAddresses{spriteSheetLookup: 0x00E772, swordOffsetADC: 0x00D2E2, shieldOffsetADC: 0x00D31F}
	romSwordShieldJP10 = romSwordShieldAddresses{spriteSheetLookup: 0x00E7B2, swordOffsetADC: 0x00D322, shieldOffsetADC: 0x00D35F}
)

type swordShieldGfx struct {
	ok bool

	// ROM addresses (bank $00) of the tables:
	sheetBank, sheetHigh, sheetLow uint16
	swordOffsets, shieldOffsets    uint16

	// levels the tiles were last built for; 0xFF = not built:
	swordLevel, shieldLevel uint8
	swordOK, shieldOK       bool

	// 4bpp tiles as they appear in WRAM $7E9000..$7E947F:
	tiles [swordShieldTileCount << 5]byte
}

func (g *Game) romU8(busAddr uint32) (uint8, bool) {
	pak, err := lorom.BusAddressToPak(busAddr)
	if err != nil || int(pak) >= len(g.rom.Contents) {
		return 0, false
	}
	return g.rom.Contents[pak], true
}

func (g *Game) romU16(busAddr uint32) (uint16, bool) {
	lo, ok1 := g.romU8(busAddr)
	hi, ok2 := g.romU8(busAddr + 1)
	return uint16(lo) | uint16(hi)<<8, ok1 && ok2
}

func (g *Game) initSwordShieldGfx() {
	s := &g.swordShield
	*s = swordShieldGfx{swordLevel: 0xFF, shieldLevel: 0xFF}

	addrs := romSwordShieldJP10
	if g.rom.Header.DestinationCode == snes.RegionNorthAmerica && g.rom.Header.MaskROMVersion == 0 {
		addrs = romSwordShieldUS10
	}

	// expect LDA abs,Y ($B9) / STA dp ($85) $CA, $C9, $C8:
	operand := func(addr uint32, opcode uint8) (uint16, bool) {
		if op, ok := g.romU8(addr); !ok || op != opcode {
			return 0, false
		}
		return g.romU16(addr + 1)
	}
	var ok [5]bool
	s.sheetBank, ok[0] = operand(addrs.spriteSheetLookup, 0xB9)
	s.sheetHigh, ok[1] = operand(addrs.spriteSheetLookup+5, 0xB9)
	s.sheetLow, ok[2] = operand(addrs.spriteSheetLookup+10, 0xB9)
	// expect ADC abs,Y ($79):
	s.swordOffsets, ok[3] = operand(addrs.swordOffsetADC, 0x79)
	s.shieldOffsets, ok[4] = operand(addrs.shieldOffsetADC, 0x79)
	for _, b := range ok {
		if !b {
			log.Printf("alttp: sprites: sword/shield graphics routines not found in ROM; not sending sword/shield sprites\n")
			return
		}
	}
	s.ok = true
}

// decompressSpriteSheet implements the game's Decompress routine for a sprite graphics sheet, writing to bank[base:].
func (g *Game) decompressSpriteSheet(sheet uint8, bank []byte, base uint16) (err error) {
	s := &g.swordShield
	b, ok1 := g.romU8(uint32(s.sheetBank) + uint32(sheet))
	h, ok2 := g.romU8(uint32(s.sheetHigh) + uint32(sheet))
	l, ok3 := g.romU8(uint32(s.sheetLow) + uint32(sheet))
	if !(ok1 && ok2 && ok3) {
		return fmt.Errorf("sprite sheet $%02x pointer not in ROM", sheet)
	}
	src := uint32(b)<<16 | uint32(h)<<8 | uint32(l)

	next := func() (uint8, error) {
		v, ok := g.romU8(src)
		if !ok {
			return 0, fmt.Errorf("sprite sheet $%02x data at $%06x not in ROM", sheet, src)
		}
		// Decompression_GetNextByte: wrap to the next LoROM bank:
		if src&0xFFFF == 0xFFFF {
			src = (src&0xFF0000 + 0x010000) | 0x8000
		} else {
			src++
		}
		return v, nil
	}

	y := uint16(0)
	put := func(v uint8) { bank[base+y] = v; y++ }

	for steps := 0; steps < 0x10000; steps++ {
		var cmd uint8
		if cmd, err = next(); err != nil {
			return
		}
		if cmd == 0xFF {
			return nil
		}

		var kind uint8
		var length int
		if cmd&0xE0 == 0xE0 {
			// expanded: 3 bit command and 10 bit length
			kind = (cmd << 3) & 0xE0
			var lo uint8
			if lo, err = next(); err != nil {
				return
			}
			length = (int(cmd&0x03)<<8 | int(lo)) + 1
		} else {
			kind = cmd & 0xE0
			length = int(cmd&0x1F) + 1
		}

		switch {
		case kind == 0x00: // copy literal bytes
			for i := 0; i < length; i++ {
				var v uint8
				if v, err = next(); err != nil {
					return
				}
				put(v)
			}
		case kind&0x80 != 0: // copy from an absolute offset in the output
			var lo, hi uint8
			if lo, err = next(); err != nil {
				return
			}
			if hi, err = next(); err != nil {
				return
			}
			x := uint16(lo) | uint16(hi)<<8
			for i := 0; i < length; i++ {
				put(bank[base+x])
				x++
			}
		case kind == 0x20: // repeat byte
			var v uint8
			if v, err = next(); err != nil {
				return
			}
			for i := 0; i < length; i++ {
				put(v)
			}
		case kind == 0x40: // repeat word
			var v0, v1 uint8
			if v0, err = next(); err != nil {
				return
			}
			if v1, err = next(); err != nil {
				return
			}
			for i := 0; i < length; i++ {
				if i&1 == 0 {
					put(v0)
				} else {
					put(v1)
				}
			}
		case kind == 0x60: // incrementing byte
			var v uint8
			if v, err = next(); err != nil {
				return
			}
			for i := 0; i < length; i++ {
				put(v)
				v++
			}
		}
	}

	return fmt.Errorf("sprite sheet $%02x: no end of data", sheet)
}

// convert3bppTo4bppRightPal implements Do3bppToWRAM4bpp_RightPal: converts count 3bpp tiles at bank7F[src:] to 4bpp
// tiles at wram9000[*x:] with the 4th bitplane set wherever a pixel is opaque (palette colors 8-15).
func convert3bppTo4bppRightPal(bank7F []byte, src uint16, wram9000 []byte, x *int, count int) {
	for ; count > 0; count-- {
		p01 := src
		p2 := src + 0x10
		for row := 0; row < 8; row++ {
			lo, hi := bank7F[p01], bank7F[p01+1]
			wram9000[*x], wram9000[*x+1] = lo, hi
			p01 += 2

			b2 := bank7F[p2]
			wram9000[*x+0x10], wram9000[*x+0x11] = b2, lo|hi|b2
			p2++

			*x += 2
		}
		*x += 0x10

		// skip to the next row of 16 tiles in the sheet:
		if p2&0x78 == 0 {
			p2 += 0x180
		}
		src = p2
	}
}

// updateSwordShieldGfx rebuilds the sword and shield tiles when the player's sword or shield changes. It returns the
// unique tile indices whose contents changed.
func (g *Game) updateSwordShieldGfx(swordLevel, shieldLevel uint8) (changed []uint16) {
	s := &g.swordShield
	if !s.ok || (swordLevel == s.swordLevel && shieldLevel == s.shieldLevel) {
		return
	}

	// emulate the WRAM buffers used by the game:
	bank7F := make([]byte, 0x10000)
	if err := g.decompressSpriteSheet(spriteSheetSwordShieldHigh, bank7F, 0x4600); err != nil {
		log.Printf("alttp: sprites: %v\n", err)
		s.ok = false
		return
	}
	if err := g.decompressSpriteSheet(spriteSheetSwordShieldLow, bank7F, 0x4000); err != nil {
		log.Printf("alttp: sprites: %v\n", err)
		s.ok = false
		return
	}

	var tiles [swordShieldTileCount << 5]byte

	// DecompressSwordGraphics; the table has entries for no sword and swords 1-4:
	s.swordOK = false
	if swordLevel <= 4 {
		if offset, ok := g.romU16(uint32(s.swordOffsets) + uint32(swordLevel)<<1); ok {
			x := 0
			convert3bppTo4bppRightPal(bank7F, 0x4000+offset, tiles[:], &x, 12)
			convert3bppTo4bppRightPal(bank7F, 0x4000+offset+0x180, tiles[:], &x, 12)
			s.swordOK = true
		}
	}

	// DecompressShieldGraphics; the table has entries for no shield and shields 1-3:
	s.shieldOK = false
	if shieldLevel <= 3 {
		if offset, ok := g.romU16(uint32(s.shieldOffsets) + uint32(shieldLevel)<<1); ok {
			x := 0x300
			convert3bppTo4bppRightPal(bank7F, 0x4000+offset, tiles[:], &x, 6)
			convert3bppTo4bppRightPal(bank7F, 0x4000+offset+0x180, tiles[:], &x, 6)
			s.shieldOK = true
		}
	}

	for i := 0; i < swordShieldTileCount; i++ {
		o := i << 5
		if string(tiles[o:o+32]) != string(s.tiles[o:o+32]) || s.swordLevel == 0xFF {
			changed = append(changed, uint16(swordShieldTileFirst+i))
		}
	}

	s.tiles = tiles
	s.swordLevel, s.shieldLevel = swordLevel, shieldLevel
	return
}

// swordShieldTile returns the 4bpp tile data for a sword or shield unique tile index.
func (g *Game) swordShieldTile(absidx uint16) ([]byte, bool) {
	s := &g.swordShield
	if absidx < swordShieldTileFirst || absidx >= swordShieldTileEnd {
		return nil, false
	}
	i := int(absidx - swordShieldTileFirst)
	if i < swordTileCount && !s.swordOK || i >= swordTileCount && !s.shieldOK {
		return nil, false
	}
	return s.tiles[i<<5 : i<<5+32], true
}
