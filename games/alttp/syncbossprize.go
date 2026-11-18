package alttp

import (
	"fmt"
	"log"

	"github.com/alttpo/snes/asm"
)

// Boss prize rescue
//
// A boss room's tag RoomTag_GetHeartForPrize ($25, $01C709) waits for the boss flag ($80 of $0403, set when the boss's
// heart container is taken) and then drops the dungeon's prize (a pendant or crystal, ancilla $29) only if it was not
// collected yet: the pendant or crystal bit of the dungeon in $7EF374/$7EF37A (randomizers check their own dungeons
// completed bits instead). Either way it deletes the tag. Picking up the prize is the only way out of the boss room:
// its trap doors stay closed, and the pendant warps Link out while the crystal starts the maiden cutscene, which does.
//
// The prize bits and the dungeons completed bits are synced, so when a remote player already got this dungeon's prize
// the local player beats the boss and is left trapped in the boss room with no prize to pick up.
//
// While the tag is active in the current room and the boss flag is still clear (the boss was not beaten before this
// visit), the room is armed. Once the boss flag is set and the game deleted the tag without spawning a prize (none of
// ancillae $29 (falling prize) or $3E (rising crystal) exists), the prize is spawned through the game's own code at
// RoomTag_GetHeartForPrize's spawn_prize, so it is the same prize the game would have dropped, and collecting it again
// takes the player out of the dungeon like the first time.

const (
	// roomTagGetHeartForPrize is the room tag ($AE/$AF) of boss rooms that drop the dungeon's prize:
	roomTagGetHeartForPrize = uint8(0x25)
	// ancilla types ($0C4A..$0C53):
	ancillaTypes        = uint16(0x0C4A)
	ancillaSlots        = 10
	ancillaFallingPrize = uint8(0x29)
	ancillaItemReceipt  = uint8(0x22)
	ancillaRisingPrize  = uint8(0x3E)
	// bossPrizeDungeons is the number of dungeons ($040C / 2) in RoomTag_GetHeartForPrize's prize table:
	bossPrizeDungeons = 13
)

// bossPrizeLocal tracks the local player's boss room.
type bossPrizeLocal struct {
	// armed is true when the prize tag was seen active in room with the boss flag clear:
	armed   bool
	room    uint16
	dungeon uint8
	// slot is the tag slot (0: $AE, 1: $AF) of the prize tag:
	slot uint8
	// done is true once the prize was seen or spawned in this visit:
	done bool

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *bossPrizeUpdate
}

func (g *Game) resetBossPrize() {
	bp := &g.bossPrize
	if bp.inflight != nil {
		bp.inflight.cancelled = true
	}
	*bp = bossPrizeLocal{}
}

// readBossPrize arms the boss prize rescue in a boss room whose boss is not beaten yet. Called when WRAM is read from
// the SNES.
func (g *Game) readBossPrize() {
	local := g.local
	bp := &g.bossPrize

	if local.Module == 0x0E {
		// menus don't touch the room:
		return
	}
	if local.Module != 0x07 {
		if bp.armed {
			g.resetBossPrize()
		}
		return
	}
	for _, o := range []uint16{0xA0, 0xA1, 0xAE, 0xAF, roomFlagsOffs, 0x040C} {
		if !g.wramFresh[o] {
			return
		}
	}
	room := g.wramU16(0xA0)
	if bp.armed && bp.room != room {
		g.resetBossPrize()
	}
	// room loads happen in other submodules and the tag only runs in submodule 0:
	if local.SubModule != 0 || bp.armed {
		return
	}

	dungeon := g.wram[0x040C]
	if dungeon == 0xFF || dungeon>>1 >= bossPrizeDungeons {
		return
	}
	if g.wram[roomFlagsOffs]&roomFlagBoss != 0 {
		// beaten before this visit; the game knows whether its prize is still to be collected:
		return
	}
	for k := uint8(0); k < 2; k++ {
		if g.wram[0xAE+uint16(k)] != roomTagGetHeartForPrize {
			continue
		}
		*bp = bossPrizeLocal{armed: true, room: room, dungeon: dungeon, slot: k}
		log.Printf("alttp: boss prize: room $%03x: armed\n", room)
		return
	}
}

// bossPrizeUpdate is a generated update routine that spawns the boss prize, awaiting execution confirmation.
type bossPrizeUpdate struct {
	g         *Game
	room      uint16
	cancelled bool
}

