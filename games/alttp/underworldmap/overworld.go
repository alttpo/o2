package underworldmap

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"runtime"
	"sync"
)

// The overworld's areas
//
// The overworld is drawn area by area with the same emulator as the underworld: each area is loaded the way the
// debug page's warp loads it, by the game's own module $08 (LoadOverworldFromDungeon) from the state the game keeps of
// the overworld while Link is in a cave ($7EC140..; see AreaPlacement), so that drawing the map also runs the warp's
// way into each area. The area's map16 tiles ($7E2000) are then drawn with the graphics and palettes the module loaded,
// and its tile attributes (GetOverworldTileType) give where Link can be (AreaWalk).
//
// Only the state the game starts a new save file in is drawn: the overlays of events (the bombed walls, the pyramid's
// hole, the opened entrances) and the overlay of BG1 (the forest's canopy) are not.

const (
	// AreaCount is the number of overworld screens: the light world $00..$3F and the dark world $40..$7F.
	AreaCount = 0x80
	// AreaPixels is the size of a small area; a large area is twice as wide and tall.
	AreaPixels = 0x200
)

// areaLarge tells which screens of a world are part of a large area of 2x2 screens (OverworldScreenSizeFlag); the light
// world and the dark world are laid out the same.
var areaLarge = [64]uint8{
	1, 1, 0, 1, 1, 1, 1, 0,
	1, 1, 0, 1, 1, 1, 1, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 0, 1, 1, 0, 1, 1,
	1, 1, 0, 1, 1, 0, 1, 1,
	0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 0, 0, 0, 1, 1, 0,
	1, 1, 0, 0, 0, 1, 1, 0,
}

// AreaParent returns the area a screen is part of: the screen itself if it is small, else the top left screen of its
// large area.
func AreaParent(screen uint8) uint8 {
	world, s := screen&0x40, int(screen&0x3F)
	r, c := s>>3, s&7
	if areaLarge[s] == 0 {
		return screen
	}
	top := r / 3 * 3
	start := c
	for start > 0 && areaLarge[r*8+start-1] != 0 {
		start--
	}
	left := start + (c-start)&^1
	return world | uint8(top*8+left)
}

// AreaIsLarge reports whether the area is a large one, 1024 pixels square.
func AreaIsLarge(area uint8) bool { return areaLarge[area&0x3F] != 0 }

// AreaSize is the size of an area in pixels.
func AreaSize(area uint8) int {
	if AreaIsLarge(area) {
		return 2 * AreaPixels
	}
	return AreaPixels
}

// AreaOrigin is where the area starts in the overworld's coordinates, which are the same in both worlds.
func AreaOrigin(area uint8) (x, y uint16) {
	return uint16(area&7) * AreaPixels, uint16(area>>3&7) * AreaPixels
}

// Areas are the areas of the overworld, light world then dark world: the parents of the screens.
func Areas() (areas []uint8) {
	for s := 0; s < AreaCount; s++ {
		if AreaParent(uint8(s)) == uint8(s) {
			areas = append(areas, uint8(s))
		}
	}
	return
}

// AreaPlacement is the state of the overworld that has Link at a point of an area: what the game keeps of it while
// Link is in a cave or house, in $7EC140.. (Dungeon_LoadEntrance), and puts back when he leaves it
// (LoadCachedEntranceProperties). The camera follows Link as it would once he walks: the screen is centered on him
// as far as the area lets it scroll, the camera's triggers are where he is then, and the map16 window the game keeps
// in VRAM and the counters that load its next row and column are in step with the scroll, as in the exit table
// (UnderworldExitData) for most exits.
type AreaPlacement struct {
	Area uint8
	// Link's coordinates ($22, $20) and the scroll ($E2, $E8), in the overworld's:
	X, Y             uint16
	ScrollX, ScrollY uint16
	// the camera's triggers, camera_x_coord_scroll_low ($061C) and camera_y_coord_scroll_low ($0618):
	CameraX, CameraY uint16
	// map16_load_src_off ($84): the map16 tile (row*$80 + column*2, in the area) the VRAM tilemap's window is kept
	// from:
	Map16 uint16
	// the counters of the pixels scrolled up and left since the last row and column of map16 were loaded
	// (overworld_unk1 $0624 and overworld_unk3 $0628; those down and right are their negatives):
	UnkY, UnkX uint16
}

