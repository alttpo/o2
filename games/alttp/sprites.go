package alttp

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/mapping/lorom"
	"io"
	"log"
	"o2/snes"
)

// Link sprite sync (outgoing only)
//
// alttpo clients draw remote players from the OAM sprites they broadcast (message 0x03). Link's body sprites are drawn
// from "unique tiles": 8x8 4bpp tiles identified by their index into Link's graphics in ROM (LinkGraphics, bank $10).
// Each frame the game DMAs the tiles for Link's current pose from ROM to VRAM using the source addresses at
// $0ACC/$0AD0/$0AD4 (head, body, aux; top row) and $0ACE/$0AD2/$0AD6 (bottom row). A tile's unique index is its
// offset from the start of LinkGraphics divided by 32. Each unique tile is sent once (message 0x15); receivers that
// lack one ask for it again with a NAK (message 0x16).
//
// alttpo reads tiles from emulated VRAM; o2 reads them straight from its in-memory copy of the ROM, which also picks up
// custom Link sprites patched into the ROM. Sword and shield tiles are rebuilt from ROM the same way the game builds
// them in WRAM (see swordshield.go). Other sprites whose tiles are only in VRAM are not sent since o2 cannot read VRAM.
// Link's shadow is sent without tile data, the same as alttpo does: receivers draw it with their own shadow tiles.
//
// o2 does not draw remote players, so incoming sprites and unique tiles are ignored.

// linkOAMCount is the number of OAM entries reserved for Link, starting at OAM index ($0352 >> 2):
const linkOAMCount = 12

// linkUniqTileCount is the number of LinkGraphics tiles that alttpo treats as unique tiles ($000..$37F):
const linkUniqTileCount = 0x380

// uniqTilesPerPacket keeps uniqtile packets well under a UDP packet size limit:
const uniqTilesPerPacket = 24

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
	// oamStart is the OAM index Link's sprites start at as of the last read; used to target the next OAM read:
	oamStart uint8

	// bank of LinkGraphics in ROM:
	tileBank   uint8
	tileBankOK bool

	// sprites sampled this frame and not yet sent:
	sprites []linkSprite
	fresh   bool

	// sprite palettes 5 (sword, shield) through 7 (Link):
	palettes [3][32]byte

	// unique tiles already broadcast and tiles waiting to be broadcast:
	sent    [swordShieldTileEnd]bool
	pending []uint16
}

func (g *Game) initSpriteSync() {
	s := &g.linkSprites
	s.oamStart = 0x64

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

// enqueueSpriteReads adds the WRAM reads needed to sample Link's sprites. $0352 is read as part of another read.
func (g *Game) enqueueSpriteReads(q []snes.Read) []snes.Read {
	start := uint32(g.linkSprites.oamStart)
	// Link's OAM entries in the OAM buffer:
	q = g.readEnqueue(q, 0xF50800+start<<2, linkOAMCount<<2, nil)
	// Link's OAM extended attributes (size, x bit 8) through Link's graphics DMA source addresses ($0ACC..$0AD7):
	q = g.readEnqueue(q, 0xF50A20+start, uint8(0x0AD8-0x0A20-start), nil)
	return q
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

	src := uint32(g.wramU16(ptr + chrY<<1))
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
	src := uint32(g.wramU16(ptr)) + x<<5
	if src < swordShieldWRAMStart || src >= swordShieldWRAMEnd {
		return 0, false
	}
	idx := uint16(swordShieldTileFirst + (src-swordShieldWRAMStart)>>5)
	if _, ok := g.swordShieldTile(idx); !ok {
		return 0, false
	}
	return idx, true
}

func isLinkShadowChr(chr uint16) bool {
	return (chr >= 0x6C && chr <= 0x6F) || (chr >= 0x7C && chr <= 0x7F)
}

// readLinkSprites samples Link's sprites from the WRAM just read. Called once per game frame while syncing.
func (g *Game) readLinkSprites() {
	s := &g.linkSprites
	if !s.tileBankOK {
		return
	}

	start := uint8(g.wramU16(0x0352) >> 2)
	oamAddr := 0x0800 + uint32(s.oamStart)<<2
	if !g.wramFresh[0x0352] || !g.wramFresh[oamAddr] || !g.wramFresh[0x0A20+uint32(s.oamStart)] || !g.wramFresh[0x0AD7] {
		return
	}
	if start != s.oamStart {
		// Link's OAM entries moved; read from the new location next time:
		s.oamStart = start
		return
	}

	// rebuild sword and shield tiles if the player's equipment changed; receivers need the changed tiles again:
	if g.wramFresh[0xF359] && g.wramFresh[0xF35A] {
		for _, idx := range g.updateSwordShieldGfx(g.wram[0xF359], g.wram[0xF35A]) {
			s.sent[idx] = false
		}
	}

	s.sprites = s.sprites[:0]
	for j := uint8(0); j < linkOAMCount; j++ {
		i := (start + j) & 0x7F
		o := 0x0800 + uint32(i)<<2
		sp := linkSprite{index: i}
		copy(sp.b[:4], g.wram[o:o+4])
		sp.b[4] = g.wram[0x0A20+uint32(i)] & 0x03
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
			for _, idx := range sp.absidx[:len(chrs)] {
				if !s.sent[idx] {
					s.sent[idx] = true
					s.pending = append(s.pending, idx)
				}
			}
		}

		s.sprites = append(s.sprites, sp)
	}

	if g.wramFresh[0xC6A0] {
		for k := range s.palettes {
			o := 0xC6A0 + k<<5
			copy(s.palettes[k][:], g.wram[o:o+32])
		}
	}
	s.fresh = true
}

