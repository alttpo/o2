package alttp

import (
	"github.com/alttpo/snes/asm"
)

// emitReport stores the low byte of A (16-bit mode) as this generator's result for ConfirmAsmExecuted.
func emitReport(a *asm.Emitter, index uint32) {
	a.SEP(0x20)
	a.STA_long(a.GetBase() + 0x02 + index)
	a.REP(0x20)
}
