package underworldmap

import (
	"github.com/alttpo/snes/emulator/cpu65c816"
)

// Loading a room spends most of its time decompressing the dungeon's graphics and loading them into VRAM, the same
// for every room of the dungeon. hookRoutines caches what those routines do: patchROM puts a WDM in place of an
// instruction at their entries and exits, whose numbers here the CPU's OnWDM dispatches.
const (
	wdmDecompressEnter = 0x10 + iota
	wdmDecompressExit
	wdmTilesetsEnter
	wdmTilesetsExit
)

// cacheRoutines turns the caches of hookRoutines on; tests turn it off to check they change nothing.
var cacheRoutines = true

// decompressed is the result of a Decompress call.
type decompressed struct {
	data []byte
	// the source pointer ($C8) after it:
	end uint32
}

type decompressCall struct {
	src, dst uint32
}

func (e *emu) dp24(addr uint32) uint32 {
	w := e.sys.WRAM[:]
	return uint32(w[addr]) | uint32(w[addr+1])<<8 | uint32(w[addr+2])<<16
}

// wramOffset converts a bus address in banks $7E and $7F to its offset in WRAM.
func wramOffset(addr uint32) (uint32, bool) {
	if addr>>17 != 0x7E>>1 {
		return 0, false
	}
	return addr & 0x1FFFF, true
}

// jumpFromWDM has the CPU go on at pc after the WDM it is running, which advances the PC by its 2 bytes after.
func (e *emu) jumpFromWDM(pc uint32) {
	e.sys.SetPC(pc - 2)
}

func (e *emu) hookRoutines() {
	e.decompressed = make(map[uint32]*decompressed)
	e.tilesets = make(map[string]*routineEffects)
	c := &e.sys.CPU
	c.OnWDM = func(n byte) {
		switch n {
		case wdmDecompressEnter:
			// REP #$10:
			c.SetFlags(c.Flags() &^ 0x10)
			e.decompressEnter()
		case wdmDecompressExit:
			e.decompressExit()
			// SEP #$10:
			c.SetFlags(c.Flags() | 0x10)
		case wdmTilesetsEnter:
			e.tilesetsEnter()
		case wdmTilesetsExit:
			e.tilesetsExit()
			// PLB : RTL:
			e.jumpFromWDM(e.pcPLBRTL)
		}
	}
}

// decompressEnter is at the start of Decompress, which decompresses the data at the 24-bit pointer in $C8 to the
// 24-bit pointer in $00. Data in ROM decompresses the same every time, so a cached result is written to WRAM in
// place of running Decompress, which returns through decompressRet as it would.
func (e *emu) decompressEnter() {
	src, dst := e.dp24(0xC8), e.dp24(0x00)
	e.decompressing = nil
	off, ok := wramOffset(dst)
	if !ok || !cacheRoutines {
		return
	}
	if _, inWRAM := wramOffset(src); inWRAM || src&0x40_0000 != 0 {
		return
	}
	d, ok := e.decompressed[src]
	if !ok || int(off)+len(d.data) > len(e.sys.WRAM) {
		e.decompressing = &decompressCall{src: src, dst: dst}
		return
	}
	copy(e.sys.WRAM[off:], d.data)
	if e.recording != nil {
		for i := range d.data {
			e.recording.mark(off + uint32(i))
		}
	}
	w := e.sys.WRAM[:]
	w[0xC8], w[0xC9], w[0xCA] = byte(d.end), byte(d.end>>8), byte(d.end>>16)
	ry := e.decompressRetY - scratchBase
	e.scratch00[ry], e.scratch00[ry+1] = byte(len(d.data)), byte(len(d.data)>>8)
	e.scratch00[e.decompressRetX-scratchBase] = byte(d.end)
	e.jumpFromWDM(e.pcDecompressRet)
}

// decompressExit is at the end of Decompress, where Y is the size of its output.
func (e *emu) decompressExit() {
	call := e.decompressing
	e.decompressing = nil
	if call == nil || e.dp24(0x00) != call.dst {
		return
	}
	off, _ := wramOffset(call.dst)
	n := uint32(e.sys.CPU.RY)
	if int(off+n) > len(e.sys.WRAM) {
		return
	}
	e.decompressed[call.src] = &decompressed{
		data: append([]byte(nil), e.sys.WRAM[off:off+n]...),
		end:  e.dp24(0xC8),
	}
}

// tilesetsKey is what InitializeTilesets depends on: the graphics sets $0AA1, $0AA2 and $0AA4 (patchROM has it skip
// the sprite sheets of $0AA3).
func (e *emu) tilesetsKey() string {
	w := e.sys.WRAM[:]
	return string([]byte{w[0x0AA1], w[0x0AA2], w[0x0AA4]})
}

