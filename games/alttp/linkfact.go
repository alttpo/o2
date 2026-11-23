package alttp

import (
	"fmt"
)

// Link graphics fact (fact 0x00; see o2-server's PROTOCOL05.md, "Sprites with facts")
//
// Every frame the NMI routine DMAs Link's graphics into fixed VRAM chrs from source addresses the pose picks: his head,
// body and aux from LinkGraphics in ROM (bank $10), his sword, shield and the item in use from 4bpp buffers in WRAM
// that the game builds from ROM sprite sheets when it loads (DecompressSwordGraphics, DecompressShieldGraphics,
// LoadItemGFXIntoWRAM4BPPBuffer, LoadItemGFX_Auxiliary). The fact holds all of it, every sword and shield level
// included, built from the ROM the same way, so that other players can draw this player with its own graphics (custom
// Link sprites included) from the pose alone (MsgSpriteChrs binds VRAM chrs to tiles of the fact).
//
//	offset  size   contents
//	$0000   $7000  bank $10 LinkGraphics
//	$7000   $0C00  swords 1-4, $300 each, laid out as WRAM $7E9000 for that level
//	$7C00   $0480  shields 1-3, $180 each, laid out as WRAM $7E9300 for that level
//	$8080   $0D00  the item buffer, as WRAM $7E9480-$7EA17F
//	$8D80   $0400  the flute's bird, as WRAM $7EB540-$7EB93F
const (
	factLinkGraphics = 0x00

	lfBody       = 0x0000
	lfBodySize   = 0x7000
	lfSwords     = 0x7000
	lfSwordSize  = 0x300
	lfShields    = 0x7C00
	lfShieldSize = 0x180
	lfItems      = 0x8080
	lfItemsSize  = 0xD00
	lfBird       = 0x8D80
	lfBirdSize   = 0x400
	linkFactSize = lfBird + lfBirdSize

	// WRAM addresses of the buffers:
	lfSwordWRAM  = 0x9000
	lfShieldWRAM = 0x9300
	lfItemsWRAM  = 0x9480
	lfBirdWRAM   = 0xB540

	// sprite sheets the item buffer and the bird are built from:
	sheetItemsA = 0x5F // 95
	sheetItemsB = 0x60 // 96
	sheetBird   = 0x58

	noFactTile = 0xFFFF
)

// linkItemGfx is LoadItemGFXIntoWRAM4BPPBuffer's list of item animations: the sheet the tiles are in (0 = sprite
// sheet 0, read uncompressed from ROM), the 3bpp tile they start at, and their number of 16 pixel wide columns.
var linkItemGfx = []struct {
	sheet uint8
	tile  int
	num   int
}{
	{0, 0, 7},            // rod
	{0, 11, 7},           // hammer
	{0, 8, 3},            // bow
	{sheetItemsA, 38, 4}, // shovel
	{sheetItemsA, 42, 3}, // sleeping zzz
	{sheetItemsA, 45, 1}, // misc
	{0, 34, 4},           // hookshot
	{sheetItemsB, 3, 14}, // bug net
	{sheetItemsB, 33, 7}, // cane
	{sheetItemsA, 46, 2}, // book of mudora
}

// expand3To4High is the game's Expand3To4High (Do3bppToWRAM4bpp_RightPal): num 3bpp tiles from src[off:] to 4bpp tiles
// at dst[*x:], with the 4th bitplane set wherever a pixel is opaque (palette colors 8-15). Rows of 16 tiles in src are
// $180 bytes apart.
func expand3To4High(src []byte, off int, dst []byte, x *int, num int) {
	for ; num > 0; num-- {
		p2 := off + 0x10
		for row := 0; row < 8; row++ {
			lo, hi := src[off], src[off+1]
			b2 := src[p2]
			dst[*x], dst[*x+1] = lo, hi
			dst[*x+0x10], dst[*x+0x11] = b2, lo|hi|b2
			off += 2
			p2++
			*x += 2
		}
		*x += 0x10
		off = p2
		if off&0x78 == 0 {
			off += 0x180
		}
	}
}

// do3To4Low is the game's Do3To4Low16Bit: num 3bpp tiles from src[off:] to 4bpp tiles at dst[x:], with the 4th bitplane
// clear (palette colors 0-7).
func do3To4Low(src []byte, off int, dst []byte, x int, num int) {
	for ; num > 0; num-- {
		p2 := off + 0x10
		for row := 0; row < 8; row++ {
			dst[x], dst[x+1] = src[off], src[off+1]
			dst[x+0x10], dst[x+0x11] = src[p2], 0
			off += 2
			p2++
			x += 2
		}
		x += 0x10
		off = p2
	}
}

// spriteSheet returns a sprite graphics sheet: sheet 0 read as it is from ROM (it is stored uncompressed), others
// decompressed.
func (g *Game) spriteSheet(sheet uint8) ([]byte, error) {
	buf := make([]byte, 0x10000)
	if sheet != 0 {
		if err := g.decompressSpriteSheet(sheet, buf, 0); err != nil {
			return nil, err
		}
		return buf, nil
	}
	s := &g.swordShield
	b, ok1 := g.romU8(uint32(s.sheetBank))
	h, ok2 := g.romU8(uint32(s.sheetHigh))
	l, ok3 := g.romU8(uint32(s.sheetLow))
	if !(ok1 && ok2 && ok3) {
		return nil, fmt.Errorf("sprite sheet $00 pointer not in ROM")
	}
	src := uint32(b)<<16 | uint32(h)<<8 | uint32(l)
	// the item animations read up to 2 rows of 16 tiles past the end of the sheet's first $600 bytes:
	for i := 0; i < 0x900; i++ {
		v, ok := g.romU8(src)
		if !ok {
			return nil, fmt.Errorf("sprite sheet $00 data at $%06x not in ROM", src)
		}
		buf[i] = v
		if src&0xFFFF == 0xFFFF {
			src = (src&0xFF0000 + 0x010000) | 0x8000
		} else {
			src++
		}
	}
	return buf, nil
}

