package alttp

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/alttpo/snes/asm"
	"o2/snes"
)

// Overworld event synchronization
//
// Some overworld changes happen while a player is in the area and are animated, but the game only draws them for the
// player who caused them. The area flags ($7EF280+area, synced with the SRAM messages) make other players load the
// result once they enter the area again; players already in the area see nothing. These events are shared with the
// players in the same area (alttpo's OverworldEvents.as) so they see the same animation:
//
//   - Entrance animations. Setting $04C6 (1: Palace of Darkness (Kiki), 2: Skull Woods (fire rod), 3: Misery Mire
//     (medallion), 4: Turtle Rock (medallion), 5: Ganon's Tower (crystals)) with $B0 = $C8 = 0 makes
//     Module09_00_PlayerControl run Overworld_AnimateEntrance every frame, which freezes Link, draws the opened entrance
//     step by step (Overworld_DrawMap16_Persist, which does not log the changes for the map16 sync), sets bit $20 of
//     the area's flags and clears $04C6 when done. Skull Woods is started with FireRodShot_BecomeSkullWoodsFire, which
//     also lights the fire around the entrance.
//   - The Desert Palace statues move apart once $02F0 is set (Link reads the tablet with the Book of Mudora), until the
//     area reloads (Sprite_ResetAll clears $02F0). Nothing is saved; the statues are back the next time.
//   - The lumberjacks' tree top (sprite $3B with $0DC0 = 2, in area $02) hops and falls apart in leaves when Link
//     dashes into it ($0D80 = 1, $0F80 = 20), revealing the hole under it until the area reloads.
//
// Message 0x1C, broadcastSector: `area u16` ($8A), `entrance u8` ($04C6 while an entrance animation runs, else 0),
// `flags u8` (bit 0: the desert statues were released, bit 1: the tree top was dashed into, both during this area
// visit). It is sent in its own packet every frame while something is set, and for a few frames after, in overworld
// modules $09/$0B (and the menus in $0E).
//
// While in the overworld module ($09), submodule 0, in the same area as a remote player on the same team:
//
//   - If the remote entrance is set, ours is not and the area's overlay (bit $20) was not drawn when the area loaded
//     (nor since by an entrance animation here), start the same entrance animation. Link must be standing on the ground
//     ($5D = 0, $4D = 0, $02E4 = 0), since the animation keeps him frozen in place.
//   - If the remote desert statues were released and ours were not, set $02F0.
//   - If the remote tree top was dashed into and ours still stands, make it hop like the dash does.

const (
	owEntranceOffs   = uint16(0x04C6) // trigger_special_entrance
	owDesertOffs     = uint16(0x02F0) // set when the desert tablet was read; Sprite_57_DesertStatue then moves
	owLumberjackArea = uint16(0x02)
	owTreeSprite     = uint8(0x3B) // Sprite_DashItem; $0DC0 = 2 on the overworld is the lumberjacks' tree top

	owEventDesert = uint8(0x01)
	owEventTree   = uint8(0x02)

	owEntranceSkullWoods = uint8(0x02)
	owEntranceMax        = uint8(0x05)

	// frames to keep sending the events after they cleared, so receivers see them clear:
	owEventsTrailFrames = 16
	// remote events not refreshed within this time are ignored (e.g. a lost packet clearing them):
	owEventsRemoteTTL = 500 * time.Millisecond

	owEventsSkipped = uint8(0x00)
	owEventsWritten = uint8(0x01)
	owEventsSame    = uint8(0x02)
)

// owEventsLocal tracks the local player's overworld events in the current area.
type owEventsLocal struct {
	// valid is true when the area was sampled in the overworld module, submodule 0:
	valid bool
	area  uint16
	// shown is true when the area's overlay was drawn: bit $20 of its flags was set when it loaded, or an entrance
	// animation ran here since:
	shown    bool
	entrance uint8
	// flags are the events (owEvent*) that happened here during this visit:
	flags uint8
	// trail counts down the frames to keep sending after nothing is set any more:
	trail int

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *owEventsUpdate
}

// owEventsRemote is a remote player's overworld events as received.
type owEventsRemote struct {
	valid    bool
	area     uint16
	entrance uint8
	flags    uint8
	at       time.Time
}

func (g *Game) resetOwEvents() {
	e := &g.owEvents
	if e.inflight != nil {
		e.inflight.cancelled = true
	}
	*e = owEventsLocal{}
}

