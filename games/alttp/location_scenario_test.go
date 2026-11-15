package alttp

import (
	"testing"
)

// setScroll sets Link's coordinates and the BG2 scroll offsets in the player's WRAM.
func (h *skScenario) setScroll(i int, x, y, xoffs, yoffs uint16) {
	e := h.gs[i].e
	e.WRAM[0x22], e.WRAM[0x23] = uint8(x), uint8(x>>8)
	e.WRAM[0x20], e.WRAM[0x21] = uint8(y), uint8(y>>8)
	e.WRAM[0xE2], e.WRAM[0xE3] = uint8(xoffs), uint8(xoffs>>8)
	e.WRAM[0xE8], e.WRAM[0xE9] = uint8(yoffs), uint8(yoffs>>8)
}

func (h *skScenario) expectRemoteLocation(when string, i, remote int, location uint32, x, y uint16, xoffs, yoffs int16) {
	h.t.Helper()
	p := &h.gs[i].g.players[remote]
	if p.Location != location || p.X != x || p.Y != y || p.XOffs != xoffs || p.YOffs != yoffs {
		h.t.Errorf(
			"%s: g%d sees g%d at location $%06x (%d,%d) offs (%d,%d); expected $%06x (%d,%d) offs (%d,%d)",
			when, i+1, remote+1,
			p.Location, p.X, p.Y, p.XOffs, p.YOffs,
			location, x, y, xoffs, yoffs,
		)
	}
}

// alttpo draws a remote player's OAM sprites offset by the difference between both players' scroll offsets, so the
// scroll offsets and Link's coordinates must keep updating while the screen scrolls during an underworld room
// transition. The location must stay on the old room until the transition finishes, like alttpo does.
func TestLocationScenario_UnderworldRoomTransition(t *testing.T) {
	h := newSKScenario(t, 2)
	h.inRoom(0, rmRoom)
	h.inRoom(1, rmRoom)
	h.setScroll(0, 0x0778, 0x0E30, 0x0700, 0x0E00)
	h.setScroll(1, 0x0740, 0x0E40, 0x0700, 0x0E00)
	h.frames(4)
	h.expectRemoteLocation("in room", 1, 0, 0x010000|uint32(rmRoom), 0x0778, 0x0E30, 0x0700, 0x0E00)

	// g1 walks through the east doorway into the next room; the screen scrolls over during module $07/$02:
	e := h.gs[0].e
	e.WRAM[0xA0], e.WRAM[0xA1] = uint8(rmOtherRoom), uint8(rmOtherRoom>>8)
	e.WRAM[skSubModule] = 0x02
	for k := uint16(1); k <= 4; k++ {
		h.setScroll(0, 0x0778+k*2, 0x0E30, 0x0700+k*0x40, 0x0E00)
		h.frames(2)
		h.expectRemoteLocation(
			"scrolling", 1, 0,
			0x010000|uint32(rmRoom), 0x0778+k*2, 0x0E30, int16(0x0700+k*0x40), 0x0E00,
		)
	}

	// transition done:
	e.WRAM[skSubModule] = 0x00
	h.frames(2)
	h.expectRemoteLocation("in next room", 1, 0, 0x010000|uint32(rmOtherRoom), 0x0780, 0x0E30, 0x0800, 0x0E00)
}