// overworld scroll limits past an area's origin, for small and large areas (Overworld_SetCameraBoundaries'
// kOverworld_Size1 and kOverworld_Size2):
var (
	scrollLimitY = [2]uint16{0x11E, 0x31E}
	scrollLimitX = [2]uint16{0x100, 0x300}
)

// PlaceInArea returns the placement of Link at x, y (the area's pixels) of the area.
func PlaceInArea(area uint8, x, y uint16) AreaPlacement {
	ox, oy := AreaOrigin(area)
	big := 0
	if AreaIsLarge(area) {
		big = 1
	}
	clamp := func(v, lo, hi int) uint16 {
		if v < lo {
			v = lo
		} else if v > hi {
			v = hi
		}
		return uint16(v)
	}
	p := AreaPlacement{Area: area, X: ox + x, Y: oy + y}
	// the camera follows Link (Overworld_OperateCameraScroll) while x + 8 is within 2 pixels left of
	// camera_x_coord_scroll_low and y + 12 within 2 above camera_y_coord_scroll_low:
	p.ScrollX = clamp(int(p.X)-0x76, int(ox), int(ox+scrollLimitX[big]))
	p.ScrollY = clamp(int(p.Y)-0x60, int(oy), int(oy+scrollLimitY[big]))
	p.CameraX = p.ScrollX + 0x7F
	p.CameraY = p.ScrollY + 0x6D
	// the next row and column load when the scroll gets to the next multiple of 16 pixels up and left:
	p.UnkX = -p.ScrollX & 0xF
	p.UnkY = -p.ScrollY & 0xF
	col := (p.ScrollX - ox + p.UnkX) >> 4
	row := (p.ScrollY - oy + p.UnkY) >> 4
	p.Map16 = row<<7 | col<<1
	return p
}

// cache are the words of the game's overworld state while Link is in a cave ($7EC140..$7EC171), by offset, that
// LoadCachedEntranceProperties reads; the others it reads are set again after it.
//
// Its room must be one of the caves $0100..$0123 but Link's house ($0104), for which it takes $10 off Link's Y, so
// that the warp's area plays the overworld's music (PreOverworld_LoadProperties plays the music of the dungeon in
// the room's low byte otherwise): it is $0100 (AreaWarpRoom).
func (p AreaPlacement) cache() map[uint16]uint16 {
	return map[uint16]uint16{
		0x00: uint16(p.Area), // overworld_area_index ($040A)
		0x02: 0x0016,         // TM, TS: set again by Overworld_LoadOverlays2
		0x04: p.ScrollY,
		0x06: p.ScrollX,
		0x08: p.Y + 0x10 - ExitWalkDown,
		0x0A: p.X,
		0x0C: uint16(p.Area), // overworld_screen_index ($8A)
		0x0E: p.Map16,
		0x10: p.CameraY,
		0x12: p.CameraX,
		0x2A: p.UnkY,
		0x2C: -p.UnkY,
		0x2E: p.UnkX,
		0x30: -p.UnkX,
	}
}

// ExitWalkDown is how far Link walks down out of the cave's door once the screen is open (Module09_0A_WalkFromExiting_
// FacingDown: 16 steps of a pixel, then 3), which the cache's Y takes off so that he ends at the point.
const ExitWalkDown = 19

// AreaWarpRoom is the room LoadOverworldFromDungeon is left from to the cached overworld state: see
// AreaPlacement.cache.
const AreaWarpRoom = 0x0100

// AreaCacheBase is where the game keeps its overworld state while Link is in a cave.
const AreaCacheBase = 0x7EC140