// buildLinkFact builds fact 0x00 from the ROM.
func (g *Game) buildLinkFact() ([]byte, error) {
	if !g.linkSprites.tileBankOK {
		return nil, fmt.Errorf("LinkGraphics not found in ROM")
	}
	if !g.swordShield.ok {
		return nil, fmt.Errorf("sprite sheet routines not found in ROM")
	}
	fact := make([]byte, linkFactSize)

	for i := 0; i < lfBodySize>>5; i++ {
		tile, ok := g.linkTile(uint16(i))
		if !ok {
			return nil, fmt.Errorf("LinkGraphics tile %#03x not in ROM", i)
		}
		copy(fact[lfBody+i<<5:], tile)
	}

	// swords and shields: as DecompressSwordGraphics and DecompressShieldGraphics, from sheets $5E and $5F, which the
	// game decompresses to $7F4000 and $7F4600:
	bank7F := make([]byte, 0x10000)
	if err := g.decompressSpriteSheet(spriteSheetSwordShieldHigh, bank7F, 0x4600); err != nil {
		return nil, err
	}
	if err := g.decompressSpriteSheet(spriteSheetSwordShieldLow, bank7F, 0x4000); err != nil {
		return nil, err
	}
	for level := 1; level <= 4; level++ {
		offset, ok := g.romU16(uint32(g.swordShield.swordOffsets) + uint32(level)<<1)
		if !ok {
			return nil, fmt.Errorf("sword offset table not in ROM")
		}
		x := lfSwords + (level-1)*lfSwordSize
		expand3To4High(bank7F, 0x4000+int(offset), fact, &x, 12)
		expand3To4High(bank7F, 0x4000+int(offset)+0x180, fact, &x, 12)
	}
	for level := 1; level <= 3; level++ {
		offset, ok := g.romU16(uint32(g.swordShield.shieldOffsets) + uint32(level)<<1)
		if !ok {
			return nil, fmt.Errorf("shield offset table not in ROM")
		}
		x := lfShields + (level-1)*lfShieldSize
		expand3To4High(bank7F, 0x4000+int(offset), fact, &x, 6)
		expand3To4High(bank7F, 0x4000+int(offset)+0x180, fact, &x, 6)
	}

	// the item buffer, as LoadItemGFXIntoWRAM4BPPBuffer (LoadItemAnimationGfxOne):
	sheets := map[uint8][]byte{}
	x := lfItems
	for _, it := range linkItemGfx {
		src, ok := sheets[it.sheet]
		if !ok {
			var err error
			if src, err = g.spriteSheet(it.sheet); err != nil {
				return nil, err
			}
			sheets[it.sheet] = src
		}
		off := it.tile * 24
		expand3To4High(src, off, fact, &x, it.num)
		expand3To4High(src, off+0x180, fact, &x, it.num)
	}
	if x != lfItems+lfItemsSize {
		return nil, fmt.Errorf("item buffer is %#x bytes; expected %#x", x-lfItems, lfItemsSize)
	}

	// the bird, as LoadItemGFX_Auxiliary:
	bird, err := g.spriteSheet(sheetBird)
	if err != nil {
		return nil, err
	}
	do3To4Low(bird, 0, fact, lfBird, lfBirdSize>>5)

	return fact, nil
}

// linkFactTile returns the tile of fact 0x00 that the DMA source address src (bank $10 for the head, body and aux
// slots, else WRAM bank $7E) shows, with the sword and shield levels the player has; noFactTile if none.
func linkFactTile(src uint16, wram bool, swordLevel, shieldLevel uint8) uint16 {
	if !wram {
		if src < 0x8000 || int(src-0x8000) >= lfBodySize {
			return noFactTile
		}
		return uint16(lfBody+int(src-0x8000)) >> 5
	}
	a := int(src)
	switch {
	case a >= lfSwordWRAM && a < lfSwordWRAM+lfSwordSize:
		if swordLevel < 1 || swordLevel > 4 {
			return noFactTile
		}
		return uint16(lfSwords+int(swordLevel-1)*lfSwordSize+a-lfSwordWRAM) >> 5
	case a >= lfShieldWRAM && a < lfShieldWRAM+lfShieldSize:
		if shieldLevel < 1 || shieldLevel > 3 {
			return noFactTile
		}
		return uint16(lfShields+int(shieldLevel-1)*lfShieldSize+a-lfShieldWRAM) >> 5
	case a >= lfItemsWRAM && a < lfItemsWRAM+lfItemsSize:
		return uint16(lfItems+a-lfItemsWRAM) >> 5
	case a >= lfBirdWRAM && a < lfBirdWRAM+lfBirdSize:
		return uint16(lfBird+a-lfBirdWRAM) >> 5
	}
	return noFactTile
}
