package asm

import (
	"bytes"
	"fmt"
)

// nvmxdizc
type Flags uint8

const (
	Carry Flags = 1 << iota
	Zero
	IRQDisable
	DecimalMode
	IndexRegister8bit
	Accumulator8bit
	Overflow
	Negative
)

// 65816 immediate assembler
type Assembler struct {
	bytes.Buffer

	flags Flags
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

func (a *Assembler) IsM16bit() bool { return a.flags & Accumulator8bit == 0 }

func (a *Assembler) AssumeREP(c uint8) {
	a.flags &= ^Flags(c)
}

func (a *Assembler) AssumeSEP(c uint8) {
	a.flags |= Flags(c)
}

func (a *Assembler) REP(c uint8) {
	a.AssumeREP(c)
	a.write([]byte{0xC2, c})
}

func (a *Assembler) SEP(c uint8) {
	a.AssumeSEP(c)
	a.write([]byte{0xE2, c})
}

func (a *Assembler) NOP() {
	a.writeByte(0xEA)
}

func (a *Assembler) JSL(addr uint32) {
	d := make([]byte, 4)
	d[0] = 0x22
	d[1], d[2], d[3] = imm24(addr)
	a.write(d)
}

func (a *Assembler) JML(addr uint32) {
	d := make([]byte, 4)
	d[0] = 0x5C
	d[1], d[2], d[3] = imm24(addr)
	a.write(d)
}

func (a *Assembler) RTL() {
	a.writeByte(0x6B)
}

func (a *Assembler) LDA_imm8(m uint8) {
	if a.IsM16bit() {
		panic(fmt.Errorf("asm: LDA_imm8 called but 'm' flag is 16-bit; call SEP(0x20) or AssumeSEP(0x20) first"))
	}
	d := make([]byte, 2)
	d[0] = 0xA9
	d[1] = m
	a.write(d)
}

func (a *Assembler) LDA_imm16(m uint16) {
	if !a.IsM16bit() {
		panic(fmt.Errorf("asm: LDA_imm16 called but 'm' flag is 8-bit; call REP(0x20) or AssumeREP(0x20) first"))
	}
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
