package alttp

import (
	"github.com/alttpo/snes/asm"
	"log"
	"testing"
)

func TestGame_initSync(t *testing.T) {
	a := asm.NewEmitter(make([]byte, 0x200), true)

	a.SetBase(0x707c00)

	{
		a.Comment("check if in HC overworld:")
		a.SEP(0x30)

		// check if in dungeon:
		a.LDA_dp(0x1B)
		a.BNE_imm8(0x6F - 0x06) // exit
		// check if in HC overworld:
		a.LDA_dp(0x8A)
		a.CMP_imm8_b(0x1B)
		a.BNE_imm8(0x6F - 0x0C) // exit

		a.Comment("find free sprite slot:")
		a.LDX_imm8_b(0x0f)      //   LDX   #$0F
		_ = 0                   // loop:
		a.LDA_abs_x(0x0DD0)     //   LDA.w $0DD0,X
		a.BEQ_imm8(0x05)        //   BEQ   found
		a.DEX()                 //   DEX
		a.BPL_imm8(-8)          //   BPL   loop
		a.BRA_imm8(0x6F - 0x18) //   BRA   exit
		_ = 0                   // found:

		a.Comment("open portal at HC:")
		// Y:
		a.LDA_imm8_b(0x50)
		a.STA_abs_x(0x0D00)
		a.LDA_imm8_b(0x08)
		a.STA_abs_x(0x0D20)
		// X:
		a.LDA_imm8_b(0xe0)
		a.STA_abs_x(0x0D10)
		a.LDA_imm8_b(0x07)
		a.STA_abs_x(0x0D30)
		// zeros:
		a.STZ_abs_x(0x0D40)
		a.STZ_abs_x(0x0D50)
		a.STZ_abs_x(0x0D60)
		a.STZ_abs_x(0x0D70)
		a.STZ_abs_x(0x0D80)
		// gfx?
		a.LDA_imm8_b(0x01)
		a.STA_abs_x(0x0D90)
		// hitbox/persist:
		a.STA_abs_x(0x0F60)
		// zeros:
		a.STZ_abs_x(0x0DA0)
		a.STZ_abs_x(0x0DB0)
		a.STZ_abs_x(0x0DC0)
		// active
		a.LDA_imm8_b(0x09)
		a.STA_abs_x(0x0DD0)
		// zeros:
		a.STZ_abs_x(0x0DE0)
		a.STZ_abs_x(0x0DF0)
		a.STZ_abs_x(0x0E00)
		a.STZ_abs_x(0x0E10)
		// whirlpool
		a.LDA_imm8_b(0xBA)
		a.STA_abs_x(0x0E20)
		// zeros:
		a.STZ_abs_x(0x0E30)
		// harmless
		a.LDA_imm8_b(0x80)
		a.STA_abs_x(0x0E40)
		// OAM:
		a.LDA_imm8_b(0x04)
		a.STA_abs_x(0x0F50)
		// exit:
		a.REP(0x30)
	}

	a.WriteTextTo(log.Writer())
}
