package alttp

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"o2/snes"
	"o2/snes/asm"
	"strings"
)

const (
	preMainLen  = 8
	// SRAM address of preMain routine called nearly every frame before `JSL GameModes`
	preMainAddr = uint32(0x718000 - preMainLen)
	preMainUpdateAAddr = uint32(0x717C00)
	preMainUpdateBAddr = uint32(0x717E00)
)

type Patcher struct {
	rom *snes.ROM
	r   io.Reader
	w   io.Writer
}

func NewPatcher(rom *snes.ROM) *Patcher {
	return &Patcher{rom: rom}
}

// Patch patches the ROM for O2 support
func (p *Patcher) Patch() (err error) {
	// free bytes in JP 1.0 rom:
	// $00:89C2 - 30 bytes
	// $00:E892 - 30 bytes
	// $00:F7E1 - 31 bytes
	// $00:FFB7 - 9 bytes

	// $1B:B1D7 - 1577 bytes free
	// the last valid SPC data is at $1B:B1D3: dw 0, $0800
	// if you do go looking for that 5.8k free space, you'll see this sequence of bytes most likely
	// $C0, $00, $00, $00, $00, $01, $FF, $00, $00
	// I can assure you it's garbage

	// patch header to expand SRAM size:
	hdr := &p.rom.Header
	if hdr.RAMSize < 6 {
		// 1024 << 6 = 65536 bytes, aka 1 full bank in $70:0000
		hdr.RAMSize = 6
		if err = p.rom.WriteHeader(); err != nil {
			return
		}
	}

	// read from $00:802F which is where NMI should be enabled in the reset routine:
	p.readAt(0x00802F)
	var code802F []byte
	code802F, err = p.read(5)
	if err != nil {
		return
	}

	// this is what code at 802F should look like:
	expected802F := []byte{
		// CODE_00802F:	A9 81       LDA #$81   ;\ Enable NMI, Auto-Joypad read enable.
		// CODE_008031:	8D 00 42    STA $4200  ;/
		0xA9, 0x81,
		0x8D, 0x00, 0x42,
	}

	if !bytes.Equal(code802F, expected802F) {
		// let's at least check that it's a JSL followed by a NOP:
		if code802F[0] != 0x22 || code802F[4] != 0xEA {
			// it's not vanilla code nor is it a JSL / NOP combo:
			return fmt.Errorf("unexpected code at $00:802F: %s", hex.Dump(code802F))
		}
	}

	// overwrite $00:802F with `JSL $1BB1D7`
	p.writeAt(0x00802F)
	const initHook = 0x1BB1D7
	b := &bytes.Buffer{}
	textBuf := &strings.Builder{}
	defer func() { log.Print(textBuf.String()) }()

	var a asm.Emitter
	a.Code = b
	a.Text = textBuf
	a.SetBase(0x00802F)
	a.JSL(initHook)
	a.NOP()
	if b.Len() != len(expected802F) {
		return fmt.Errorf("assembler failed to produce exactly %d bytes to patch", len(expected802F))
	}
	if _, err = b.WriteTo(p.w); err != nil {
		return
	}

	// frame hook:
	const frameHook = 0x008056
	// 008056 is 22 B5 80 00   JSL GameModes
	p.readAt(frameHook)
	var frameJSL []byte
	frameJSL, err = p.read(4)
	if err != nil {
		return
	}
	if frameJSL[0] != 0x22 {
		return fmt.Errorf("frame hook $008056 does not contain a JSL instruction: %s", hex.Dump(frameJSL))
	}
	gameModes := frameJSL[1:]

	// we can't write to SRAM from this program because we only have access to the ROM contents,
	// so we have to write an ASM routine to initialize what we want in SRAM before we call it.
	// initialize the end of SRAM with the original JSL from the frameHook followed by RTL:

	// Build a temporary assembler to write the routine that gets written to SRAM:
	var ta asm.Emitter
	ta.Text = textBuf

	// assemble #`preMainLen` bytes of code:
	preMainBuf := &bytes.Buffer{}
	ta.Code = preMainBuf
	ta.SetBase(preMainAddr)
	ta.JSR_abs(0x7C00)
	ta.JSL_lhb(gameModes[0], gameModes[1], gameModes[2])
	ta.RTL()
	if preMainBuf.Len() != preMainLen {
		panic(fmt.Errorf("SRAM preMain assembled code length: %02x (actual) != %02x (expected)", preMainBuf.Len(), preMainLen))
	}

	// assemble the RTS instructions at the two A/B update routine locations:
	preMainUpdateABuf := &bytes.Buffer{}
	ta.Code = preMainUpdateABuf
	ta.SetBase(preMainUpdateAAddr)
	ta.RTS()
	ta.NOP() // to make an even number of code bytes so that 16-bit copies work nicely
	bufUpdateB := &bytes.Buffer{}
	ta.Code = bufUpdateB
	ta.SetBase(preMainUpdateBAddr)
	ta.RTS()
	ta.NOP() // to make an even number of code bytes so that 16-bit copies work nicely

	// start writing at the end of the ROM after music data:
	p.writeAt(initHook)
	a.SetBase(initHook)
	a.REP(0x20)
	p.asmCopyRoutine(preMainUpdateABuf.Bytes(), &a, preMainUpdateAAddr)
	p.asmCopyRoutine(bufUpdateB.Bytes(), &a, preMainUpdateBAddr)
	p.asmCopyRoutine(preMainBuf.Bytes(), &a, preMainAddr)
	a.SEP(0x20)
	// emit asm code:
	if _, err = b.WriteTo(p.w); err != nil {
		return
	}
	// append the original 802F code to our custom init hook:
	if err = p.write(code802F); err != nil {
		return
	}
	// follow by `RTL`
	a.RTL()
	if _, err = b.WriteTo(p.w); err != nil {
		return
	}

	// overwrite the frame hook with a JSL to the end of SRAM:
	p.writeAt(frameHook)
	a.SetBase(frameHook)
	a.JSL(preMainAddr)
	// emit asm code:
	if _, err = b.WriteTo(p.w); err != nil {
		return
	}

	return nil
}

func (p *Patcher) asmCopyRoutine(tc []byte, a *asm.Emitter, addr uint32) uint32 {
	// copy the assembled routine using LDA.w and STA.l instruction pairs, 16-bits at a time:
	for i := 0; i < len(tc); i += 2 {
		a.LDA_imm16_lh(tc[i], tc[i+1])
		a.STA_long(addr)
		addr += 2
	}
	return addr
}

func (p *Patcher) readAt(busAddr uint32) {
	p.r = p.rom.BusReader(busAddr)
}

func (p *Patcher) writeAt(busAddr uint32) {
	p.w = p.rom.BusWriter(busAddr)
}

func (p *Patcher) read(length int) (d []byte, err error) {
	d = make([]byte, length)
	t := 0
	for t < len(d) {
		var n int
		n, err = p.r.Read(d[t:])
		if err != nil {
			return
		}
		t += n
	}
	return
}

func (p *Patcher) write(d []byte) (err error) {
	t := 0
	for t < len(d) {
		var n int
		n, err = p.w.Write(d[t:])
		if err != nil {
			return
		}
		t += n
	}
	return
}