func (u *bossPrizeUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	bp := &g.bossPrize
	if u.cancelled || bp.inflight != u {
		return
	}
	bp.inflight = nil

	if value == crystalSwitchSkipped {
		// not yet, or the spawn failed; try again later:
		return
	}
	if !bp.armed || bp.room != u.room {
		return
	}
	bp.done = true
	if value == crystalSwitchWritten {
		log.Printf("alttp: boss prize: room $%03x: spawned the prize that was already collected\n", u.room)
		g.PushNotification("dropped the dungeon prize again to leave the dungeon")
	}
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *bossPrizeUpdate) AbortAsm() {
	bp := &u.g.bossPrize
	if u.cancelled || bp.inflight != u {
		return
	}
	// the routine checks for an existing prize, so running it again does no harm:
	bp.inflight = nil
}

// generateBossPrizeUpdate generates an update routine that spawns the boss prize when the game deleted the prize tag
// without spawning it. It has no side effects; the caller must set inflight if the routine is emitted.
func (g *Game) generateBossPrizeUpdate(newEmitter func() *asm.Emitter, index uint32) (u *bossPrizeUpdate, a *asm.Emitter) {
	bp := &g.bossPrize
	local := g.local
	if !bp.armed || bp.done || bp.inflight != nil {
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 {
		return
	}
	if g.wram[roomFlagsOffs]&roomFlagBoss == 0 || g.wram[0xAE+uint16(bp.slot)] != 0 {
		// the boss is not beaten yet or the tag has not run yet:
		return
	}
	fnSpawn, ok := g.romFunctions[fnRoomTagSpawnPrize]
	if !ok {
		return
	}
	fnRTL, ok := g.romFunctions[fnBank01RTL]
	if !ok {
		return
	}

	u = &bossPrizeUpdate{g: g, room: bp.room}

	a = newEmitter()
	a.Comment(fmt.Sprintf("boss prize in room $%03x:", bp.room))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("bp_skip")
	a.LDA_dp(0x11)
	a.BNE("bp_skip")
	a.Comment("still in the same room:")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(bp.room))
	a.BNE("bp_skip")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(bp.room >> 8))
	a.BNE("bp_skip")
	a.LDA_abs(0x040C)
	a.CMP_imm8_b(bp.dungeon)
	a.BNE("bp_skip")
	a.Comment("the boss is beaten and the prize tag is done:")
	a.LDA_abs(roomFlagsOffs)
	a.AND_imm8_b(roomFlagBoss)
	a.BEQ("bp_skip")
	a.LDA_dp(0xAE + bp.slot)
	a.BNE("bp_skip")
	a.Comment("no prize around; wait for an item receipt (the heart container) to finish:")
	a.LDX_imm8_b(ancillaSlots - 1)
	a.Label("bp_scan")
	a.LDA_abs_x(ancillaTypes)
	a.CMP_imm8_b(ancillaFallingPrize)
	a.BEQ("bp_same")
	a.CMP_imm8_b(ancillaRisingPrize)
	a.BEQ("bp_same")
	a.CMP_imm8_b(ancillaItemReceipt)
	a.BEQ("bp_skip")
	a.DEX()
	a.BPL("bp_scan")
	a.Comment("spawn the prize like RoomTag_GetHeartForPrize does; it deletes the (already deleted) tag in slot $0E:")
	a.LDA_dp(0x0E)
	a.PHA()
	a.LDA_imm8_b(bp.slot)
	a.STA_dp(0x0E)
	a.JSRBanked(fnSpawn, fnRTL)
	a.PLA()
	a.STA_dp(0x0E)
	a.Comment("did it spawn? (not if all ancilla slots are taken):")
	a.LDX_imm8_b(ancillaSlots - 1)
	a.Label("bp_check")
	a.LDA_abs_x(ancillaTypes)
	a.CMP_imm8_b(ancillaFallingPrize)
	a.BEQ("bp_written")
	a.DEX()
	a.BPL("bp_check")
	a.BRA("bp_skip")
	a.Label("bp_written")
	a.LDA_imm8_b(crystalSwitchWritten)
	a.BRA("bp_report")
	a.Label("bp_same")
	a.LDA_imm8_b(crystalSwitchSame)
	a.BRA("bp_report")
	a.Label("bp_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.Label("bp_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}
