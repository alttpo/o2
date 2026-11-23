package alttp

import (
	"encoding/binary"
	"io"
	"log"
	"o2/snes"

	"github.com/alttpo/snes/mapping/lorom"
)

// Link sprite sync (outgoing only)
//
// alttpo clients draw remote players from the OAM sprites they broadcast (MsgSprites). Link's body sprites are drawn
// from "unique tiles": 8x8 4bpp tiles identified by their index into Link's graphics in ROM (LinkGraphics, bank $10).
// Each frame the game DMAs the tiles for Link's current pose from ROM to VRAM using the source addresses at
// $0ACC/$0AD0/$0AD4 (head, body, aux; top row) and $0ACE/$0AD2/$0AD6 (bottom row). A tile's unique index is its
// offset from the start of LinkGraphics divided by 32. The tile data itself is no longer sent; the sprite redesign
// (o2-server's PROTOCOL05.md, "Sprites with facts") replaces unique tiles with the Link graphics fact.
//
// alttpo reads tiles from emulated VRAM; o2 reads them straight from its in-memory copy of the ROM, which also picks up
// custom Link sprites patched into the ROM. Sword and shield tiles are rebuilt from ROM the same way the game builds
// them in WRAM (see swordshield.go). Other sprites whose tiles are only in VRAM are not sent since o2 cannot read VRAM.
// Link's shadow is sent without tile data, the same as alttpo does: receivers draw it with their own shadow tiles.
//
// Incoming sprites are used to draw remote players (see remotesprites.go): one with Link's head, body, sword and shield,
// whose tiles are taken from the local ROM and WRAM. Tiles sent inline with the sprites are only used to find alttpo
// players' sword and shield poses.

// linkOAMCount is the number of OAM entries reserved for Link, starting at OAM index ($0352 >> 2):
const linkOAMCount = 12

// linkUniqTileCount is the number of LinkGraphics tiles that alttpo treats as unique tiles ($000..$37F):
const linkUniqTileCount = 0x380

const noUniqTile = 0xFFFF

type linkSprite struct {
	// OAM index:
	index uint8
	// OAM table bytes: x, y, chr, attributes; b4 = (size << 1) | x bit 8:
	b      [5]uint8
	size   uint8
	pal    uint8
	absidx [4]uint16
}

type linkSpriteSync struct {
	// oamCopy is the last read of the copy of Link's OAM entries that the patched ROM writes to SRAM every frame;
	// see linkOAMCopyAddr for the layout:
	oamCopy      [linkOAMCopyReadSize]byte
	oamCopyFresh bool

	// bank of LinkGraphics in ROM:
	tileBank   uint8
	tileBankOK bool

	// sprites sampled this frame and not yet sent:
	sprites           []linkSprite
	fresh             bool
	linkMissingFrames int

	// sprite palettes 0 through 7 ($7EC600..$7EC6FF; color 0 of palette 0 is not read, it is transparent anyway):
	palettes [8][32]byte
}

func (g *Game) initSpriteSync() {
	s := &g.linkSprites

	// find LinkGraphics bank from `LDA.b #LinkGraphics>>16` at $00:8A01 in the NMI routine:
	s.tileBank, s.tileBankOK = 0x10, true
	if pak, err := lorom.BusAddressToPak(0x008A01); err == nil && int(pak)+1 < len(g.rom.Contents) {
		if g.rom.Contents[pak] == 0xA9 {
			s.tileBank = g.rom.Contents[pak+1]
		} else {
			log.Printf("alttp: sprites: unexpected NMI code at $008A01; assuming LinkGraphics is in bank $10\n")
		}
	}
	if _, ok := g.linkTile(0); !ok {
		log.Printf("alttp: sprites: LinkGraphics bank $%02x not in ROM; disabling sprite sync\n", s.tileBank)
		s.tileBankOK = false
	}
}