// CacheWords returns the words to write at AreaCacheBase+offset for the placement, in increasing offsets.
func (p AreaPlacement) CacheWords() (offsets []uint16, values []uint16) {
	c := p.cache()
	for o := uint16(0); o < 0x40; o += 2 {
		if v, ok := c[o]; ok {
			offsets = append(offsets, o)
			values = append(values, v)
		}
	}
	return
}

// Area is an overworld area drawn.
type Area struct {
	Area uint8
	// Size is its size in pixels:
	Size int
	Walk *AreaWalk
	// Loaded is where the game put Link and the screen after loading the area from AreaPlacement: the same as asked,
	// unless the warp's way into areas is wrong.
	Placed, Loaded AreaPlacement
}

// OverworldOptions control GenerateOverworld.
type OverworldOptions struct {
	Logger io.Writer
	// OnArea is called with each area as soon as it is drawn, from one of several goroutines at once. img is only
	// valid during the call.
	OnArea func(a *Area, img *image.NRGBA)
}

// overworldTables are the ROM's tables to read the areas' tiles: the map16 tiles' map8 tiles (Map16Definitions, 4
// words per tile: top left, top right, bottom left, bottom right) and the map8 tiles' attributes
// (OverworldTileTypes), found by the code of GetOverworldTileType that reads them,
// `LDA.l Map16Definitions,X : STA.b $06 : AND.w #$01FF : TAX : LDA.l OverworldTileTypes,X`.
type overworldTables struct {
	map16, attrs uint32
}

func findOverworldTables(rom []byte) (t overworldTables, ok bool) {
	for i := 0; i+14 <= len(rom); i++ {
		b := rom[i:]
		if b[0] == 0xBF && b[4] == 0x85 && b[5] == 0x06 && b[6] == 0x29 && b[7] == 0xFF && b[8] == 0x01 && b[9] == 0xAA && b[10] == 0xBF {
			t.map16 = uint32(b[1]) | uint32(b[2])<<8 | uint32(b[3])<<16
			t.attrs = uint32(b[11]) | uint32(b[12])<<8 | uint32(b[13])<<16
			return t, true
		}
	}
	return t, false
}

// exitTable finds UnderworldExitData by the code of LoadOverworldFromUnderworld that searches it,
// `LDX.b #$9E : .next : DEX : DEX : CMP.w UnderworldExitData.room_id,X : BNE .next`, in the same bank.
func exitTable(rom []byte) (addr uint32, ok bool) {
	for i := 0; i+8 <= len(rom); i++ {
		b := rom[i:]
		if b[0] == 0xA2 && b[1] == 0x9E && b[2] == 0xCA && b[3] == 0xCA && b[4] == 0xDD && b[7] == 0xD0 {
			return uint32(i>>15)<<16 | uint32(b[5]) | uint32(b[6])<<8, true
		}
	}
	return 0, false
}

// ExitCount is the number of exits of UnderworldExitData.
const ExitCount = 79

// OverworldExit is where an exit of the exit table puts Link in the overworld.
type OverworldExit struct {
	Room uint16
	Area uint8
	// the exit's Link X and Y, scroll, map16 load offset, camera triggers and scroll counters:
	X, Y, ScrollX, ScrollY, Map16, CameraX, CameraY uint16
	UnkY, UnkX                                      int8
}

