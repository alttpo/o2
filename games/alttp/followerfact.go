package alttp

import (
	"fmt"
)

// Follower graphics fact (fact 0x01)
//
// A follower (Zelda, the old man, the blind maiden, the frogsmith and smithy, the locksmith, Kiki, the purple chest,
// the super bomb) is drawn as two 16x16 sprites, chrs $20 and $22 (only $22 for the purple chest and the super bomb).
// Every frame the NMI routine uploads their top rows from $7EB940 + dma_var6*2 and + dma_var7*2 ($0AE8, $0AEA, which
// Tagalong_Draw sets from the pose) and their bottom rows from $200 bytes further on (chrs $30-$31, $32-$33). The 32
// tiles at $7EB940..$7EBD3F are what LoadFollowerGraphics built from ROM sprite sheets for the follower
// ($7EF3CC) when the area loaded: two rows of 16 tiles.
//
// The fact holds that buffer for every follower $00-$0D, built from the ROM the same way, $400 bytes each at follower
// * $400 ($3800 = 14,336 bytes). A follower's chrs are bound to it by pose with MsgSpriteChrs, like Link's to fact
// 0x00: chr $20 (n = 2) to tile follower*32 + dma_var6/16, chr $30 to 16 tiles after it, and the same for chr $22 and
// $32 with dma_var7 (see followerFactTile). Receivers draw another player's follower with that player's own follower
// graphics and pose, whatever follower they have themselves.
const (
	factFollowerGraphics = 0x01

	// followerCount is the number of followers in the fact, $00-$0D (LoadFollowerGraphics' offset table):
	followerCount    = 14
	followerGfxSize  = 0x400
	followerFactSize = followerCount * followerGfxSize

	// the WRAM buffer LoadFollowerGraphics builds, and the bottom row's offset in it:
	followerGfxWRAM   = 0xB940
	followerBottomRow = 0x200

	// the tiles of a follower in the fact, and the offset of the bottom row's tiles:
	followerFactTiles     = followerGfxSize >> 5
	followerFactBottomRow = followerBottomRow >> 5
)

// romFollowerGfxUS and romFollowerGfxJP are where LoadFollowerGraphics is in the US and JP 1.0 ROMs (randomizers are
// JP-based):
const (
	romFollowerGfxUS = uint32(0x00D423)
	romFollowerGfxJP = uint32(0x00D463)
)

// followerGfxRoutine is what o2 reads of LoadFollowerGraphics:
//
//	PHB : PHK : PLB
//	LDY.b #$64 : LDA.l $7EF3CC : CMP.b #$01 : BEQ .start      ; Zelda
//	LDY.b #$66 : LDA.l $7EF3CC : CMP.b #$09 : BCC .start
//	LDY.b #$59 : CMP.b #$0C : BCC .start
//	LDY.b #$58
//	.start: JSR Decompress_sprite_high : LDY.b #$65 : JSR Decompress_sprite_low
//	REP #$30 : LDA.l $7EF3CC : AND.w #$00FF : ASL A : TAX
//	LDA.b $00 : CLC : ADC.l .gfx_offset,X
//	LDY.w #$0020 : LDX.w #$2940 : JSR Do3bppToWRAM4bpp_LeftPal_arbitrary
type followerGfxRoutine struct {
	// the sheet decompressed to $7F4600 for Zelda, for followers below the second threshold, below the third, and the
	// rest:
	sheetZelda, sheetA, sheetB, sheetC uint8
	// the follower that gets sheetZelda, and the two thresholds:
	zelda, thresholdB, thresholdC uint8
	// the sheet decompressed to $7F4000:
	sheetLow uint8
	// the bus address of .gfx_offset: per follower, the offset of its first 3bpp tile from $7F4000:
	gfxOffsets uint32
}

