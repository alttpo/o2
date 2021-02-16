package asm

import (
	"bytes"
)

// 65816 immediate assembler
type Assembler struct {
	bytes.Buffer
}

func NewAssembler() *Assembler {
	return &Assembler{}
}

func (a *Assembler) write(d []byte) {
	_, _ = a.Write(d)
}

func (a *Assembler) writeByte(d byte) {
	_ = a.WriteByte(d)
}

func imm24(v uint32) (byte, byte, byte) {
	return byte(v), byte(v >> 8), byte(v >> 16)
}

func (a *Assembler) JSL(addr uint32) {
	d := make([]byte, 4)
	d[0] = 0x22
	d[1], d[2], d[3] = imm24(addr)
	a.write(d)
}

func (a *Assembler) RTL() {
	a.writeByte(0x6B)
}

func (a *Assembler) NOP() {
	a.writeByte(0xEA)
}