// OverworldExits reads the exit table of rom.
func OverworldExits(rom []byte) ([]OverworldExit, error) {
	if len(rom)&0x7FFF == 0x200 {
		rom = rom[0x200:]
	}
	base, ok := exitTable(rom)
	if !ok {
		return nil, fmt.Errorf("underworldmap: no exit table in the ROM")
	}
	o := int(romOffset(base))
	const n = ExitCount
	if o+n*21 > len(rom) {
		return nil, fmt.Errorf("underworldmap: exit table out of the ROM")
	}
	w := func(k, i int) uint16 { return binary.LittleEndian.Uint16(rom[o+k+i*2:]) }
	kArea := n * 2
	kMap16 := kArea + n
	kScrollY := kMap16 + 2*n
	kScrollX := kScrollY + 2*n
	kY := kScrollX + 2*n
	kX := kY + 2*n
	kCamY := kX + 2*n
	kCamX := kCamY + 2*n
	kUnkY := kCamX + 2*n
	kUnkX := kUnkY + n
	exits := make([]OverworldExit, n)
	for i := range exits {
		exits[i] = OverworldExit{
			Room: w(0, i), Area: rom[o+kArea+i],
			Map16: w(kMap16, i), ScrollY: w(kScrollY, i), ScrollX: w(kScrollX, i),
			Y: w(kY, i), X: w(kX, i), CameraY: w(kCamY, i), CameraX: w(kCamX, i),
			UnkY: int8(rom[o+kUnkY+i]), UnkX: int8(rom[o+kUnkX+i]),
		}
	}
	return exits, nil
}

// areaGfx is what draws an area: its map16 tiles, BG tiles and palette.
type areaGfx struct {
	size  int
	map16 [0x1000]uint16
	chr   []byte
	cgram [0x100]uint16
	defs  []uint16
}

// GenerateOverworld draws every area of the overworld from rom, as Generate does the underworld.
func GenerateOverworld(rom []byte, opts OverworldOptions) ([]*Area, error) {
	if len(rom)&0x7FFF == 0x200 {
		rom = rom[0x200:]
	}
	a, err := addressesFor(rom)
	if err != nil {
		return nil, err
	}
	tables, ok := findOverworldTables(rom)
	if !ok {
		return nil, fmt.Errorf("underworldmap: no overworld tile tables in the ROM")
	}
	exits, err := OverworldExits(rom)
	if err != nil {
		return nil, err
	}
	logf := func(format string, args ...any) {
		if opts.Logger != nil {
			_, _ = fmt.Fprintf(opts.Logger, format, args...)
		}
	}

	e, err := newEmu(rom, a)
	if err != nil {
		return nil, err
	}
	// the map16 tiles' map8 tiles and the attributes of the map8 tiles, as words:
	defs := make([]uint16, 0x4000)
	for i := range defs {
		defs[i] = e.rom16(tables.map16 + uint32(i)*2)
	}
	var attrs [0x200]uint8
	for i := range attrs {
		attrs[i] = e.sys.ROM[romOffset(tables.attrs+uint32(i))]
	}

	draws := make(chan struct {
		area *Area
		gfx  *areaGfx
	}, runtime.GOMAXPROCS(0))
	var drawing sync.WaitGroup
	for i := 0; i < cap(draws); i++ {
		drawing.Add(1)
		go func() {
			defer drawing.Done()
			for d := range draws {
				img := d.gfx.render()
				if opts.OnArea != nil {
					opts.OnArea(d.area, img)
				}
			}
		}()
	}
	defer func() {
		close(draws)
		drawing.Wait()
	}()

	var areas []*Area
	for _, area := range Areas() {
		size := AreaSize(area)
		p := PlaceInArea(area, uint16(size/2), uint16(size/2))
		if err = e.loadArea(p); err != nil {
			return nil, fmt.Errorf("underworldmap: area $%02x: %w", area, err)
		}
		ar := &Area{Area: area, Size: size, Placed: p, Loaded: e.areaPlacement()}
		// the module ends before Link walks out of the door:
		ar.Loaded.Y += ExitWalkDown
		if ar.Loaded != p {
			logf("area $%02x: loaded at %+v; placed at %+v\n", area, ar.Loaded, p)
		}

		g := &areaGfx{size: size, defs: defs}
		w := e.sys.WRAM[:]
		for i := range g.map16 {
			g.map16[i] = binary.LittleEndian.Uint16(w[0x2000+i*2:])
		}
		for i := range g.cgram {
			g.cgram[i] = binary.LittleEndian.Uint16(w[0xC300+i*2:])
		}
		g.chr = append([]byte(nil), e.ppu.VRAM[0x4000:]...)

		var seeds []areaSeed
		ox, oy := AreaOrigin(area)
		for _, x := range exits {
			if x.Area < 0x80 && AreaParent(x.Area) == area && x.X >= ox && x.Y >= oy {
				seeds = append(seeds, areaSeed{x: x.X - ox, y: x.Y - oy})
			}
		}
		ar.Walk = computeAreaWalk(size, &g.map16, defs, &attrs, seeds)
		logf("area $%02x: %d exits\n", area, len(seeds))
		areas = append(areas, ar)
		draws <- struct {
			area *Area
			gfx  *areaGfx
		}{ar, g}
	}
	return areas, nil
}