// followerGfxPattern is LoadFollowerGraphics' code; -1 marks the bytes o2 reads (sheets, thresholds) or that differ
// between ROMs (JSR targets, the table's address):
var followerGfxPattern = []int{
	0x8B, 0x4B, 0xAB,
	0xA0, -1, 0xAF, 0xCC, 0xF3, 0x7E, 0xC9, -1, 0xF0, 0x12,
	0xA0, -1, 0xAF, 0xCC, 0xF3, 0x7E, 0xC9, -1, 0x90, 0x08,
	0xA0, -1, 0xC9, -1, 0x90, 0x02,
	0xA0, -1,
	0x20, -1, -1, 0xA0, -1, 0x20, -1, -1,
	0xC2, 0x30, 0xAF, 0xCC, 0xF3, 0x7E, 0x29, 0xFF, 0x00, 0x0A, 0xAA,
	0xA5, 0x00, 0x18, 0x7F, -1, -1, -1,
	0xA0, 0x20, 0x00, 0xA2, 0x40, 0x29,
}

// findFollowerGfxRoutine reads LoadFollowerGraphics at the US or JP 1.0 address.
func (g *Game) findFollowerGfxRoutine() (r followerGfxRoutine, err error) {
	for _, addr := range []uint32{romFollowerGfxUS, romFollowerGfxJP} {
		code := make([]byte, len(followerGfxPattern))
		ok := true
		for i, want := range followerGfxPattern {
			v, inROM := g.romU8(addr + uint32(i))
			if !inROM || (want >= 0 && v != uint8(want)) {
				ok = false
				break
			}
			code[i] = v
		}
		if !ok {
			continue
		}
		r = followerGfxRoutine{
			sheetZelda: code[4], zelda: code[10],
			sheetA: code[14], thresholdB: code[20],
			sheetB: code[24], thresholdC: code[26],
			sheetC:     code[30],
			sheetLow:   code[35],
			gfxOffsets: uint32(code[54]) | uint32(code[55])<<8 | uint32(code[56])<<16,
		}
		return r, nil
	}
	return r, fmt.Errorf("LoadFollowerGraphics not found in ROM")
}

// sheet returns the sheet LoadFollowerGraphics decompresses to $7F4600 for a follower.
func (r *followerGfxRoutine) sheet(follower uint8) uint8 {
	switch {
	case follower == r.zelda:
		return r.sheetZelda
	case follower < r.thresholdB:
		return r.sheetA
	case follower < r.thresholdC:
		return r.sheetB
	}
	return r.sheetC
}

// expand3To4Low is the game's Do3bppToWRAM4bpp_LeftPal: num 3bpp tiles from src[off:] to 4bpp tiles at dst[x:], with
// the 4th bitplane clear (palette colors 0-7).
func expand3To4Low(src []byte, off int, dst []byte, x int, num int) {
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

// buildFollowerFact builds fact 0x01 from the ROM: for every follower, the buffer LoadFollowerGraphics builds.
func (g *Game) buildFollowerFact() ([]byte, error) {
	if !g.swordShield.ok {
		return nil, fmt.Errorf("sprite sheet routines not found in ROM")
	}
	r, err := g.findFollowerGfxRoutine()
	if err != nil {
		return nil, err
	}
	fact := make([]byte, followerFactSize)
	for f := 0; f < followerCount; f++ {
		bank7F := make([]byte, 0x10000)
		if err = g.decompressSpriteSheet(r.sheet(uint8(f)), bank7F, 0x4600); err != nil {
			return nil, err
		}
		if err = g.decompressSpriteSheet(r.sheetLow, bank7F, 0x4000); err != nil {
			return nil, err
		}
		offset, ok := g.romU16(r.gfxOffsets + uint32(f)<<1)
		if !ok {
			return nil, fmt.Errorf("follower graphics offset table not in ROM")
		}
		src := 0x4000 + int(offset)
		if src+(followerGfxSize>>5)*24 > len(bank7F) {
			return nil, fmt.Errorf("follower %#02x graphics offset %#04x out of range", f, offset)
		}
		expand3To4Low(bank7F, src, fact, f*followerGfxSize, followerGfxSize>>5)
	}
	return fact, nil
}

// followerFactTile returns the tile of fact 0x01 that a follower's chr $20 (dma = dma_var6) or $22 (dma = dma_var7)
// shows; the bottom row's chr ($30, $32) shows the tile followerFactBottomRow after it. noFactTile for no follower, a
// follower not in the fact, or a source that isn't 2 whole tiles of the top row.
func followerFactTile(follower, dma uint8) uint16 {
	if follower == 0 || follower >= followerCount || dma&0x0F != 0 || dma > 0xE0 {
		return noFactTile
	}
	return uint16(follower)*followerFactTiles + uint16(dma>>4)
}
