package asm

// Assembler represents a 65816 immediate assembler
type Assembler interface {
	FlagsTracker

	REP(c uint8)
	SEP(c uint8)
	NOP()
	JSL(addr uint32)
	JML(addr uint32)
	RTL()
	LDA_imm8(m uint8)
	LDA_imm16(m uint16)
	STA_long(addr uint32)
}
