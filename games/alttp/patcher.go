package alttp

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"o2/snes"
	"o2/snes/asm"
	"o2/snes/lorom"
	"strings"
)

const (
	preMainLen = 8
	// SRAM address of preMain routine called nearly every frame before `JSL GameModes`
	preMainAddr        = uint32(0x708000 - preMainLen)
	preMainUpdateAAddr = uint32(0x707C00)
	preMainUpdateBAddr = uint32(0x707E00)
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
	if hdr.RAMSize < 5 {
		// 1024 << 5 = 32768 bytes, aka $70:0000-7FFF
		hdr.RAMSize = 5
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

	textBuf := &strings.Builder{}
	defer func() {
		log.Print(textBuf.String())
	}()

	// overwrite $00:802F with `JSL $1BB1D7`
	const initHook = 0x1BB1D7

	a := asm.NewEmitter(p.rom.Slice(lorom.BusAddressToPC(0x00_802F), uint32(len(expected802F))), true)
	a.SetBase(0x00802F)
	a.JSL(initHook)
	a.NOP()
	a.Finalize()
	a.WriteTextTo(textBuf)
	if a.Len() != len(expected802F) {
		return fmt.Errorf("assembler failed to produce exactly %d bytes to patch", len(expected802F))
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
	taMain := asm.NewEmitter(make([]byte, 0x200), true)
	taMain.SetBase(preMainAddr)
	taMain.JSR_abs(0x7C00)
	taMain.JSL_lhb(gameModes[0], gameModes[1], gameModes[2])
	taMain.RTL()
	taMain.WriteTextTo(textBuf)
	if taMain.Len() != preMainLen {
		panic(fmt.Errorf("SRAM preMain assembled code length: %02x (actual) != %02x (expected)", taMain.Len(), preMainLen))
	}

	// assemble the RTS instructions at the two A/B update routine locations:
	taUpdateA := asm.NewEmitter(make([]byte, 0x200), true)
	taUpdateA.SetBase(preMainUpdateAAddr)
	taUpdateA.RTS()
	taUpdateA.NOP() // to make an even number of code bytes so that 16-bit copies work nicely
	taUpdateA.WriteTextTo(textBuf)
	if taUpdateA.Len()%2 != 0 {
		panic(fmt.Errorf("SRAM updateA assembled code length %#02x must be aligned to 16-bits", taUpdateA.Len()))
	}

	taUpdateB := asm.NewEmitter(make([]byte, 0x200), true)
	taUpdateB.SetBase(preMainUpdateBAddr)
	taUpdateB.RTS()
	taUpdateB.NOP() // to make an even number of code bytes so that 16-bit copies work nicely
	taUpdateB.WriteTextTo(textBuf)
	if taUpdateB.Len()%2 != 0 {
		panic(fmt.Errorf("SRAM updateB assembled code length %#02x must be aligned to 16-bits", taUpdateB.Len()))
	}

	// start writing at the end of the ROM after music data:
	a = asm.NewEmitter(p.rom.Slice(lorom.BusAddressToPC(initHook), 0x1C_0000-initHook), true)
	a.SetBase(initHook)
	a.REP(0x20)
	p.asmCopyRoutine(taUpdateA.Bytes(), a, preMainUpdateAAddr)
	p.asmCopyRoutine(taUpdateB.Bytes(), a, preMainUpdateBAddr)
	p.asmCopyRoutine(taMain.Bytes(), a, preMainAddr)
	a.SEP(0x20)
	// append the original 802F code to our custom init hook:
	a.EmitBytes(code802F)
	// follow by `RTL`
	a.RTL()
	a.WriteTextTo(textBuf)

	// overwrite the frame hook with a JSL to the end of SRAM:
	a = asm.NewEmitter(p.rom.Slice(lorom.BusAddressToPC(frameHook), 4), true)
	a.SetBase(frameHook)
	a.JSL(preMainAddr)
	a.WriteTextTo(textBuf)

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
