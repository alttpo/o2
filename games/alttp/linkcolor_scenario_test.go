package alttp

import "testing"

const linkColorGreen = 0x12EF

func (h *skScenario) sramU16(i int, addr uint32) uint16 {
	o := addr - 0x700000
	s := h.gs[i].e.SRAM
	return uint16(s[o]) | uint16(s[o+1])<<8
}

// reloadLinkPalette writes vanilla green colors $9..$C to Link's palette in both buffers, like the game does when it
// reloads the palette from ROM, at sprite palette offs (7 normally, 0 during the translucency swap):
func (h *skScenario) reloadLinkPalette(i int, offs uint32) {
	for _, base := range []uint32{0xC400, 0xC600} {
		for c := uint32(9); c <= 0xC; c++ {
			h.setWRAMU16(i, base+offs*0x20+c<<1, linkColorGreen)
		}
	}
}

func (h *skScenario) expectLinkColors(when string, i int, base uint32, light, dark uint16) {
	h.t.Helper()
	for c, want := range map[uint32]uint16{9: dark, 0xA: light, 0xB: dark, 0xC: light} {
		if got := h.wramU16(i, base+c<<1); got != want {
			h.t.Errorf("%s: $7E%04X color $%X: expected $%04x; got $%04x", when, base, c, want, got)
		}
	}
}

func TestLinkColorScenario(t *testing.T) {
	h := newSKScenario(t, 1)
	h.onOverworld(0, 0)
	g := h.gs[0].g
	g.SyncTunicColor = true
	g.local.PlayerColor = 0x5CD2
	dark := linkDarkColor(0x5CD2)
	h.frames(skSettleFrames)

	if got := h.sramU16(0, linkColorAddr); got != 0x5CD2|linkColorEnabled {
		t.Fatalf("SRAM light color: expected $%04x; got $%04x", 0x5CD2|linkColorEnabled, got)
	}
	h.expectLinkColors("applied", 0, 0xC4E0, 0x5CD2, dark)
	h.expectLinkColors("applied", 0, 0xC6E0, 0x5CD2, dark)

	// the game reloads the palette from ROM (e.g. an area transition); the patch puts the color back in the same frame
	// without o2 writing anything:
	h.reloadLinkPalette(0, 7)
	h.gs[0].e.WRAM[0x15] = 0
	h.frame()
	h.expectLinkColors("reloaded", 0, 0xC4E0, 0x5CD2, dark)
	h.expectLinkColors("reloaded", 0, 0xC6E0, 0x5CD2, dark)
	if h.gs[0].e.WRAM[0x15] == 0 {
		t.Errorf("reloaded: expected $15 to be set to upload the palette")
	}

	// a fade is in progress (the main buffer differs from the aux buffer): only the aux buffer is updated:
	h.reloadLinkPalette(0, 7)
	h.setWRAMU16(0, 0xC6E0+0xC<<1, 0x0001)
	h.frame()
	h.expectLinkColors("fading", 0, 0xC4E0, 0x5CD2, dark)
	if got := h.wramU16(0, 0xC6E0+0xC<<1); got != 0x0001 {
		t.Errorf("fading: expected the main buffer to be left alone; got $%04x", got)
	}

	// translucency swap: Link's palette is in sprite palette 0 and palette 7 holds palette 0's colors:
	h.reloadLinkPalette(0, 7)
	h.reloadLinkPalette(0, 0)
	h.gs[0].e.WRAM[0x0ABD] = 1
	h.frame()
	h.expectLinkColors("swapped", 0, 0xC400, 0x5CD2, dark)
	h.expectLinkColors("swapped", 0, 0xC600, 0x5CD2, dark)
	h.expectLinkColors("swapped", 0, 0xC4E0, linkColorGreen, linkColorGreen)
	h.gs[0].e.WRAM[0x0ABD] = 0

	// not applied outside the modules o2 updates in (e.g. the file select screen):
	h.reloadLinkPalette(0, 7)
	h.gs[0].e.WRAM[0x10] = 0x01
	h.frame()
	h.expectLinkColors("file select", 0, 0xC4E0, linkColorGreen, linkColorGreen)
	h.gs[0].e.WRAM[0x10] = 0x09

	// turning the option off stops applying the color:
	g.SyncTunicColor = false
	h.frames(skSettleFrames)
	if got := h.sramU16(0, linkColorAddr); got&linkColorEnabled != 0 {
		t.Fatalf("disabled: expected SRAM colors to be cleared; got $%04x", got)
	}
	h.reloadLinkPalette(0, 7)
	h.frame()
	h.expectLinkColors("disabled", 0, 0xC4E0, linkColorGreen, linkColorGreen)
}
