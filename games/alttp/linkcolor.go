package alttp

import (
	"encoding/binary"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Player color (Game.SyncTunicColor)
//
// The game reloads Link's palette from ROM (or, in randomizers, from the custom sprite's bank) on every area and room
// transition, when Link's armor or gloves change, when the sprite is swapped, etc. Rather than chasing each of those
// with WRAM writes from o2, o2 writes the player's colors to SRAM at linkColorAddr once, and the trampoline (see
// emitLinkColor) puts them back into Link's palette on the frame the game reloaded it, before NMI uploads it to CGRAM.
//
// SRAM layout at linkColorAddr (zeroed on reset, so nothing is recolored until o2 writes it):
//
//	+0  light color (cap and tunic colors $A and $C) | $8000; the color is only applied while bit 15 is set
//	+2  dark color (cap and tunic colors $9 and $B)
const (
	linkColorAddr = uint32(0x7062F0)
	linkColorSize = 4

	linkColorEnabled = uint16(0x8000)

	// Link's palette (sprite palette 7) in the aux and main palette buffers. The main buffer is what NMI uploads to
	// CGRAM; palette fades and filters compute it from the aux buffer, which holds the colors the game loaded:
	linkPaletteAux  = uint32(0x7EC4E0)
	linkPaletteMain = uint32(0x7EC6E0)
	// AssertTranslucencySwap swaps Link's palette with sprite palette 0 while $0ABD is set (rooms with translucent
	// layers); Link is then drawn with palette 0:
	linkPaletteSwapOffs  = uint16(0x00E0)
	wramTranslucencySwap = uint32(0x7E0ABD)
)

// the color must be between the copy of Link's OAM entries and the capture mask, in the area zeroed on reset:
const _ = linkColorAddr - (linkOAMCopyAddr + linkOAMCopyReadSize)
const _ = captureMaskAddr - (linkColorAddr + linkColorSize)

// linkDarkColor makes the dark tunic and cap color from the light one: 75% as bright.
func linkDarkColor(light uint16) uint16 {
	return ((light & 31) * 3 / 4) |
		((((light >> 5) & 31) * 3 / 4) << 5) |
		((((light >> 10) & 31) * 3 / 4) << 10)
}

// emitLinkColor emits the trampoline's code that applies the colors at linkColorAddr to Link's palette after the
// module ran, in the same modules o2's update routines run in. The aux buffer's light color ($C) tells whether the
// game reloaded the palette since the colors were applied. The main buffer is only written when it showed the same
// color as the aux buffer, i.e. no fade or filter was in progress; a fade in progress computes the main buffer from
// the aux buffer anyway. Called with A and X 8-bit.
//
// Cycles (cycles_test.go): 60 with no colors to apply, including the module call before it; the path that applies them
// is not measured yet.
func emitLinkColor(a *asm.Emitter) {
	a.AssumeSEP(0x30)
	a.Comment("apply the player's color to Link's palette whenever the game reloaded it:")
	a.PHP()
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BCC("linkColorExit")
	a.CMP_imm8_b(0x19)
	a.BCS("linkColorExit")
	a.CMP_imm8_b(0x08)
	a.BEQ("linkColorExit")
	a.CMP_imm8_b(0x0A)
	a.BEQ("linkColorExit")
	a.CMP_imm8_b(0x14)
	a.BEQ("linkColorExit")

	a.Comment("X = offset of Link's palette from sprite palette 0:")
	a.LDA_long(wramTranslucencySwap)
	a.REP(0x30)
	a.LDX_imm16_w(linkPaletteSwapOffs)
	a.AND_imm16_w(0x00FF)
	a.BEQ("linkColorNoSwap")
	a.LDX_imm16_w(0x0000)
	a.Label("linkColorNoSwap")

	sp0Aux := linkPaletteAux - uint32(linkPaletteSwapOffs)
	sp0Main := linkPaletteMain - uint32(linkPaletteSwapOffs)

	a.LDA_long(linkColorAddr)
	a.BPL("linkColorExit")
	a.AND_imm16_w(^linkColorEnabled)
	a.CMP_long_x(sp0Aux + 0x0C<<1)
	a.BEQ("linkColorExit")

	a.Comment("the game reloaded the palette; update the main buffer too unless it is being faded:")
	a.LDA_long_x(sp0Main + 0x0C<<1)
	a.CMP_long_x(sp0Aux + 0x0C<<1)
	a.BNE("linkColorAux")
	a.LDA_long(linkColorAddr)
	a.AND_imm16_w(^linkColorEnabled)
	a.STA_long_x(sp0Main + 0x0A<<1)
	a.STA_long_x(sp0Main + 0x0C<<1)
	a.LDA_long(linkColorAddr + 2)
	a.STA_long_x(sp0Main + 0x09<<1)
	a.STA_long_x(sp0Main + 0x0B<<1)
	a.Comment("upload the palette in NMI:")
	a.SEP(0x20)
	a.INC_dp(0x15)
	a.REP(0x20)

	a.Label("linkColorAux")
	a.LDA_long(linkColorAddr)
	a.AND_imm16_w(^linkColorEnabled)
	a.STA_long_x(sp0Aux + 0x0A<<1)
	a.STA_long_x(sp0Aux + 0x0C<<1)
	a.LDA_long(linkColorAddr + 2)
	a.STA_long_x(sp0Aux + 0x09<<1)
	a.STA_long_x(sp0Aux + 0x0B<<1)

	a.Label("linkColorExit")
	a.PLP()
	a.AssumeSEP(0x30)
}

// linkColorPak is the FX Pak Pro address of the colors in SRAM:
var linkColorPak = func() uint32 {
	pak, err := lorom.BusAddressToPak(linkColorAddr)
	if err != nil {
		panic(err)
	}
	return pak
}()

// enqueueLinkColorRead reads the colors the game applies from SRAM.
func (g *Game) enqueueLinkColorRead(q []snes.Read) []snes.Read {
	return g.readEnqueue(q, linkColorPak, linkColorSize, nil)
}

// readLinkColorResponse keeps the colors read from SRAM. Reads that may have been issued before the last routine that
// wrote them ran are ignored.
func (g *Game) readLinkColorResponse(rsp snes.Response) {
	if rsp.Address != linkColorPak || len(rsp.Data) != linkColorSize {
		return
	}
	if g.linkColorSkipReads > 0 {
		g.linkColorSkipReads--
		return
	}
	g.linkColorSRAM = [2]uint16{
		binary.LittleEndian.Uint16(rsp.Data[0:2]),
		binary.LittleEndian.Uint16(rsp.Data[2:4]),
	}
	g.linkColorFresh = true
}

// linkColorWanted is what o2 wants the colors in SRAM to be: the local player's color, or none.
func (g *Game) linkColorWanted() [2]uint16 {
	if !g.SyncTunicColor {
		return [2]uint16{}
	}
	light := g.LocalPlayer().PlayerColor & 0x7FFF
	return [2]uint16{light | linkColorEnabled, linkDarkColor(light)}
}

// generateLinkColorUpdate emits a routine that writes the wanted colors to SRAM if the colors last read from SRAM are
// different. Called with A 16-bit.
func (g *Game) generateLinkColorUpdate(a *asm.Emitter) (updated bool, commit func()) {
	if !g.linkColorFresh {
		return false, nil
	}
	want := g.linkColorWanted()
	have := g.linkColorSRAM
	if have[0]&linkColorEnabled == 0 && want[0]&linkColorEnabled == 0 {
		// neither applies colors:
		return false, nil
	}
	if have == want {
		return false, nil
	}

	a.Comment("set the player's color for the trampoline to apply to Link's palette:")
	a.LDA_imm16_w(want[0])
	a.STA_long(linkColorAddr)
	a.LDA_imm16_w(want[1])
	a.STA_long(linkColorAddr + 2)
	return true, func() {
		// wait for a read issued after this routine ran:
		g.linkColorFresh = false
		g.linkColorSkipReads = 2
	}
}
