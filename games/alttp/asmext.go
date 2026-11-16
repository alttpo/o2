package alttp

import (
	"fmt"

	"github.com/alttpo/snes/asm"
)

// emitReport stores the low byte of A (16-bit mode) as this generator's result for ConfirmAsmExecuted.
func emitReport(a *asm.Emitter, index uint32) {
	a.SEP(0x20)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x20)
}

// The helpers below emit 65816 instructions that github.com/alttpo/snes/asm does not provide yet. They are emitted as
// raw bytes, preceded by a comment with the mnemonic for the assembly listing.

func emitRaw(a *asm.Emitter, mnemonic string, b ...byte) {
	a.Comment(mnemonic)
	a.EmitBytes(b)
}

// emitSBCLong emits `SBC.l addr`.
func emitSBCLong(a *asm.Emitter, addr uint32) {
	emitRaw(a, fmt.Sprintf("sbc.l $%06x", addr), 0xEF, uint8(addr), uint8(addr>>8), uint8(addr>>16))
}

// emitSBCAbs emits `SBC.w addr`.
func emitSBCAbs(a *asm.Emitter, addr uint16) {
	emitRaw(a, fmt.Sprintf("sbc.w $%04x", addr), 0xED, uint8(addr), uint8(addr>>8))
}

// emitCMPAbs emits `CMP.w addr`.
func emitCMPAbs(a *asm.Emitter, addr uint16) {
	emitRaw(a, fmt.Sprintf("cmp.w $%04x", addr), 0xCD, uint8(addr), uint8(addr>>8))
}

// emitORAAbs emits `ORA.w addr`.
func emitORAAbs(a *asm.Emitter, addr uint16) {
	emitRaw(a, fmt.Sprintf("ora.w $%04x", addr), 0x0D, uint8(addr), uint8(addr>>8))
}

// emitORAAbsY emits `ORA.w addr,Y`.
func emitORAAbsY(a *asm.Emitter, addr uint16) {
	emitRaw(a, fmt.Sprintf("ora.w $%04x,Y", addr), 0x19, uint8(addr), uint8(addr>>8))
}

// emitLDAAbsY emits `LDA.w addr,Y`.
func emitLDAAbsY(a *asm.Emitter, addr uint16) {
	emitRaw(a, fmt.Sprintf("lda.w $%04x,Y", addr), 0xB9, uint8(addr), uint8(addr>>8))
}

// emitSTAAbsY emits `STA.w addr,Y`.
func emitSTAAbsY(a *asm.Emitter, addr uint16) {
	emitRaw(a, fmt.Sprintf("sta.w $%04x,Y", addr), 0x99, uint8(addr), uint8(addr>>8))
}

// emitCPYImm16 emits `CPY.w #imm`.
func emitCPYImm16(a *asm.Emitter, imm uint16) {
	emitRaw(a, fmt.Sprintf("cpy.w #$%04x", imm), 0xC0, uint8(imm), uint8(imm>>8))
}

// emitBITImm16 emits `BIT.w #imm`.
func emitBITImm16(a *asm.Emitter, imm uint16) {
	emitRaw(a, fmt.Sprintf("bit.w #$%04x", imm), 0x89, uint8(imm), uint8(imm>>8))
}

// emitEORImm8 emits `EOR.b #imm`.
func emitEORImm8(a *asm.Emitter, imm uint8) {
	emitRaw(a, fmt.Sprintf("eor.b #$%02x", imm), 0x49, imm)
}