// sendLinkSprites broadcasts any new unique tiles and the sprites sampled this frame.
func (g *Game) sendLinkSprites() {
	s := &g.linkSprites

	// send unique tiles first so receivers have them by the time they draw the sprites:
	for len(s.pending) > 0 {
		n := len(s.pending)
		if n > uniqTilesPerPacket {
			n = uniqTilesPerPacket
		}
		m := g.makeBroadcastMessage()
		if err := g.SerializeUniqTiles(m, s.pending[:n]); err != nil {
			log.Printf("alttp: sprites: %v\n", err)
			s.pending = s.pending[:0]
			break
		}
		g.send(m)
		s.pending = s.pending[n:]
	}

	if !s.fresh {
		return
	}
	s.fresh = false

	m := g.makeBroadcastMessage()
	g.SerializeLinkSprites(m, s.sprites)
	g.send(m)
}

// SerializeUniqTiles writes message 0x15 with the tile data for the given unique tile indices.
func (g *Game) SerializeUniqTiles(w io.Writer, absidx []uint16) (err error) {
	if err = binary.Write(w, binary.LittleEndian, uint8(MsgUniqTiles)); err != nil {
		return
	}
	if err = binary.Write(w, binary.LittleEndian, uint8(len(absidx))); err != nil {
		return
	}
	for _, idx := range absidx {
		tile, ok := g.linkTile(idx)
		if !ok {
			return fmt.Errorf("uniqtile %#03x not in ROM", idx)
		}
		if err = binary.Write(w, binary.LittleEndian, idx); err != nil {
			return
		}
		if _, err = w.Write(tile); err != nil {
			return
		}
	}
	return
}

// SerializeLinkSprites writes message 0x03 in alttpo's format. Tile data is never sent inline; each of sprite palettes
// 5-7 is sent along with the first sprite that uses it.
func (g *Game) SerializeLinkSprites(w io.Writer, sprites []linkSprite) {
	s := &g.linkSprites
	_ = binary.Write(w, binary.LittleEndian, uint8(MsgSprites1))
	_ = binary.Write(w, binary.LittleEndian, uint8(len(sprites)))

	var paletteSent [8]bool
	for _, sp := range sprites {
		b4 := sp.b[4]
		sendPalette := !paletteSent[sp.pal] && sp.pal >= 5
		if sendPalette {
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
		if sendPalette {
			_, _ = w.Write(s.palettes[sp.pal-5][:])
		}
	}
}

// DeserializeNakUniqTiles handles a request from another player to re-send unique tiles they are missing.
func (g *Game) DeserializeNakUniqTiles(p *Player, r io.Reader) (err error) {
	var playerIndex uint16
	if playerIndex, err = readU16(r); err != nil {
		return
	}
	var count uint8
	if count, err = readU8(r); err != nil {
		return
	}
	s := &g.linkSprites
	forUs := g.local != nil && int(playerIndex) == g.local.Index()
	for i := 0; i < int(count); i++ {
		var idx uint16
		if idx, err = readU16(r); err != nil {
			return
		}
		if !forUs || idx >= swordShieldTileEnd || !s.tileBankOK {
			continue
		}
		// queue for re-sending if not already queued:
		queued := false
		for _, q := range s.pending {
			if q == idx {
				queued = true
				break
			}
		}
		if !queued {
			s.sent[idx] = true
			s.pending = append(s.pending, idx)
		}
	}
	return
}