// loadArea loads the area as the debug page's warp does: from the overworld state the game keeps while Link is in a
// cave, by module $08.
func (e *emu) loadArea(p AreaPlacement) error {
	e.restore()
	w := e.sys.WRAM[:]
	offs, vals := p.CacheWords()
	for i, o := range offs {
		e.setWRAM16(AreaCacheBase-0x7E0000+uint32(o), vals[i])
	}
	e.setWRAM16(0xA0, AreaWarpRoom)
	e.setWRAM16(0x0696, 0)
	e.setWRAM16(0x0698, 0)
	w[0xF3CA] = p.Area & 0x40
	return e.call(e.pcLoadArea)
}

// areaPlacement reads where the game put Link and the screen.
func (e *emu) areaPlacement() AreaPlacement {
	return AreaPlacement{
		Area:    e.wram8(0x8A),
		X:       e.wram16(0x22),
		Y:       e.wram16(0x20),
		ScrollX: e.wram16(0xE2),
		ScrollY: e.wram16(0xE8),
		CameraX: e.wram16(0x061C),
		CameraY: e.wram16(0x0618),
		Map16:   e.wram16(0x84),
		UnkY:    e.wram16(0x0624),
		UnkX:    e.wram16(0x0628),
	}
}

// render draws the area's map16 tiles, which are BG2's in the overworld.
func (g *areaGfx) render() *image.NRGBA {
	n := g.size / 8
	tiles := make([]uint16, n*n)
	for row := 0; row < n/2; row++ {
		for col := 0; col < n/2; col++ {
			m := int(g.map16[row*64+col])
			for k := 0; k < 4; k++ {
				tiles[(row*2+k>>1)*n+col*2+k&1] = g.defs[(m*4+k)&0x3FFF]
			}
		}
	}
	var pal [0x100][4]uint8
	for i, c := range g.cgram {
		p := bgr15(c)
		pal[i] = [4]uint8{p.R, p.G, p.B, 0xFF}
	}
	idx := make([]uint8, g.size*g.size)
	for _, prio := range []bool{false, true} {
		drawTiles(idx, g.size, tiles, n, g.chr, prio)
	}
	img := image.NewNRGBA(image.Rect(0, 0, g.size, g.size))
	for i, c := range idx {
		copy(img.Pix[i*4:], pal[c][:])
	}
	return img
}

// drawTiles draws the n x n 8x8 tiles (vhopppcc cccccccc) of priority prio into the size x size palette indices.
func drawTiles(dst []uint8, size int, tiles []uint16, n int, chr []byte, prio bool) {
	for t, z := range tiles {
		if (z&0x2000 != 0) != prio {
			continue
		}
		c := int(z & 0x03FF)
		pal := uint8(z>>10&7) << 4
		tx, ty := (t%n)<<3, (t/n)<<3
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
			row := (ty + fy) * size
			for x := 0; x < 8; x++ {
				i := (p0>>x)&1 | ((p1>>x)&1)<<1 | ((p2>>x)&1)<<2 | ((p3>>x)&1)<<3
				if i == 0 {
					continue
				}
				fx := 7 - x
				if z&0x4000 != 0 {
					fx = x
				}
				dst[row+tx+fx] = pal | i
			}
		}
	}
}
