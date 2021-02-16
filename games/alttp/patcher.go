package alttp

import (
	"bytes"
	"fmt"
	"io"
	"o2/snes"
	"o2/snes/asm"
)

type Patcher struct {
	rom *snes.ROM
	r   io.Reader
	w   io.Writer
}

func NewPatcher(rom *snes.ROM) *Patcher {
	return &Patcher{rom: rom}
}

// patches the ROM for O2 support
func (p *Patcher) Patch() (err error) {
	var d []byte

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

	// read from $00:802F which is where NMI should be enabled at reset vector:
	p.readAt(0x00802F)
	d, err = p.read(5)
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
	if !bytes.Equal(d, expected802F) {
		return fmt.Errorf("unexpected code at $00:802F")
	}

	// overwrite $00:802F with `JSL $1BB1D7`
	p.writeAt(0x00802F)
	const initHook = 0x1BB1D7
	var a asm.Assembler
	a.JSL(initHook)
	a.NOP()
	if a.Len() != len(expected802F) {
		return fmt.Errorf("assembler failed to produce exactly %d bytes to patch", len(expected802F))
	}
	if _, err = a.WriteTo(p.w); err != nil {
		return
	}

	// start writing at the end of the ROM after music data:
	p.writeAt(initHook)

	// initialize the end of SRAM with a tiny routine:
	a.REP(0x20)
	a.LDA_imm16(0x006B) // RTL
	a.STA_long(0x70FFFC)
	a.SEP(0x20)
	if _, err = a.WriteTo(p.w); err != nil {
		return
	}

	// write the original 802F code to our custom init hook:
	if err = p.write(expected802F); err != nil {
		return
	}
	// follow by `RTL`
	a.RTL()
	if _, err = a.WriteTo(p.w); err != nil {
		return
	}

	return nil
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
