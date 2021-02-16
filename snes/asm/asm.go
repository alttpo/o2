package asm

import (
	"bytes"
	"io"
)

// 65816 immediate assembler
type Assembler struct {
	w bytes.Buffer
}

func NewAssembler() *Assembler {
	return &Assembler{}
}

func (a *Assembler) Reset() {
	a.w.Reset()
}

func (a *Assembler) WriteTo(w io.Writer) (err error) {
	_, err = a.w.WriteTo(w)
	return
}

func (a *Assembler) write(d []byte) {
	_, _ = a.w.Write(d)
}

func imm24(v uint32) (byte, byte, byte) {
	return byte(v >> 16), byte(v >> 8), byte(v)
}

func (a *Assembler) JSL(addr uint32) {
	d := make([]byte, 4)
	d[0] = 0x22
	d[1], d[2], d[3] = imm24(addr)
	a.write(d)
}
