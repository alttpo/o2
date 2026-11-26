package alttp

import (
	"fmt"
	"o2/games/alttp/underworldmap"

	"github.com/alttpo/snes/asm"
)

// Warps to a point of an overworld area
//
// The debug page's map picks an area (a parent area: a small one or the top left screen of a large one) and where
// Link is placed in it, moved by the page to where the area's walk data (underworldmap.AreaWalk) says he can stand.
// The warp leaves for the overworld the way Link leaves a cave: module $08 (LoadOverworldFromDungeon) puts back the
// state of the overworld the game kept when he went in (LoadCachedEntranceProperties, $7EC140..), which the warp
// writes for the point first (underworldmap.AreaPlacement: the area, Link, the scroll, the camera and the map16
// window), and leaves as if from cave $0100 (underworldmap.AreaWarpRoom). The rest of the area's state (its graphics,
// the camera's bounds, the overlays) is loaded by the module from the area, as for any exit. The underworld map's
// generator loads every area this same way, which checks it puts Link and the screen where asked.
//
// After the iris opens, the game walks Link down out of the cave's door (underworldmap.ExitWalkDown), and from the
// overworld the iris would keep his Y in the cache, so the warp takes him as indoors for it. No door is drawn opening
// ($0696 and $0698 are 0). Like the room warp, the warp stays queued until Link stands at the
// point (debugAreaWarpDone).

// debugOverworldMap returns the overworld's areas of the unpatched ROM, drawn the first time they are asked for.
func (g *Game) debugOverworldMap() *underworldmap.OverworldStream {
	if g.unpatchedROM == nil {
		return nil
	}
	return underworldmap.SharedOverworld(g.unpatchedROM)
}

// prepareAreaWarp checks the point of an area warp is where Link can stand and computes the overworld state for it.
func (g *Game) prepareAreaWarp(op *debugOp) error {
	s := g.debugOverworldMap()
	if s == nil {
		return fmt.Errorf("warp: no overworld map without the ROM")
	}
	a := s.Area(uint8(op.R))
	if a == nil {
		return fmt.Errorf("warp: area $%02x is not on the map (yet)", op.R)
	}
	if a.Walk.At(op.X, op.Y)&underworldmap.WalkStandable == 0 {
		return fmt.Errorf("warp: Link can't stand at ($%03x, $%03x) of area $%02x", op.X, op.Y, op.R)
	}
	p := underworldmap.PlaceInArea(uint8(op.R), op.X, op.Y)
	op.area = &p
	return nil
}

// debugAreaWarpDone reports whether Link stands at the warp's point in the overworld. Called with the state lock held.
func (g *Game) debugAreaWarpDone(op *debugOp) bool {
	p := op.area
	if p == nil || g.wram[0x10] != 0x09 || g.wram[0x11] != 0 || g.wram[0x1B] != 0 || g.wram[0x8A] != p.Area {
		return false
	}
	return g.wramU16(0x22) == p.X && g.wramU16(0x20) == p.Y
}

func (g *Game) emitDebugAreaWarp(a *asm.Emitter, op *debugOp, lbl func(string) string) {
	p := op.area
	if p == nil {
		return
	}
	done := lbl("done")
	emitWarpGuard(a, done, lbl)

	a.REP(0x20)
	offs, vals := p.CacheWords()
	for i, o := range offs {
		a.LDA_imm16_w(vals[i])
		a.STA_long(underworldmap.AreaCacheBase + uint32(o))
	}
	// the graphics indices are kept as they are; LoadOverworldScreenProperties sets them for the area after:
	a.LDA_abs(0x0AA0)
	a.STA_long(underworldmap.AreaCacheBase + 0x24)
	a.LDA_abs(0x0AA2)
	a.STA_long(underworldmap.AreaCacheBase + 0x26)
	// no door opening as Link comes out:
	a.STZ_abs(0x0696)
	a.STZ_abs(0x0698)
	a.SEP(0x20)

	emitWarpDoor(a, 0, underworldmap.AreaWarpRoom, p.Area&0x40, true, lbl)

	// from the overworld, the iris's first step keeps Link's Y in the cache's (Dungeon_PrepExitWithSpotlight:
	// link_y_coord_exit, $7EC148), which would undo the warp's: Link is taken as indoors for the iris, as from a cave,
	// with the fixed color it sets outdoors (black outside the iris). LoadOverworldFromUnderworld clears $1B:
	indoors := lbl("indoors")
	a.LDA_dp(0x1B)
	a.BNE(indoors)
	a.LDA_imm8_b(0x20)
	a.STA_dp(0x9C)
	a.LDA_imm8_b(0x40)
	a.STA_dp(0x9D)
	a.LDA_imm8_b(0x80)
	a.STA_dp(0x9E)
	a.LDA_imm8_b(0x01)
	a.STA_dp(0x1B)
	a.Label(indoors)
	a.Label(done)
}
