package alttp

import (
	"encoding/binary"
	"io"
	"log"
	"o2/snes"
	"sort"

	"github.com/alttpo/snes/mapping/lorom"
)

// Link sprite sync (outgoing)
//
// Every frame the NMI routine DMAs Link's graphics into fixed VRAM chrs ($00-$0A and $10-$1A) from source addresses the
// pose picks (DMA sources $0AC0-$0ACF; see linkChrRows). Those sources are ROM data or built from ROM, and all of it is
// in the Link graphics fact (fact 0x00, see linkfact.go), so o2 sends the pose instead of tiles (see o2-server's
// PROTOCOL05.md, "Sprites with facts"):
//
//   - MsgSpriteChrs binds the rows of Link's chrs the sprites show to tiles of fact 0x00;
//   - MsgSprites is Link's OAM entries (x, y, chr, attributes, extended bits), in OAM order;
//   - MsgSpritePalettes is the sprite palettes the sprites use, when they change.
//
// Receivers draw a bound chr from the sender's fact, and an unbound one (Link's shadow, sparkles, dust, carried
// objects) from their own VRAM. Sprites showing one of Link's chrs that has no tile (e.g. an unknown DMA source) are not
// sent.
//
// Incoming sprites are used to draw remote players (see remotesprites.go).

// linkOAMCount is the number of OAM entries reserved for Link, starting at OAM index ($0352 >> 2):
const linkOAMCount = 12

// linkUniqTileCount is the number of LinkGraphics tiles ($000..$37F):
const linkUniqTileCount = 0x380

const noUniqTile = 0xFFFF

// linkChrRows are the rows of Link's VRAM chrs that NMI_DoUpdates uploads every frame, and the WRAM address of each
// one's DMA source address: from LinkGraphics (bank $10) for the head, body and aux, from WRAM bank $7E for the sword,
// shield and the item in use.
var linkChrRows = []struct {
	chr, n uint8
	ptr    uint32
	wram   bool
}{
	{0x00, 2, 0x0ACC, false}, {0x10, 2, 0x0ACE, false}, // head
	{0x02, 2, 0x0AD0, false}, {0x12, 2, 0x0AD2, false}, // body
	{0x04, 1, 0x0AD4, false}, {0x14, 1, 0x0AD6, false}, // aux
	{0x05, 2, 0x0AC0, true}, {0x15, 2, 0x0AC2, true}, // sword
	{0x07, 2, 0x0AC4, true}, {0x17, 2, 0x0AC6, true}, // shield
	{0x09, 2, 0x0AC8, true}, {0x19, 2, 0x0ACA, true}, // item in use
}

// isLinkChr reports whether chr is one of Link's chrs in linkChrRows.
func isLinkChr(chr uint16) bool {
	return chr < 0x20 && chr&0x0F <= 0x0A
}

type linkSprite struct {
	// OAM index:
	index uint8
	// OAM table bytes: x, y, chr, attributes; b4 = (size << 1) | x bit 8:
	b    [5]uint8
	size uint8
	pal  uint8
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

	// the tiles of fact 0x00 that Link's chrs $00..$1F show this frame; noFactTile where none:
	chrTiles [0x20]uint16
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

// linkChrTiles returns the tiles of fact 0x00 that Link's chrs show, from the DMA source addresses in the copy of
// Link's OAM entries and the sword and shield levels.
func (g *Game) linkChrTiles() (tiles [0x20]uint16) {
	s := &g.linkSprites
	for i := range tiles {
		tiles[i] = noFactTile
	}
	sword, shield := g.wram[0xF359], g.wram[0xF35A]
	for _, r := range linkChrRows {
		t := linkFactTile(s.oamCopyDMAU16(r.ptr), r.wram, sword, shield)
		if t == noFactTile {
			continue
		}
		for k := uint8(0); k < r.n; k++ {
			tiles[r.chr+k] = t + uint16(k)
		}
	}
	return
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
	s.chrTiles = g.linkChrTiles()

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
		if !isLinkShadowChr(chr) {
			chrs := []uint16{chr}
			if sp.size != 0 {
				chrs = append(chrs, chr+0x01, chr+0x10, chr+0x11)
			}
			ok := true
			for _, c := range chrs {
				if !isLinkChr(c) || s.chrTiles[c] == noFactTile {
					ok = false
					break
				}
			}
			if !ok {
				// not one of Link's chrs, or one without a tile:
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

// linkSpritesInOrder returns the sprites ordered by OAM index (front to back).
func linkSpritesInOrder(sprites []linkSprite) []linkSprite {
	sorted := append([]linkSprite(nil), sprites...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].index < sorted[j].index })
	return sorted
}

// SerializeSprites writes MsgSprites: `u8 count`, then per sprite in OAM order `u8 x, u8 y, u8 chr, u8 attributes,
// u8 ext` (bit 0 = x bit 8, bit 1 = 16x16).
func SerializeSprites(w io.Writer, sprites []linkSprite) error {
	b := []byte{byte(MsgSprites), uint8(len(sprites))}
	for _, sp := range linkSpritesInOrder(sprites) {
		b = append(b, sp.b[0], sp.b[1], sp.b[2], sp.b[3], sp.b[4]&3)
	}
	_, err := w.Write(b)
	return err
}

// SerializeSpriteChrs writes MsgSpriteChrs: `u8 count`, then per binding `u8 chr, u8 n, u8 factNum, u16 factTile`:
// chrs [chr, chr+n) show tiles [factTile, factTile+n) of the fact. Only the rows of Link's chrs that the sprites show
// are bound.
func SerializeSpriteChrs(w io.Writer, sprites []linkSprite, tiles *[0x20]uint16) error {
	var used [0x20]bool
	for _, sp := range sprites {
		if sp.b[3]&1 != 0 {
			continue
		}
		chr := uint16(sp.b[2])
		chrs := []uint16{chr}
		if sp.size != 0 {
			chrs = append(chrs, chr+0x01, chr+0x10, chr+0x11)
		}
		for _, c := range chrs {
			if c < 0x20 {
				used[c] = true
			}
		}
	}
	var entries []byte
	n := 0
	for _, r := range linkChrRows {
		any := false
		for k := uint8(0); k < r.n; k++ {
			any = any || used[r.chr+k]
		}
		if !any || tiles[r.chr] == noFactTile {
			continue
		}
		entries = append(entries, r.chr, r.n, factLinkGraphics, uint8(tiles[r.chr]), uint8(tiles[r.chr]>>8))
		n++
	}
	if n == 0 {
		return nil
	}
	_, err := w.Write(append([]byte{byte(MsgSpriteChrs), uint8(n)}, entries...))
	return err
}

// SerializeSpritePalettes writes MsgSpritePalettes: `u8 mask`, then the 32 bytes of each sprite palette in mask.
func (g *Game) SerializeSpritePalettes(w io.Writer, send [8]bool) error {
	s := &g.linkSprites
	b := []byte{byte(MsgSpritePalettes), 0}
	for k := range send {
		if send[k] {
			b[1] |= 1 << k
			b = append(b, s.palettes[k][:]...)
		}
	}
	if b[1] == 0 {
		return nil
	}
	_, err := w.Write(b)
	return err
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
