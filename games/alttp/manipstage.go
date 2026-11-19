package alttp

import (
	"encoding/binary"
	"log"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Staged manipulable object changes
//
// Remote players' lifted pots, big rocks, pounded pegs and pushed blocks (see syncmanip.go) are applied by update
// routines in submodule 0, after a room finished loading, so a player entering a room saw them change only after the
// room transition or after the spotlight opened. Like the overworld map16 changes (see map16stage.go), they are now
// applied while the room loads:
//
//   - Every room load (Dungeon_LoadRoom: room transitions, staircases, falling through holes, entering from the
//     overworld) draws the room into the tilemaps at $7E2000/$7E4000 and registers the manipulable objects in their
//     slots; the tilemaps are uploaded to VRAM afterwards. The patch hooks the instructions right after the push
//     blocks are drawn (manipStageHookAddr, `REP #$20 : LDA.w $042C`, the same in US, JP and randomizer ROMs): for the
//     room being loaded ($A0), the hook writes the staged replacement tiles of revealed objects into the tilemap (and
//     into the slot's replacement tiles), and moves pushed blocks: what was under the block at its old position, the
//     block at its new one, and the slot's position.
//   - The attribute table is built from the tilemap afterwards and marks every slot at its position with $70+slot, so
//     revealed objects could still be lifted and moved blocks pushed. The hook records them in a pending list, which
//     the trampoline works off in the first frame of submodule 0 in that room: RoomDraw_16x16Single for revealed
//     objects still marked (it draws the same tiles again and sets their attributes), attribute $27 for moved blocks.
//     A block on the upper layer (no $4000 in its position) toggles $0641 like PushBlock_CheckForPit does, and the push
//     block door tags' shutters are operated like the update routines do.
//   - Only blocks the remote player left in state $0000 one step from where the block stands here are moved; blocks
//     on switches ($0005) or in pits are left to the update routines, which check the tile attributes.
//   - o2 stages the objects of all remote players on the same team in the room their objects are for (up to
//     manipStageMax), in two tables with a selector like map16stage.go. Message 0x18 is also sent to all players
//     (broadcastAll) whenever it changes and every 32 frames, so a player entering the room has them already.
const (
	manipStageSelAddr = uint32(0x706610)
	manipStageAAddr   = manipStageSelAddr + 2
	manipStageMax     = 32 // entries per table: room, objx, pos, type (0 for push blocks), 4 tiles
	manipStageEntry   = 16
	manipStageSize    = manipStageMax*manipStageEntry + 2
	manipStageBAddr   = manipStageAAddr + manipStageSize
	manipStageEnd     = manipStageBAddr + manipStageSize

	// the pending list the hook writes and the trampoline works off: byte length (u16), then up to manipPendMax
	// entries of room, slot*2, kind (manipPend*), position:
	manipPendAddr  = uint32(0x706A20)
	manipPendList  = manipPendAddr + 2
	manipPendMax   = 8
	manipPendEntry = 8
	// scratch words for the hook and the trampoline:
	manipStageTmpOld  = manipPendList + manipPendMax*manipPendEntry
	manipStageTmpSlot = manipStageTmpOld + 2
	manipStageTmpIdx  = manipStageTmpSlot + 2
	manipStageScratch = manipStageTmpIdx + 2

	manipPendRevealed   = 0
	manipPendBlock      = 1
	manipPendBlockUpper = 2

	manipStageRoomEnd = uint16(0xFFFF)

	// manipStageHookAddr is in Dungeon_LoadRoom after the push blocks are drawn, in all supported ROMs:
	manipStageHookAddr = uint32(0x01889A)
)

const _ = manipPendAddr - manipStageEnd
const _ = captureDyingAddr - manipStageScratch

// manipStageHookExpected is the code at manipStageHookAddr the hook replaces with `JSL hook; NOP`: `REP #$20`,
// `LDA.w $042C`. The hook runs them before returning.
var manipStageHookExpected = []byte{0xC2, 0x20, 0xAD, 0x2C, 0x04}

type manipStagedEntry struct {
	Room, ObjX, Pos, Type uint16
	Tiles                 [4]uint16
}

// manipStagedEntries returns the objects to stage: those revealed and the push blocks at rest of the remote players on
// the same team that are in the room their objects are for, objects first.
func (g *Game) manipStagedEntries() (list []manipStagedEntry) {
	local := g.local
	if local == nil {
		return
	}
	var blocks []manipStagedEntry
	seen := make(map[[3]uint16]bool)
	for _, p := range g.RemotePlayers() {
		if p == local || p.TTL() <= 0 || p.Team != local.Team {
			continue
		}
		if !(p.Module == 0x07 || p.Module == 0x0E) || p.Location&0x010000 == 0 {
			continue
		}
		rm := &p.Manip
		if rm.room != p.DungeonRoom {
			continue
		}
		for i := 0; i < rm.n; i++ {
			rs := &rm.slots[i]
			if manipIsPushBlock(rs.Type) {
				if rs.Type != 0x0000 {
					continue
				}
				k := [3]uint16{rm.room, rs.ObjX, 0xFFFF}
				if seen[k] {
					continue
				}
				seen[k] = true
				blocks = append(blocks, manipStagedEntry{Room: rm.room, ObjX: rs.ObjX, Pos: rs.Pos})
				continue
			}
			if !manipIsLiftable(rs.Type) || rs.Flags&manipFlagRevealed == 0 || rs.Flags&manipFlagTiles == 0 {
				continue
			}
			k := [3]uint16{rm.room, rs.ObjX, rs.Pos}
			if seen[k] {
				continue
			}
			seen[k] = true
			list = append(list, manipStagedEntry{rm.room, rs.ObjX, rs.Pos, rs.Type, rs.Tiles})
		}
	}
	list = append(list, blocks...)
	if len(list) > manipStageMax {
		list = list[:manipStageMax]
	}
	return
}

// stagedTable is the state of a double-buffered table of staged changes in SRAM: a selector byte says which of the
// two tables the game uses; o2 writes the other one and then switches the selector.
type stagedTable struct {
	written     []byte
	writtenB    bool
	writtenOK   bool
	writtenTime time.Time
}

// writeStagedTable writes data (terminated by the caller) to the table the game does not use and selects it, when data
// changed or every refresh. Returns whether it was written.
func (g *Game) writeStagedTable(s *stagedTable, selAddr, aAddr, bAddr uint32, data []byte, now time.Time, refresh time.Duration) bool {
	if s.writtenOK && string(data) == string(s.written) && now.Sub(s.writtenTime) < refresh {
		return false
	}
	q := g.getQueue()
	if q == nil {
		return false
	}
	toB := !s.writtenB
	addr := aAddr
	if toB {
		addr = bAddr
	}
	pak, err := lorom.BusAddressToPak(addr)
	if err != nil {
		return false
	}
	selPak, _ := lorom.BusAddressToPak(selAddr)
	var sel byte
	if toB {
		sel = 1
	}
	writes := []snes.Write{
		// may be larger than 255 bytes; the device drivers split it into chunks and write them in order:
		{Address: pak, Data: data},
		{Address: selPak, Size: 1, Data: []byte{sel}},
	}
	if err := g.enqueueUnlocked(func() error {
		return q.MakeWriteCommands(writes, nil).EnqueueTo(q)
	}); err != nil {
		log.Printf("alttp: error enqueuing staged table write: %v\n", err)
		return false
	}
	s.written = append(s.written[:0], data...)
	s.writtenB, s.writtenOK, s.writtenTime = toB, true, now
	return true
}

// writeManipStage writes the staged manipulable objects to SRAM when they changed, and every map16StageRefresh.
func (g *Game) writeManipStage(now time.Time) {
	if !g.ROMVerified || g.IsResetting() {
		return
	}
	var list []manipStagedEntry
	if g.SyncUnderworldLocal {
		list = g.manipStagedEntries()
	}
	data := make([]byte, 0, len(list)*manipStageEntry+2)
	for _, e := range list {
		data = binary.LittleEndian.AppendUint16(data, e.Room)
		data = binary.LittleEndian.AppendUint16(data, e.ObjX)
		data = binary.LittleEndian.AppendUint16(data, e.Pos)
		data = binary.LittleEndian.AppendUint16(data, e.Type)
		for _, t := range e.Tiles {
			data = binary.LittleEndian.AppendUint16(data, t)
		}
	}
	data = binary.LittleEndian.AppendUint16(data, manipStageRoomEnd)
	changed := !g.manipStage.writtenOK || string(data) != string(g.manipStage.written)
	if g.writeStagedTable(&g.manipStage, manipStageSelAddr, manipStageAAddr, manipStageBAddr, data, now, map16StageRefresh) && changed {
		log.Printf("alttp: manip: staged %d remote objects for room loads\n", len(list))
	}
}

// manipStagedRevealed reports whether the staged table last written has a revealed object for the room, room data
// offset and position: the room load revealed it here already.
func (g *Game) manipStagedRevealed(room, objx, pos uint16) bool {
	d := g.manipStage.written
	for o := 0; o+manipStageEntry <= len(d); o += manipStageEntry {
		e := d[o:]
		if binary.LittleEndian.Uint16(e[0:]) == room && binary.LittleEndian.Uint16(e[2:]) == objx &&
			binary.LittleEndian.Uint16(e[4:]) == pos && binary.LittleEndian.Uint16(e[6:]) != 0 {
			return true
		}
	}
	return false
}

// emitManipStageInit emits the init code that empties both staged tables and the pending list. 16-bit registers.
func emitManipStageInit(a *asm.Emitter) {
	a.Comment("empty the staged manipulable object tables and the pending list:")
	a.LDA_imm16_w(manipStageRoomEnd)
	a.STA_long(manipStageAAddr)
	a.STA_long(manipStageBAddr)
	a.LDA_imm16_w(0x0000)
	a.STA_long(manipStageSelAddr)
	a.STA_long(manipPendAddr)
}

// emitManipPend appends a pending entry of kind for the slot in manipStageTmpSlot and the position of the staged entry
// at Y. DB = $70, 16-bit registers; clobbers A and X.
func emitManipPend(a *asm.Emitter, kind uint16, label string) {
	count := uint16(manipPendAddr & 0xFFFF)
	list := uint16(manipPendList & 0xFFFF)
	a.LDA_abs(count)
	a.CMP_imm16_w(manipPendMax * manipPendEntry)
	a.BCS(label)
	a.TAX()
	a.LDA_long(0x7E00A0)
	a.STA_abs_x(list + 0)
	a.LDA_abs(uint16(manipStageTmpSlot & 0xFFFF))
	a.STA_abs_x(list + 2)
	a.LDA_imm16_w(kind)
	a.STA_abs_x(list + 4)
	a.LDA_abs_y(4)
	a.STA_abs_x(list + 6)
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(manipPendEntry)
	a.STA_abs(count)
	a.Label(label)
}

// emitManipStageHook emits the routine that the patched code in Dungeon_LoadRoom calls with JSL: it applies the staged
// objects of the room being loaded ($A0) to the tilemaps and slots and records them in the pending list, then runs the
// instructions it replaced (manipStageHookExpected). Preserves X, Y, DB and P.
func emitManipStageHook(a *asm.Emitter, object, block uint32) {
	a.AssumeSEP(0x20)
	a.Label("manipStageHook")
	a.PHP()
	a.PHB()
	a.REP(0x30)
	a.PHX()
	a.PHY()
	a.PEA(0x7070)
	a.PLB()
	a.PLB()
	tmpOld := uint16(manipStageTmpOld & 0xFFFF)
	tmpSlot := uint16(manipStageTmpSlot & 0xFFFF)
	a.STZ_abs(uint16(manipPendAddr & 0xFFFF))
	emitStageModuleGuard(a, "ms_done")
	a.Comment("Y = the selected table:")
	a.LDY_imm16_w(uint16(manipStageAAddr & 0xFFFF))
	a.LDA_abs(uint16(manipStageSelAddr & 0xFFFF))
	a.AND_imm16_w(0x00FF)
	a.BEQ("ms_entry")
	a.LDY_imm16_w(uint16(manipStageBAddr & 0xFFFF))

	a.Label("ms_entry")
	a.LDA_abs_y(0)
	a.CMP_imm16_w(manipStageRoomEnd)
	a.BNE("ms_entry_1")
	a.JMP_abs("ms_done")
	a.Label("ms_entry_1")
	a.CMP_long(0x7E00A0)
	a.BEQ("ms_room")
	a.JMP_abs("ms_entry_next")
	a.Label("ms_room")
	a.Comment("find the slot with the same room data offset:")
	a.LDX_imm16_w(0x0000)
	a.Label("ms_slot")
	a.TXA()
	a.CMP_long(0x7E042C)
	a.BCC("ms_slot_1")
	a.JMP_abs("ms_entry_next")
	a.Label("ms_slot_1")
	a.LDA_abs_y(2)
	a.CMP_long_x(0x7E0520)
	a.BEQ("ms_slot_2")
	a.JMP_abs("ms_slot_next")
	a.Label("ms_slot_2")
	a.LDA_abs_y(6)
	a.BEQ("ms_block")

	a.Label("ms_object")
	a.Comment("a revealed object of the same type at the same position:")
	a.CMP_long_x(0x7E0500)
	a.BNE("ms_slot_next")
	a.LDA_abs_y(4)
	a.CMP_long_x(0x7E0540)
	a.BNE("ms_slot_next")
	a.STX_abs(tmpSlot)
	a.JSL(object)
	a.JMP_abs("ms_entry_next")

	a.Label("ms_block")
	a.Comment("a push block still where the room load put it, one step from the remote one:")
	a.LDA_long_x(0x7E0500)
	a.BNE("ms_entry_next")
	a.LDA_long_x(0x7E0540)
	a.STA_abs(tmpOld)
	a.LDA_abs_y(4)
	a.SEC()
	a.SBC_abs(tmpOld)
	for _, d := range []uint16{0x0100, 0xFF00, 0x0004, 0xFFFC} {
		a.CMP_imm16_w(d)
		a.BEQ("ms_block_step")
	}
	a.BRA("ms_entry_next")
	a.Label("ms_block_step")
	a.STX_abs(tmpSlot)
	a.JSL(block)
	a.BRA("ms_entry_next")

	a.Label("ms_slot_next")
	a.INX()
	a.INX()
	a.JMP_abs("ms_slot")

	a.Label("ms_entry_next")
	a.TYA()
	a.CLC()
	a.ADC_imm16_w(manipStageEntry)
	a.TAY()
	a.CPY_imm16_w(uint16((manipStageEnd - manipStageEntry) & 0xFFFF))
	a.BCS("ms_done")
	a.JMP_abs("ms_entry")

	a.Label("ms_done")
	a.PLY()
	a.PLX()
	a.PLB()
	a.PLP()
	a.Comment("the instructions the JSL replaced:")
	a.EmitBytes(manipStageHookExpected)
	a.RTL()
}

// emitManipStageObject emits the routine the hook calls with JSL for a revealed object of the staged entry at Y in the
// slot at manipStageTmpSlot: its replacement tiles become the entry's, which are drawn into the tilemap. DB = $70,
// 16-bit registers; preserves Y.
func emitManipStageObject(a *asm.Emitter) {
	a.AssumeREP(0x30)
	a.Label("manipStageObject")
	a.LDX_abs(uint16(manipStageTmpSlot & 0xFFFF))
	a.Comment("its replacement tiles, as revealed remotely (a pot can hide a hole or a switch):")
	for t := uint32(0); t < 4; t++ {
		a.LDA_abs_y(uint16(8 + 2*t))
		a.STA_long_x(0x7E0560 + t<<5)
	}
	a.Comment("drawn into the tilemap (top left, bottom left, top right, bottom right):")
	a.LDA_abs_y(4)
	a.AND_imm16_w(0x3FFE)
	a.TAX()
	for t, o := range []uint32{0x00, 0x80, 0x02, 0x82} {
		a.LDA_abs_y(uint16(8 + 2*t))
		a.STA_long_x(0x7E2000 + o)
	}
	emitManipPend(a, manipPendRevealed, "mo_pend")
	a.RTL()
}

// emitManipStageBlock emits the routine the hook calls with JSL to move the push block in the slot at
// manipStageTmpSlot from manipStageTmpOld to the position of the staged entry at Y. DB = $70, 16-bit registers;
// preserves Y.
func emitManipStageBlock(a *asm.Emitter) {
	a.AssumeREP(0x30)
	a.Label("manipStageBlock")
	a.LDA_abs_y(4)
	a.STA_abs(uint16(manipStageTmpIdx & 0xFFFF))
	a.LDX_abs(uint16(manipStageTmpSlot & 0xFFFF))
	a.PHY()
	a.PHB()
	a.PEA(0x7E7E)
	a.PLB()
	a.PLB()
	a.Comment("draw what was under it at its old position:")
	a.LDA_long(manipStageTmpOld)
	a.AND_imm16_w(0x3FFE)
	a.TAY()
	for t, o := range []uint16{0x00, 0x80, 0x02, 0x82} {
		a.LDA_abs_x(0x0560 + uint16(t)<<5)
		a.STA_abs_y(0x2000 + o)
	}
	a.Comment("move the slot and draw the block at its new position, with the block's tiles like after a push:")
	a.LDA_long(manipStageTmpIdx)
	a.STA_abs_x(0x0540)
	a.AND_imm16_w(0x3FFE)
	a.TAY()
	blockTiles := [4]uint16{0x0922, 0x0932, 0x0923, 0x0933}
	for t, o := range []uint16{0x00, 0x80, 0x02, 0x82} {
		a.LDA_imm16_w(blockTiles[t])
		a.STA_abs_y(0x2000 + o)
		a.STA_abs_x(0x0560 + uint16(t)<<5)
	}
	a.PLB()
	a.PLY()
	a.LDA_abs(uint16(manipStageTmpIdx & 0xFFFF))
	a.AND_imm16_w(0x4000)
	a.BNE("mb_lower")
	emitManipPend(a, manipPendBlockUpper, "mb_pend_upper")
	a.RTL()
	a.Label("mb_lower")
	emitManipPend(a, manipPendBlock, "mb_pend")
	a.RTL()
}

// emitManipPending emits the trampoline's code, run after the module, that works off the pending list the hook wrote
// in the first frame of submodule 0 of the dungeon module in the room it was written for (see manipstage.go). fnDraw
// is RoomDraw_16x16Single and fnRTL an RTL in its bank. Called with JSL with A and X 8-bit; preserves P.
//
// Before each object it checks the time (see lagcheck.go); without time left, or with the VRAM stripe buffer getting
// full, it goes on with the objects not done yet in the next frame.
//
// Cycles (cycles_test.go): 27 with nothing pending; the path that applies pending objects is not measured yet.
func emitManipPending(a *asm.Emitter, fnDraw, fnRTL, block uint32) {
	a.AssumeSEP(0x30)
	a.Label("manipPending")
	a.Comment("manipulable objects applied by the room load: fix their attributes once the room is loaded:")
	a.PHP()
	a.REP(0x30)
	count := manipPendAddr
	list := manipPendList
	a.LDA_long(count)
	a.BNE("mp_any")
	a.JMP_abs("mp_exit")
	a.Label("mp_any")
	a.Comment("module $07, submodule 0:")
	a.LDA_dp(0x10)
	a.CMP_imm16_w(0x0007)
	a.BEQ("mp_start")
	a.JMP_abs("mp_exit")
	a.Label("mp_start")
	a.LDX_imm16_w(0x0000)
	a.Label("mp_loop")
	a.TXA()
	a.CMP_long(count)
	a.BCC("mp_room")
	a.JMP_abs("mp_done")
	a.Label("mp_room")
	a.Comment("try again next frame when the frame has no time left for an object (see lagcheck.go):")
	a.SEP(0x20)
	emitTimeCheckLong(a, mustLateLineFor(manipPendingEntryCycles, "manip pending"), "mp_time", "mp_exit")
	a.REP(0x20)
	a.Comment("try again next frame when the VRAM stripe buffer is getting full (all of this can be done again):")
	a.LDA_long(0x7E1000)
	a.CMP_imm16_w(0x0068)
	a.BCC("mp_room_1")
	a.JMP_abs("mp_exit")
	a.Label("mp_room_1")
	a.LDA_long_x(list + 0)
	a.CMP_dp(0xA0)
	a.BEQ("mp_room_2")
	a.JMP_abs("mp_next")
	a.Label("mp_room_2")
	a.LDA_long_x(list + 6)
	a.AND_imm16_w(0x3FFF)
	a.LSR()
	a.STA_long(manipStageTmpIdx)
	a.LDA_long_x(list + 4)
	a.BNE("mp_block")

	a.Label("mp_object")
	a.Comment("a revealed object still marked $70+slot: RoomDraw_16x16Single sets its attributes (and redraws it):")
	a.PHX()
	a.LDA_long(manipStageTmpIdx)
	a.TAX()
	a.LDA_long_x(0x7F2000)
	a.AND_imm16_w(0x00FF)
	a.STA_long(manipStageTmpOld)
	a.PLX()
	a.LDA_long_x(list + 2)
	a.LSR()
	a.CLC()
	a.ADC_imm16_w(uint16(manipAttrBase))
	a.CMP_long(manipStageTmpOld)
	a.BEQ("mp_object_1")
	a.JMP_abs("mp_next")
	a.Label("mp_object_1")
	a.PHX()
	a.LDA_long_x(list + 2)
	a.TAY()
	if fnDraw != 0 {
		a.JSRBanked(fnDraw, fnRTL)
	}
	a.REP(0x30)
	a.PLX()
	a.JMP_abs("mp_next")

	a.Label("mp_block")
	a.JSL(block)
	a.REP(0x30)

	a.Label("mp_next")
	a.Comment("done with the entry: not done again when the list is resumed next frame:")
	a.LDA_imm16_w(manipStageRoomEnd)
	a.STA_long_x(list + 0)
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(manipPendEntry)
	a.TAX()
	a.JMP_abs("mp_loop")
	a.Label("mp_done")
	a.LDA_imm16_w(0x0000)
	a.STA_long(count)
	a.Label("mp_exit")
	a.PLP()
	a.RTL()
}

// emitManipPendingBlock emits the routine emitManipPending calls with JSL for a moved block (A = kind, X = pending
// entry, manipStageTmpIdx = its attribute index): attribute $27 so it can't be pushed again, and for upper layer blocks
// $0641 toggled like PushBlock_CheckForPit does, which push block doors ($0B-$13) act on. 16-bit registers; preserves X.
func emitManipPendingBlock(a *asm.Emitter) {
	a.AssumeREP(0x30)
	a.Label("manipPendingBlock")
	a.PHX()
	a.PHA()
	a.LDA_long(manipStageTmpIdx)
	a.TAX()
	a.LDA_imm16_w(0x2727)
	a.STA_long_x(0x7F2000)
	a.STA_long_x(0x7F2040)
	a.PLA()
	a.CMP_imm16_w(manipPendBlockUpper)
	a.BNE("mpb_done")
	a.LDA_long(0x7E0641)
	a.EOR_imm16_w(0x0001)
	a.STA_long(0x7E0641)
	a.LDA_long(0x7E00AE)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x000B)
	a.BCC("mpb_tag_2")
	a.CMP_imm16_w(0x0014)
	a.BCC("mpb_door")
	a.Label("mpb_tag_2")
	a.LDA_long(0x7E00AF)
	a.AND_imm16_w(0x00FF)
	a.CMP_imm16_w(0x000B)
	a.BCC("mpb_done")
	a.CMP_imm16_w(0x0014)
	a.BCS("mpb_done")
	a.Label("mpb_door")
	emitOperateBlockDoor(a, "mpb_door_done")
	a.Label("mpb_done")
	a.PLX()
	a.RTL()
}
