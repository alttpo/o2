// Package worldmap draws the light world and dark world maps of A Link to the Past, the Mode 7 maps the game shows
// when the player presses X on the overworld, straight from the game's ROM. It is a port of alttpo's
// WorldMapWindow.loadMap and drawMode7Map.
package worldmap

import (
	"fmt"
	"image"
	"image/color"
)

const (
	// the part of the 128x128 tile Mode 7 tilemap that is drawn, in 8x8 tiles:
	TileTop    = 4
	TileLeft   = 8
	TileWidth  = 48
	TileHeight = 57

	// the size of the map image in pixels:
	Width  = TileWidth * 8
	Height = TileHeight * 8

	// MaxBrightness is the brightest a map is drawn, as a luma of 0-255: as bright as alttpo's darkened underworld
	// map, so that the players' colored dots stand out:
	MaxBrightness = 140

	// OverworldLeft and OverworldTop are added to an overworld coordinate before it is divided by 16 to place it on
	// the map, before the map's own TileLeft*8 and TileTop*8 offsets are taken off:
	OverworldLeft = 2048
	OverworldTop  = 2132
)

// addresses are the SNES addresses of the world maps' data, which differ between versions of the game:
type addresses struct {
	tilemapLight uint32
	tilemapDark  uint32
	paletteLight uint32
	paletteDark  uint32
}

var (
	// US 1.0 and the European versions:
	addrUS = addresses{
		tilemapLight: 0x0AC727,
		tilemapDark:  0x0AD727,
		paletteLight: 0x0ADB27,
		paletteDark:  0x0ADC27,
	}
	// JP 1.0, which the randomizers are built on:
	addrJP = addresses{
		tilemapLight: 0x0AC739,
		tilemapDark:  0x0AD739,
		paletteLight: 0x0ADB39,
		paletteDark:  0x0ADC39,
	}
)

// the Mode 7 graphics of the world maps, from CopyMode7Chr in bank $00: 256 8bpp tiles of 64 bytes:
const gfxAddress = 0x18C000

// LoROM header fields, as offsets into the ROM:
const (
	headerDestination = 0x7FD9
)

// pc converts a LoROM SNES address to an offset into the ROM:
func pc(addr uint32) int {
	return int((addr&0x7F0000)>>1 | addr&0x7FFF)
}

type romReader []byte

func (r romReader) block(addr uint32, n int) ([]byte, error) {
	o := pc(addr)
	if o+n > len(r) {
		return nil, fmt.Errorf("worldmap: ROM is too small to read $%06x", addr)
	}
	return r[o : o+n], nil
}

// Generate draws the light world map, or the dark world map if dark is set, from rom, the contents of the ROM.
func Generate(rom []byte, dark bool) (*image.NRGBA, error) {
	// strip a copier header:
	if len(rom)&0x7FFF == 0x200 {
		rom = rom[0x200:]
	}
	if len(rom) < 0x8000 {
		return nil, fmt.Errorf("worldmap: ROM is too small")
	}
	a := &addrUS
	if rom[headerDestination] == 0x00 {
		a = &addrJP
	}
	r := romReader(rom)

	gfx, err := r.block(gfxAddress, 0x4000)
	if err != nil {
		return nil, err
	}

	// the light world's tilemap is in 4 quadrants of 32x32 tiles, from NMI_LightWorldMode7Tilemap in bank $00:
	var tilemap [0x4000]uint8
	src, err := r.block(a.tilemapLight, 0x1000)
	if err != nil {
		return nil, err
	}
	p := 0
	for _, dest := range []int{0x0000, 0x0020, 0x1000, 0x1020} {
		for row := 0; row < 0x20; row++ {
			copy(tilemap[dest:dest+0x20], src[p:p+0x20])
			dest += 0x80 // the width of the Mode 7 tilemap (128x128 tiles)
			p += 0x20
		}
	}

	paletteAddr := a.paletteLight
	if dark {
		// the dark world replaces the middle 32x32 tiles of the light world's tilemap:
		src, err = r.block(a.tilemapDark, 0x400)
		if err != nil {
			return nil, err
		}
		dest := 0x0810
		for row := 0; row < 0x20; row++ {
			copy(tilemap[dest:dest+0x20], src[row*0x20:row*0x20+0x20])
			dest += 0x80
		}
		paletteAddr = a.paletteDark
	}

	pb, err := r.block(paletteAddr, 0x200)
	if err != nil {
		return nil, err
	}
	var palette [0x100]uint16
	for i := range palette {
		palette[i] = uint16(pb[i*2]) | uint16(pb[i*2+1])<<8
	}

	darkenPalette(&palette, &tilemap, gfx)

	return draw(&tilemap, gfx, &palette), nil
}

// luma of a SNES color as 0-255:
func luma(c uint16) float64 {
	r := float64(c&31) * 255.0 / 31.0
	g := float64((c>>5)&31) * 255.0 / 31.0
	b := float64((c>>10)&31) * 255.0 / 31.0
	return 0.299*r + 0.587*g + 0.114*b
}

// darkenPalette scales the colors that the drawn part of the map uses so that the brightest is no brighter than
// MaxBrightness; the hues are kept. A map that is not brighter is left as it is.
func darkenPalette(palette *[0x100]uint16, tilemap *[0x4000]uint8, gfx []byte) {
	var used [0x100]bool
	for my := TileTop; my < TileTop+TileHeight; my++ {
		for mx := TileLeft; mx < TileLeft+TileWidth; mx++ {
			t := int(tilemap[my*128+mx])
			for _, c := range gfx[t*64 : t*64+64] {
				used[c] = true
			}
		}
	}

	brightest := 0.0
	for i, c := range palette {
		if !used[i] {
			continue
		}
		if l := luma(c); l > brightest {
			brightest = l
		}
	}
	if brightest <= MaxBrightness {
		return
	}

	scale := MaxBrightness / brightest
	for i, c := range palette {
		r := uint16(float64(c&31)*scale + 0.5)
		g := uint16(float64((c>>5)&31)*scale + 0.5)
		b := uint16(float64((c>>10)&31)*scale + 0.5)
		palette[i] = r | g<<5 | b<<10
	}
}

func bgr15(c uint16) color.NRGBA {
	r, g, b := c&0x1F, c>>5&0x1F, c>>10&0x1F
	return color.NRGBA{R: uint8(r<<3 | r>>2), G: uint8(g<<3 | g>>2), B: uint8(b<<3 | b>>2), A: 0xFF}
}

// draw draws the map's Mode 7 tiles:
func draw(tilemap *[0x4000]uint8, gfx []byte, palette *[0x100]uint16) *image.NRGBA {
	var colors [0x100]color.NRGBA
	for i, c := range palette {
		colors[i] = bgr15(c)
	}

	img := image.NewNRGBA(image.Rect(0, 0, Width, Height))
	for my := TileTop; my < TileTop+TileHeight; my++ {
		for mx := TileLeft; mx < TileLeft+TileWidth; mx++ {
			t := int(tilemap[my*128+mx])
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					img.SetNRGBA((mx-TileLeft)*8+x, (my-TileTop)*8+y, colors[gfx[t*64+y*8+x]])
				}
			}
		}
	}
	return img
}
