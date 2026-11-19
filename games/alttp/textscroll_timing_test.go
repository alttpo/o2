package alttp

import (
	"os"
	"testing"

	"github.com/alttpo/snes/emulator"
)

// the vanilla JP ROM, for timing the game's own routines:
const jpROMPath = "../../../alttp-jp.sfc"

// RenderText_Draw_Scroll on the JP ROM (US $0E:CFE2): scrolls the dialog's text up a pixel at a time, $1CEA + 1 times
// a frame (5 for the default speed 4), or fewer if a line's 16 pixels finish.
const jpRenderTextDrawScroll = 0x0E_F952

// How long a frame of scrolling dialog text spends in RenderText_Draw_Scroll, on the clocked emulator: the remote
// players drawn by remoteFrame disappear while dialog text scrolls, and this is the work that makes those frames late.
func TestTiming_RenderTextDrawScroll(t *testing.T) {
	romData, err := os.ReadFile(jpROMPath)
	if err != nil {
		t.Skipf("no JP ROM: %v", err)
	}
	for _, c := range []struct {
		name string
		// $1CDF, the pixels scrolled; a line is done when its low nibble wraps to 0:
		scrolled uint8
		passes   int
	}{
		{"5 passes (speed 4)", 0x01, 5},
		{"the last pass of a line", 0x0F, 1},
	} {
		ppu := &emulator.PPU{}
		e := &emulator.System{Devices: []emulator.Device{ppu}}
		if err := e.CreateEmulator(); err != nil {
			t.Fatal(err)
		}
		copy(e.ROM[:], romData)
		e.Bus.EaWrite(0x4200, 0x00)

		e.WRAM[0xF2] = 0x00
		e.WRAM[0x1CEA] = 0x04
		e.WRAM[0x1CDF] = c.scrolled
		// return with RTS to $0E:8000:
		e.CPU.Reset()
		e.CPU.SetFlags(0x34)
		e.CPU.SP = 0x01FD
		e.WRAM[0x01FE], e.WRAM[0x01FF] = 0xFF, 0x7F
		e.SetPC(jpRenderTextDrawScroll)
		ppu.V, ppu.HClock = 0, 0
		clocks, cycles := e.Clocks, e.CPU.AllCycles
		if !e.RunUntil(0x0E_8000, 200_000) {
			t.Fatalf("%s: did not return; PC=$%06x", c.name, e.GetPC())
		}
		clocks, cycles = e.Clocks-clocks, e.CPU.AllCycles-cycles
		if got := int(e.WRAM[0x1CDF] - c.scrolled); got != c.passes {
			t.Errorf("%s: scrolled %d pixels; expected %d", c.name, got, c.passes)
		}
		t.Logf("%s: %d cycles, %d master clocks (%.2f per cycle): ends on line %d, dot %d of the frame it started at the "+
			"top of (%d frame(s) later), without HDMA", c.name, cycles, clocks, float64(clocks)/float64(cycles), ppu.V,
			ppu.HDot(), ppu.Frame)
	}
}