// linkTile returns the 32 bytes of 4bpp tile data for unique tile absidx.
func (g *Game) linkTile(absidx uint16) (tile []byte, ok bool) {
	s := &g.linkSprites
	if absidx >= linkUniqTileCount {
		return g.swordShieldTile(absidx)
	}
	pak, err := lorom.BusAddressToPak(uint32(s.tileBank)<<16 | (0x8000 + uint32(absidx)<<5))
	if err != nil || int(pak)+32 > len(g.rom.Contents) {
		return nil, false
	}
	return g.rom.Contents[pak : pak+32], true
}

// linkOAMCopyPak is the FX Pak Pro address of the copy of Link's OAM entries in SRAM:
var linkOAMCopyPak = func() uint32 {
	pak, err := lorom.BusAddressToPak(linkOAMCopyAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// enqueueSpriteReads adds the SRAM read needed to sample Link's sprites. The OAM buffer in WRAM is not read directly
// since the game clears it at the start of every frame; the patched ROM copies Link's entries to SRAM instead.
func (g *Game) enqueueSpriteReads(q []snes.Read) []snes.Read {
	return g.readEnqueue(q, linkOAMCopyPak, linkOAMCopyReadSize, nil)
}

// readSpriteResponse keeps the copy of Link's OAM entries from an SRAM read response.
func (g *Game) readSpriteResponse(rsp snes.Response) {
	if rsp.Address != linkOAMCopyPak || len(rsp.Data) != linkOAMCopyReadSize {
		return
	}
	s := &g.linkSprites
	copy(s.oamCopy[:], rsp.Data)
	s.oamCopyFresh = true
}

// oamCopyDMAU16 reads a word of $0AC0..$0AD7 (sword, shield and Link's graphics DMA source addresses) from the copy
// of Link's OAM entries, which matches the copied entries' tiles.
func (s *linkSpriteSync) oamCopyDMAU16(addr uint32) uint16 {
	o := linkOAMCopyDMA + addr - linkOAMCopyDMAWRAM
	return uint16(s.oamCopy[o]) | uint16(s.oamCopy[o+1])<<8
}

// linkUniqTileIndex returns the unique tile index for the VRAM chr, or false if it is not one of Link's body tiles.
func (g *Game) linkUniqTileIndex(chr uint16) (uint16, bool) {
	if chr >= 0x20 {
		return 0, false
	}
	chrX, chrY := uint32(chr&0x0F), uint32(chr>>4)

	var ptr, x uint32
	switch {
	case chrX <= 0x01:
		// head:
		ptr, x = 0x0ACC, chrX
	case chrX <= 0x03:
		// body:
		ptr, x = 0x0AD0, chrX-2
	case chrX == 0x04:
		// aux:
		ptr, x = 0x0AD4, 0
	case chrX <= 0x06:
		// sword, from WRAM:
		return g.swordShieldUniqTileIndex(0x0AC0+chrY<<1, chrX-5)
	case chrX <= 0x08:
		// shield, from WRAM:
		return g.swordShieldUniqTileIndex(0x0AC4+chrY<<1, chrX-7)
	default:
		// other items are decompressed into WRAM; not available:
		return 0, false
	}

	src := uint32(g.linkSprites.oamCopyDMAU16(ptr + chrY<<1))
	if src < 0x8000 {
		return 0, false
	}
	idx := (src - 0x8000 + x<<5) >> 5
	if idx >= linkUniqTileCount {
		return 0, false
	}
	return uint16(idx), true
}

// swordShieldUniqTileIndex maps a sword or shield DMA source address in WRAM to a unique tile index.
func (g *Game) swordShieldUniqTileIndex(ptr uint32, x uint32) (uint16, bool) {
	src := uint32(g.linkSprites.oamCopyDMAU16(ptr)) + x<<5
	if src < swordShieldWRAMStart || src >= swordShieldWRAMEnd {
		return 0, false
	}
	idx := uint16(swordShieldTileFirst + (src-swordShieldWRAMStart)>>5)
	if _, ok := g.swordShieldTile(idx); !ok {
		return 0, false
	}
	return idx, true
}

// isLinkShadowChr reports whether chr is one of Link's shadow tiles: $6C on the ground, $28 or $38 while recoiling or
// jumping off a ledge, or what the game draws under him instead (kLinkShadows_Chardata; see isLinkFootChr). They
// are static tiles that receivers have in their own VRAM.
func isLinkShadowChr(chr uint16) bool {
	return (chr >= 0x6C && chr <= 0x6F) || (chr >= 0x7C && chr <= 0x7F) || chr == 0x28 || chr == 0x38 ||
		isLinkFootChr(chr)
}

// isLinkFootChr reports whether chr is what LinkOam_DrawFootObject draws under Link's feet in his own OAM entries
// instead of his shadow: water ripples $D8..$DA in shallow water and swamp puddles, and tall grass $C8..$CA.
func isLinkFootChr(chr uint16) bool {
	return (chr >= 0xD8 && chr <= 0xDA) || (chr >= 0xC8 && chr <= 0xCA)
}

// readLinkSprites samples Link's sprites from the copy of Link's OAM entries just read. Called once per game frame
// while syncing.
func (g *Game) readLinkSprites() {
	s := &g.linkSprites
	if !s.tileBankOK {
		return
	}
	if !s.oamCopyFresh {
		return
	}
	if !g.spritesGated() {
		// the patch doesn't copy Link's OAM entries while the sprites gate is clear; the copy is stale:
		return
	}

	// send the scroll offsets Link's entries were drawn with, so receivers place the sprites with the offsets of the
	// same frame (see linkOAMCopyScroll); not on the overworld map, like the offsets read from WRAM:
	if local := g.local; !(local.Module == 0x0E && local.SubModule == 0x07) {
		local.XOffs = int16(binary.LittleEndian.Uint16(s.oamCopy[linkOAMCopyScroll:]))
		local.YOffs = int16(binary.LittleEndian.Uint16(s.oamCopy[linkOAMCopyScroll+2:]))
	}

	if g.local.isGameOver() {
		// the GAME OVER animation and menu draw their own sprites around Link's OAM slots, which alttpo would draw
		// garbled; send no sprites so receivers stop drawing this player:
		s.sprites = s.sprites[:0]
		s.fresh = true
		return
	}

	c := s.oamCopy[:]
	start := uint16(c[linkOAMCopyStart]) | uint16(c[linkOAMCopyStart+1])<<8
	if start&3 != 0 || start >= 0x200 {
		// not a valid OAM buffer offset; SRAM may not have been written yet:
		return
	}

	// rebuild sword and shield tiles if the player's equipment changed:
	if g.wramFresh[0xF359] && g.wramFresh[0xF35A] {
		g.updateSwordShieldGfx(g.wram[0xF359], g.wram[0xF35A])
	}

	s.sprites = s.sprites[:0]
	for j := 0; j < linkOAMCount; j++ {
		i := int(start>>2) + j
		if i >= 0x80 {
			// the copy is contiguous and does not wrap around to the start of the OAM buffer:
			break
		}
		o := linkOAMCopyEntries + j<<2
		sp := linkSprite{index: uint8(i)}
		copy(sp.b[:4], c[o:o+4])
		sp.b[4] = c[linkOAMCopyExt+j] & 0x03
		sp.size = (sp.b[4] >> 1) & 1
		sp.pal = (sp.b[3] >> 1) & 7

		// skip disabled sprites the same way alttpo's Sprite::decodeOAMTableBytes does:
		if sp.b[1] == 0xF0 {
			continue
		}
		x := int(sp.b[0]) | int(sp.b[4]&1)<<8
		if x >= 256 {
			x -= 512
		}
		px := 8
		if sp.size != 0 {
			px = 16
		}
		if !(x > -px && x < 256) {
			continue
		}

		chr := uint16(sp.b[2]) | uint16(sp.b[3]&1)<<8
		sp.absidx = [4]uint16{noUniqTile, noUniqTile, noUniqTile, noUniqTile}
		if !isLinkShadowChr(chr) {
			chrs := []uint16{chr}
			if sp.size != 0 {
				chrs = append(chrs, chr+0x01, chr+0x10, chr+0x11)
			}
			ok := true
			for k, c := range chrs {
				if sp.absidx[k], ok = g.linkUniqTileIndex(c); !ok {
					break
				}
			}
			if !ok {
				// tiles not available from ROM (sword, shield, etc.):
				continue
			}
		}

		s.sprites = append(s.sprites, sp)
	}

	// captured sparkles, dust, projectiles and carried and thrown objects (see emitCaptureExtras): their tiles are
	// static or in the common sprite sheets, which receivers draw from their own VRAM:
	if n := int(c[linkOAMCopyExtras]) | int(c[linkOAMCopyExtras+1])<<8; n <= linkOAMExtrasMax*linkOAMExtraSize {
		var extras []linkSprite
		for o := linkOAMCopyExtras + 2; o < linkOAMCopyExtras+2+n; o += linkOAMExtraSize {
			e := c[o : o+linkOAMExtraSize]
			sp := linkSprite{index: e[5] & 0x7F}
			copy(sp.b[:4], e[:4])
			sp.b[4] = e[4] & 3
			sp.size = (sp.b[4] >> 1) & 1
			sp.pal = (sp.b[3] >> 1) & 7
			sp.absidx = [4]uint16{noUniqTile, noUniqTile, noUniqTile, noUniqTile}
			extras = append(extras, sp)
		}
		dying, dyingAll := oamCopyDying(c[:])
		for _, sp := range extras {
			// enemies and NPCs on screen use the same shadows as bombs and thrown objects; only send those the objects
			// drew themselves:
			if isCaptureShadowChr(sp.b[2]) && !isCasterShadow(sp, extras) {
				continue
			}
			// enemies' deaths are synced by alttpo, so every player draws them already:
			if isEnemyDeathChr(sp.b[2]) && (dyingAll || nearDyingEnemy(&sp, dying)) {
				continue
			}
			s.sprites = append(s.sprites, sp)
		}
	}

	if g.wramFresh[0xC601] {
		for k := range s.palettes {
			o := 0xC600 + k<<5
			copy(s.palettes[k][:], g.wram[o:o+32])
		}
	}
	s.fresh = true
}

// SerializeLinkSprites writes message 0x03 in alttpo's format. Tile data is never sent inline; each palette in
// sendPalette is sent along with the first sprite that uses it. alttpo draws remote sprites with the palettes it last
// received from the player.
func (g *Game) SerializeLinkSprites(w io.Writer, sprites []linkSprite, sendPalette [8]bool) {
	s := &g.linkSprites
	_ = binary.Write(w, binary.LittleEndian, uint8(MsgSprites))
	_ = binary.Write(w, binary.LittleEndian, uint8(len(sprites)))

	var paletteSent [8]bool
	for _, sp := range sprites {
		b4 := sp.b[4]
		withPalette := sendPalette[sp.pal] && !paletteSent[sp.pal]
		if withPalette {
			b4 |= 0x80
			paletteSent[sp.pal] = true
		}
		_, _ = w.Write([]byte{sp.index, sp.b[0], sp.b[1], sp.b[2], sp.b[3], b4})
		n := 1
		if sp.size != 0 {
			n = 4
		}
		for _, idx := range sp.absidx[:n] {
			_ = binary.Write(w, binary.LittleEndian, idx)
		}
		if withPalette {
			_, _ = w.Write(s.palettes[sp.pal][:])
		}
	}
}

// oamCopyDying returns the screen positions of the enemies drawing their death that the trampoline listed with the
// captured entries (see linkOAMCopyDying), or all if it listed more than it holds (all death chrs are left out then).
func oamCopyDying(c []byte) (dying [][2]int, all bool) {
	n := int(c[linkOAMCopyDying]) | int(c[linkOAMCopyDying+1])<<8
	if n > linkOAMDyingMax*4 {
		return nil, true
	}
	for o := linkOAMCopyDying + 2; o < linkOAMCopyDying+2+n; o += 4 {
		x := int(int16(uint16(c[o]) | uint16(c[o+1])<<8))
		y := int(int16(uint16(c[o+2]) | uint16(c[o+3])<<8))
		dying = append(dying, [2]int{x, y})
	}
	return
}
