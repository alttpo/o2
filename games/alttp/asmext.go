package alttp

import (
	"fmt"

	"github.com/alttpo/snes/asm"
)

// 65816 instructions not provided by github.com/alttpo/snes/asm, emitted as raw bytes. These are only used in 16-bit
// (REP #$30) code.

func imm16b(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }

func asmADC_imm16(a *asm.Emitter, v uint16) {
	a.Comment(fmt.Sprintf("adc.w #$%04x", v))
	a.EmitBytes(append([]byte{0x69}, imm16b(v)...))
}

func asmSBC_dp(a *asm.Emitter, dp uint8) {
	a.Comment(fmt.Sprintf("sbc.b $%02x", dp))
	a.EmitBytes([]byte{0xE5, dp})
}

func asmCMP_dp(a *asm.Emitter, dp uint8) {
	a.Comment(fmt.Sprintf("cmp.b $%02x", dp))
	a.EmitBytes([]byte{0xC5, dp})
}

func asmSEC(a *asm.Emitter) {
	a.Comment("sec")
	a.EmitBytes([]byte{0x38})
}

func asmINX(a *asm.Emitter) {
	a.Comment("inx")
	a.EmitBytes([]byte{0xE8})
}

func asmLSR(a *asm.Emitter) {
	a.Comment("lsr a")
	a.EmitBytes([]byte{0x4A})
}

func asmCPX_imm16(a *asm.Emitter, v uint16) {
	a.Comment(fmt.Sprintf("cpx.w #$%04x", v))
	a.EmitBytes(append([]byte{0xE0}, imm16b(v)...))
}

func asmEOR_imm16(a *asm.Emitter, v uint16) {
	a.Comment(fmt.Sprintf("eor.w #$%04x", v))
	a.EmitBytes(append([]byte{0x49}, imm16b(v)...))
}

func asmSTA_long_x(a *asm.Emitter, addr uint32) {
	a.Comment(fmt.Sprintf("sta.l $%06x,X", addr))
	a.EmitBytes([]byte{0x9F, byte(addr), byte(addr >> 8), byte(addr >> 16)})
}

func asmPEA(a *asm.Emitter, v uint16) {
	a.Comment(fmt.Sprintf("pea $%04x", v))
	a.EmitBytes(append([]byte{0xF4}, imm16b(v)...))
}

// emitJSRBank01 calls a routine in bank $01 that returns with RTS from code in any bank. It pushes our return address
// for an RTL and then the address of an RTL instruction in bank $01 (minus one) for the routine's RTS; the routine's
// RTS returns to that RTL which returns to us. Registers are passed through untouched.
func emitJSRBank01(a *asm.Emitter, routine, rtl uint32) {
	// PHK + PEA + PEA + JML = 11 bytes; the return address is the instruction after the JML:
	ret := a.PC() + 11
	a.PHK()
	asmPEA(a, uint16(ret-1))
	asmPEA(a, uint16(rtl-1))
	a.JML(routine)
	if a.PC() != ret {
		panic(fmt.Errorf("emitJSRBank01: expected return address $%06x but PC is $%06x", ret, a.PC()))
	}
}

// emitReport stores the low byte of A (16-bit mode) as this generator's result for ConfirmAsmExecuted.
func emitReport(a *asm.Emitter, index uint32) {
	a.SEP(0x20)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x20)
}

// asmBNEFar branches to label when Z is clear, without the 8-bit range limit of BNE; local must be a unique label.
func asmBNEFar(a *asm.Emitter, label, local string) {
	a.BEQ(local)
	a.JMP_abs(label)
	a.Label(local)
}
