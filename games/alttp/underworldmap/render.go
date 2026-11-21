package underworldmap

import (
	"encoding/binary"
	"image"
	"image/color"
)

// RoomPixels is the size of a room in pixels: 64x64 tiles of 8x8.
const RoomPixels = 512

// roomGfx is what draws a room: its two 64x64 tilemaps, the BG tiles in VRAM and the palette.
type roomGfx struct {
	bg1, bg2 [0x1000]uint16
	// BG1 and BG2's 4bpp tiles, from their base address in VRAM:
	chr1, chr2 []byte
	cgram      [0x100]uint16
	// the room's BG2 effect ($0414): 0 none, 3 BG2 drawn over BG1, 4 and 7 BG2 translucent:
	effect uint8
}

func (e *emu) readRoomGfx() *roomGfx {
	w := e.sys.WRAM[:]
	g := &roomGfx{effect: w[0x0414]}
	for i := range g.bg1 {
		g.bg1[i] = binary.LittleEndian.Uint16(w[0x2000+i*2:])
		g.bg2[i] = binary.LittleEndian.Uint16(w[0x4000+i*2:])
	}
	// the palette the room fades in to ($7EC300), as $7EC500 is still faded out to black after Module06:
	for i := range g.cgram {
		g.cgram[i] = binary.LittleEndian.Uint16(w[0xC300+i*2:])
	}
	// BG1 and BG2's tiles are at VRAM word $2000 in the underworld (the game sets BG12NBA to $22 in startup code
	// the emulator doesn't run):
	chr := append([]byte(nil), e.ppu.VRAM[0x4000:]...)
	g.chr1, g.chr2 = chr, chr
	return g
}

func bgr15(c uint16) color.NRGBA {
	r, g, b := c&0x1F, c>>5&0x1F, c>>10&0x1F
	return color.NRGBA{R: uint8(r<<3 | r>>2), G: uint8(g<<3 | g>>2), B: uint8(b<<3 | b>>2), A: 0xFF}
}

// layer is a BG drawn to 512x512 palette indices, 0 where transparent.
type layer [RoomPixels * RoomPixels]uint8

// drawBG draws the tiles of tilemap bg with priority prio into l.
func (l *layer) drawBG(bg *[0x1000]uint16, chr []byte, prio bool) {
	for t, z := range bg {
		// vhopppcc cccccccc:
		if (z&0x2000 != 0) != prio {
			continue
		}
		c := int(z & 0x03FF)
		pal := uint8(z>>10&7) << 4
		tx, ty := (t&0x3F)<<3, (t>>6)<<3
		base := c << 5
		if base+32 > len(chr) {
			continue
		}
		for y := 0; y < 8; y++ {
			p0, p1 := chr[base+y*2], chr[base+y*2+1]
			p2, p3 := chr[base+16+y*2], chr[base+16+y*2+1]
			fy := y
			if z&0x8000 != 0 {
				fy = 7 - y
			}
			row := (ty + fy) * RoomPixels
			for x := 0; x < 8; x++ {
				i := (p0>>x)&1 | ((p1>>x)&1)<<1 | ((p2>>x)&1)<<2 | ((p3>>x)&1)<<3
				if i == 0 {
					continue
				}
				fx := 7 - x
				if z&0x4000 != 0 {
					fx = x
				}
				l[row+tx+fx] = pal | i
			}
		}
	}
}

// render draws the room into dst at (x0, y0), RoomPixels square.
func (g *roomGfx) render(dst *image.NRGBA, x0, y0 int) {
	var pal [0x100]color.NRGBA
	for i, c := range g.cgram {
		pal[i] = bgr15(c)
	}

	// the main screen has BG1 over BG2, by priority, except that effect 3 puts BG2 over BG1; effects 4 and 7 have
	// BG2 on the subscreen added to (7) or averaged with (4) the main screen:
	main, sub := new(layer), new(layer)
	switch g.effect {
	case 4, 7:
		main.drawBG(&g.bg1, g.chr1, false)
		main.drawBG(&g.bg1, g.chr1, true)
		sub.drawBG(&g.bg2, g.chr2, false)
		sub.drawBG(&g.bg2, g.chr2, true)
	case 3:
		main.drawBG(&g.bg1, g.chr1, true)
		main.drawBG(&g.bg1, g.chr1, false)
		main.drawBG(&g.bg2, g.chr2, true)
		main.drawBG(&g.bg2, g.chr2, false)
	default:
		main.drawBG(&g.bg2, g.chr2, false)
		main.drawBG(&g.bg1, g.chr1, false)
		main.drawBG(&g.bg2, g.chr2, true)
		main.drawBG(&g.bg1, g.chr1, true)
	}

	for y := 0; y < RoomPixels; y++ {
		o := dst.PixOffset(x0, y0+y)
		for x := 0; x < RoomPixels; x++ {
			c := pal[main[y*RoomPixels+x]]
			if g.effect == 4 || g.effect == 7 {
				if s := sub[y*RoomPixels+x]; s != 0 {
					d := pal[s]
					if g.effect == 4 {
						c.R, c.G, c.B = uint8((int(c.R)+int(d.R))/2), uint8((int(c.G)+int(d.G))/2), uint8((int(c.B)+int(d.B))/2)
					} else {
						c.R, c.G, c.B = sat(int(c.R)+int(d.R)), sat(int(c.G)+int(d.G)), sat(int(c.B)+int(d.B))
					}
				}
			}
			dst.Pix[o+0], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = c.R, c.G, c.B, 0xFF
			o += 4
		}
	}
}

func sat(v int) uint8 {
	if v > 0xFF {
		return 0xFF
	}
	return uint8(v)
}