// readOwEvents samples the local player's overworld events. Called when WRAM is read from the SNES.
func (g *Game) readOwEvents() {
	local := g.local
	e := &g.owEvents

	if local.Module == 0x0E {
		// menus and text boxes keep the area:
		return
	}
	if !isOverworldModule(local.Module) {
		if e.valid {
			g.resetOwEvents()
		}
		return
	}
	if !g.wramFresh[0x8A] || !g.wramFresh[0x8B] {
		return
	}
	area := g.wramU16(0x8A)
	if e.valid && e.area != area {
		g.resetOwEvents()
	}
	// area loads happen in other submodules:
	if local.SubModule != 0 {
		return
	}
	if !e.valid {
		if area >= 0xC0 || !g.wramFresh[0xF280+uint32(area)] {
			return
		}
		e.valid = true
		e.area = area
		e.shown = g.wram[0xF280+uint32(area)]&0x20 != 0
	}

	if g.wramFresh[owEntranceOffs] {
		e.entrance = g.wram[owEntranceOffs]
		if e.entrance != 0 {
			e.shown = true
		}
	}
	if g.wramFresh[owDesertOffs] && g.wram[owDesertOffs] != 0 {
		e.flags |= owEventDesert
	}
	if area == owLumberjackArea && g.treeDashed() {
		e.flags |= owEventTree
	}
}

// treeDashed returns true if the lumberjacks' tree top was dashed into, i.e. it is hopping or falling apart.
func (g *Game) treeDashed() bool {
	for i := uint32(0); i < 0x10; i++ {
		if !g.wramFresh[0x0DD0+i] || !g.wramFresh[0x0E20+i] || !g.wramFresh[0x0DC0+i] || !g.wramFresh[0x0D80+i] {
			return false
		}
		if g.wram[0x0DD0+i] == 0 || g.wram[0x0E20+i] != owTreeSprite || g.wram[0x0DC0+i] != 2 {
			continue
		}
		if g.wram[0x0D80+i] != 0 {
			return true
		}
	}
	return false
}

// enqueueOwEventReads reads the sprite tables in the lumberjacks' area to notice the tree top being dashed into.
func (g *Game) enqueueOwEventReads(q []snes.Read) []snes.Read {
	if !g.SyncOverworld || !isOverworldModule(g.local.Module) {
		return q
	}
	if g.wram[0x1B] != 0 || g.wramU16(0x8A) != owLumberjackArea {
		return q
	}
	// $0D80 ai state, $0DC0 graphics, $0DD0 state, $0E20 type:
	return g.readEnqueue(q, 0xF50D80, 0xB0, nil) // [$0D80..$0E2F]
}

// sendOwEvents sends the local player's overworld events to the players in the same area.
func (g *Game) sendOwEvents() {
	e := &g.owEvents
	if !e.valid || !(isOverworldModule(g.local.Module) || g.local.Module == 0x0E) {
		return
	}
	if e.entrance != 0 || e.flags != 0 {
		e.trail = owEventsTrailFrames
	} else if e.trail > 0 {
		e.trail--
	} else {
		return
	}

	m := g.makeSectorMessage()
	if err := g.SerializeOwEvents(m); err != nil {
		panic(err)
	}
	g.send(m)
}

// SerializeOwEvents writes message 0x1C.
func (g *Game) SerializeOwEvents(w io.Writer) (err error) {
	e := &g.owEvents
	_, err = w.Write([]byte{uint8(MsgOwEvents), uint8(e.area), uint8(e.area >> 8), e.entrance, e.flags})
	return
}

func (g *Game) DeserializeOwEvents(p *Player, r io.Reader) (err error) {
	var b [4]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return fmt.Errorf("error deserializing overworld events: %w", err)
	}
	area := uint16(b[0]) | uint16(b[1])<<8
	re := &p.OwEvents
	if re.valid && (re.area != area || re.entrance != b[2] || re.flags != b[3]) || !re.valid && (b[2] != 0 || b[3] != 0) {
		log.Printf("alttp: owevents: from '%s': area $%02x entrance %d flags $%02x\n", p.Name(), area, b[2], b[3])
	}
	*re = owEventsRemote{valid: true, area: area, entrance: b[2], flags: b[3], at: time.Now()}
	return nil
}

// owEventsUpdate is a generated update routine applying a remote player's overworld events.
type owEventsUpdate struct {
	g         *Game
	area      uint16
	entrance  uint8
	flags     uint8
	from      *Player
	cancelled bool
}

