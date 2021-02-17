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

func imm16(v uint16) (byte, byte) {
	return byte(v), byte(v >> 8)
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

func (a *Assembler) LDA_imm16(m uint16) {
	d := make([]byte, 3)
	d[0] = 0xA9
	d[1], d[2] = imm16(m)
	a.write(d)
}

func (a *Assembler) STA_long(addr uint32) {
	d := make([]byte, 4)
	d[0] = 0x8F
	d[1], d[2], d[3] = imm24(addr)
	a.write(d)
}

func (a *Assembler) REP(c uint8) {
	a.write([]byte{0xC2, c})
}

func (a *Assembler) SEP(c uint8) {
	a.write([]byte{0xE2, c})
}

func (a *Assembler) JML(addr uint32) {
	d := make([]byte, 4)
	d[0] = 0x5C
	d[1], d[2], d[3] = imm24(addr)
	a.write(d)
}