// tilesetsEnter is in InitializeTilesets after its PHB : PHK : PLB. Its effects are cached by tilesetsKey: a
// cached call puts them back and ends the routine. It rewrites all of the BG graphics in VRAM, which is all of VRAM
// the map uses, so restoring all of VRAM after it is as good as running it, whatever was there before.
func (e *emu) tilesetsEnter() {
	e.recording = nil
	if !cacheRoutines {
		return
	}
	k := e.tilesetsKey()
	fx, ok := e.tilesets[k]
	if !ok {
		e.recording = &routineRecording{key: k, dirty: make(map[uint32]struct{}, 0x4000)}
		return
	}
	for i, at := range fx.wramAt {
		e.sys.WRAM[at] = fx.wramVal[i]
	}
	copy(e.ppu.VRAM[:], fx.vram)
	fx.regs.restore(&e.sys.CPU)
	e.jumpFromWDM(e.pcPLBRTL)
}

// tilesetsExit is at the end of InitializeTilesets, before its PLB : RTL.
func (e *emu) tilesetsExit() {
	r := e.recording
	e.recording = nil
	if r == nil {
		return
	}
	fx := &routineEffects{
		vram: append([]byte(nil), e.ppu.VRAM[:]...),
		regs: saveRegs(&e.sys.CPU),
	}
	for at := range r.dirty {
		fx.wramAt = append(fx.wramAt, at)
		fx.wramVal = append(fx.wramVal, e.sys.WRAM[at])
	}
	e.tilesets[r.key] = fx
}

// trackedWRAM is WRAM mapped on the bus that notes what is written to it while the emulator records a routine's
// effects. data starts at the start of WRAM.
type trackedWRAM struct {
	e      *emu
	data   []byte
	offset uint32
}

func (m trackedWRAM) Read(address uint32) byte { return m.data[address-m.offset] }

func (m trackedWRAM) Write(address uint32, value byte) {
	i := address - m.offset
	m.data[i] = value
	if r := m.e.recording; r != nil {
		r.mark(i)
	}
}

func (m trackedWRAM) Shutdown()                  {}
func (m trackedWRAM) Size() uint32               { return uint32(len(m.data)) }
func (m trackedWRAM) Clear()                     {}
func (m trackedWRAM) Dump(address uint32) []byte { return nil }

// attachWRAM maps WRAM ($7E0000..$7FFFFF and its first $2000 bytes in banks $00..$3F and $80..$BF) as trackedWRAM,
// over what CreateEmulator mapped.
func (e *emu) attachWRAM() error {
	w := e.sys.WRAM[:]
	if err := e.sys.Bus.Attach(trackedWRAM{e: e, data: w, offset: 0x7E_0000}, "wram", 0x7E_0000, 0x7F_FFFF); err != nil {
		return err
	}
	for _, b := range []uint32{0x00, 0x80} {
		for bank := b; bank < b+0x40; bank++ {
			start := bank << 16
			if err := e.sys.Bus.Attach(trackedWRAM{e: e, data: w[:0x2000], offset: start}, "wram", start, start|0x1FFF); err != nil {
				return err
			}
		}
	}
	return nil
}

// routineRecording collects the WRAM a routine writes while it runs.
type routineRecording struct {
	key   string
	dirty map[uint32]struct{}
}

func (r *routineRecording) mark(i uint32) { r.dirty[i] = struct{}{} }

// routineEffects are what a routine did: the WRAM it wrote, all of VRAM after it, and the registers at its end.
type routineEffects struct {
	wramAt  []uint32
	wramVal []byte
	vram    []byte
	regs    cpuRegs
}

// cpuRegs are the CPU's registers, copied as they are kept.
type cpuRegs struct {
	ra, rx, ry         uint16
	rah, ral, rxl, ryl byte
	rdbr               byte
	rd                 uint16
	n, v, m, x, d, i   byte
	z, c, b, e         byte
}

func saveRegs(c *cpu65c816.CPU) cpuRegs {
	return cpuRegs{
		ra: c.RA, rx: c.RX, ry: c.RY,
		rah: c.RAh, ral: c.RAl, rxl: c.RXl, ryl: c.RYl,
		rdbr: c.RDBR, rd: c.RD,
		n: c.N, v: c.V, m: c.M, x: c.X, d: c.D, i: c.I,
		z: c.Z, c: c.C, b: c.B, e: c.E,
	}
}

func (r *cpuRegs) restore(c *cpu65c816.CPU) {
	c.RA, c.RX, c.RY = r.ra, r.rx, r.ry
	c.RAh, c.RAl, c.RXl, c.RYl = r.rah, r.ral, r.rxl, r.ryl
	c.RDBR, c.RD = r.rdbr, r.rd
	c.N, c.V, c.M, c.X, c.D, c.I = r.n, r.v, r.m, r.x, r.d, r.i
	c.Z, c.C, c.B, c.E = r.z, r.c, r.b, r.e
}