func (u *owEventsUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	e := &g.owEvents
	if u.cancelled || e.inflight != u {
		return
	}
	e.inflight = nil

	if value == owEventsSkipped {
		// conditions were not right; try again later:
		return
	}
	if !e.valid || e.area != u.area {
		return
	}
	// either applied, or nothing to do (already done here, or no tree top to dash into):
	e.flags |= u.flags
	if u.entrance != 0 {
		e.shown = true
	}
	if value == owEventsWritten {
		// the area flags sync notifies about opened entrances:
		log.Printf("alttp: owevents: area $%02x: entrance %d flags $%02x from player '%s'\n", u.area, u.entrance, u.flags, u.from.Name())
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed; it is generated again.
func (u *owEventsUpdate) AbortAsm() {
	e := &u.g.owEvents
	if u.cancelled || e.inflight != u {
		return
	}
	e.inflight = nil
}

// generateOwEventsUpdate generates an update routine to replay the overworld events of the remote players in the same
// area that did not happen here yet. The caller must set inflight if the routine is emitted.
func (g *Game) generateOwEventsUpdate(newEmitter func() *asm.Emitter, index uint32) (u *owEventsUpdate, a *asm.Emitter) {
	e := &g.owEvents
	local := g.local
	if !e.valid || e.inflight != nil {
		return
	}
	if local.Module != 0x09 || local.SubModule != 0 {
		return
	}

	var from *Player
	entrance, flags := uint8(0), uint8(0)
	now := time.Now()
	for _, p := range g.RemotePlayers() {
		// game worlds are team specific; menus and text boxes ($0E) keep the area:
		if p.Team != local.Team || !(isOverworldModule(p.Module) || p.Module == 0x0E) || p.Location&0x010000 != 0 || p.OverworldArea != e.area {
			continue
		}
		re := &p.OwEvents
		if !re.valid || re.area != e.area || now.Sub(re.at) > owEventsRemoteTTL {
			continue
		}
		if entrance == 0 && !e.shown && e.entrance == 0 && re.entrance != 0 && re.entrance <= owEntranceMax {
			entrance, from = re.entrance, p
		}
		if f := re.flags &^ e.flags &^ flags; f != 0 {
			flags |= f
			if from == nil {
				from = p
			}
		}
	}
	if from == nil {
		return
	}
	fnSkull, ok := g.romFunctions[fnSkullWoodsFire]
	if entrance == owEntranceSkullWoods && !ok {
		entrance = 0
		if flags == 0 {
			return
		}
	}

	u = &owEventsUpdate{g: g, area: e.area, entrance: entrance, flags: flags, from: from}

	a = newEmitter()
	a.Comment(fmt.Sprintf("overworld events in area $%02x from %s: entrance %d flags $%02x:", e.area, from.Name(), entrance, flags))
	a.Comment("in the overworld module, submodule 0, in the same area:")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x09)
	a.BNE_far("owe_skip")
	a.LDA_dp(0x11)
	a.BNE_far("owe_skip")
	a.LDA_dp(0x1B)
	a.BNE_far("owe_skip")
	a.LDA_dp(0x8A)
	a.CMP_imm8_b(uint8(e.area))
	a.BNE_far("owe_skip")
	a.LDA_dp(0x8B)
	a.CMP_imm8_b(uint8(e.area >> 8))
	a.BNE_far("owe_skip")
	a.Comment("$00 = result:")
	a.LDA_imm8_b(owEventsSame)
	a.STA_dp(0x00)

	if entrance != 0 {
		a.Comment("start the entrance animation unless one is running:")
		a.LDA_abs(owEntranceOffs)
		a.BNE("owe_entrance_done")
		a.Comment("Link must stand on the ground and not be frozen, since the animation freezes him:")
		a.LDA_dp(0x5D)
		a.ORA_long(0x7E004D)
		a.ORA_long(0x7E02E4)
		a.BNE_far("owe_skip")
		if entrance == owEntranceSkullWoods {
			a.Comment("light the fire and start the animation like a fire rod shot does:")
			a.JSL(fnSkull)
		} else {
			a.LDA_imm8_b(entrance)
			a.STA_abs(owEntranceOffs)
			a.STZ_dp(0xB0)
			a.STZ_dp(0xC8)
		}
		a.LDA_imm8_b(owEventsWritten)
		a.STA_dp(0x00)
		a.Label("owe_entrance_done")
	}
	if flags&owEventDesert != 0 {
		a.Comment("release the desert statues:")
		a.LDA_abs(owDesertOffs)
		a.BNE("owe_desert_done")
		a.INC_abs(owDesertOffs)
		a.LDA_imm8_b(owEventsWritten)
		a.STA_dp(0x00)
		a.Label("owe_desert_done")
	}
	if flags&owEventTree != 0 {
		a.Comment("make the tree top hop like dashing into it does:")
		a.LDX_imm8_b(0x0F)
		a.Label("owe_tree_loop")
		a.LDA_abs_x(0x0DD0)
		a.BEQ("owe_tree_next")
		a.LDA_abs_x(0x0E20)
		a.CMP_imm8_b(owTreeSprite)
		a.BNE("owe_tree_next")
		a.LDA_abs_x(0x0DC0)
		a.CMP_imm8_b(0x02)
		a.BNE("owe_tree_next")
		a.LDA_abs_x(0x0D80)
		a.BNE("owe_tree_next")
		a.LDA_imm8_b(0x01)
		a.STA_abs_x(0x0D80)
		a.LDA_imm8_b(20)
		a.STA_abs_x(0x0F80)
		a.LDA_imm8_b(owEventsWritten)
		a.STA_dp(0x00)
		a.Label("owe_tree_next")
		a.DEX()
		a.BPL("owe_tree_loop")
	}

	a.LDA_dp(0x00)
	a.BRA("owe_report")
	a.Label("owe_skip")
	a.LDA_imm8_b(owEventsSkipped)
	a.Label("owe_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}
