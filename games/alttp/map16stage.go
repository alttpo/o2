package alttp

import (
	"bytes"
	"encoding/binary"
	"log"
	"time"

	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Staged map16 changes
//
// Remote map16 changes (see syncmap16.go) are applied by update routines in submodule 0, after an area finished
// loading: the player entering an area saw the tiles change only after the scroll transition, or after the spotlight
// opened when coming out of a cave or dungeon. To make them appear seamlessly, the changes are applied to the tilemap
// while the area loads, before anything is drawn from it:
//
//   - Every area load (scroll transitions in Module09_LoadNewMapAndGFX, loads from the underworld and special areas,
//     warps, the flute, the reload after a mirror bonk) decompresses the area's tilemap into $7E2000 and then calls
//     Overworld_HandleOverlaysAndBombDoors, which applies the area's overlay and bombed doors to it. The scroll
//     transition then streams the new area into VRAM row by row from $7E2000, and the other loads draw the whole
//     screen from it before the spotlight or mosaic opens. The patch hooks the first instructions of that routine
//     (emitMap16StageHook): the hook writes the staged changes of the area being loaded ($8A) to $7E2000 first.
//   - o2 stages the logs of all remote players on the same team that are in the overworld area their log is for,
//     whichever area that is (map16StagedEntries), in one of two SRAM tables and then switches the selector byte to
//     it, so the hook never sees a half written table. Since the hook runs a few frames after a scroll transition
//     starts, the logs must be known before the player enters the area: message 0x17 is sent to all players
//     (broadcastAll), not just to the players in the same area.
//   - The hook does not add the changes to the local log ($04AC is reset after it in scroll transitions); the update
//     routines in submodule 0 then find the tiles already there and only memorize them (see emitMap16Apply).
//
// Small areas and the parts of large areas around the screen are drawn from $7E2000 by the load, the rest when the
// player scrolls to it, so nothing needs to be drawn by the hook.
const (
	// map16StageSelAddr is non-zero while the game uses table B rather than A; both are $FFFF terminated lists of
	// `area u16`, `pos u16`, `tile u16`. Written by o2; the init code terminates both tables on reset:
	map16StageSelAddr = uint32(0x706300)
	map16StageAAddr   = map16StageSelAddr + 2
	map16StageMax     = 64 // entries per table
	map16StageEntry   = 6
	map16StageSize    = map16StageMax*map16StageEntry + 2
	map16StageBAddr   = map16StageAAddr + map16StageSize
	map16StageEnd     = map16StageBAddr + map16StageSize

	map16StageAreaEnd = uint16(0xFFFF)
	// map16StageRefresh is how often the staged table is written again even if unchanged, in case the game was reset
	// (which empties both tables):
	map16StageRefresh = 2 * time.Second
)

// the staged tables must end before the update routine buffers:
const _ = updateBufAAddr - map16StageEnd

// map16StageHookExpected is the code at fnOverworldHandleOverlays that the hook replaces with `JSL hook; NOP`:
// `LDA.w #$020F`, `LDX.b $8A`. The hook runs them before returning.
var map16StageHookExpected = []byte{0xA9, 0x0F, 0x02, 0xA6, 0x8A}

type map16Stage struct {
	// the table last written, which table it was written to (false: A) and when:
	written     []map16StagedEntry
	writtenB    bool
	writtenOK   bool
	writtenTime time.Time
}

type map16StagedEntry struct {
	Area, Pos, Tile uint16
}

// map16StagedEntries returns the changes to stage: the logs of the remote players on the same team that are in the
// overworld location their log is for, keeping the last tile for each area and position, up to map16StageMax.
func (g *Game) map16StagedEntries() (list []map16StagedEntry) {
	local := g.local
	if local == nil {
		return
	}
	at := make(map[[2]uint16]int)
	for _, p := range g.RemotePlayers() {
		if p == local || p.TTL() <= 0 || p.Team != local.Team {
			continue
		}
		if !(isOverworldModule(p.Module) || p.Module == 0x0E) {
			continue
		}
		rm := &p.Map16
		if rm.loc != p.Location || rm.loc&0x010000 != 0 {
			continue
		}
		area := owLocationArea(rm.loc)
		for i := 0; i < rm.count && rm.have[i]; i++ {
			e := rm.entries[i]
			k := [2]uint16{area, e.Pos}
			if j, ok := at[k]; ok {
				list[j].Tile = e.Tile
				continue
			}
			if len(list) >= map16StageMax {
				continue
			}
			at[k] = len(list)
			list = append(list, map16StagedEntry{area, e.Pos, e.Tile})
		}
	}
	return
}

func equalStaged(a, b []map16StagedEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeMap16Stage writes the staged map16 changes to SRAM when they changed, and every map16StageRefresh.
func (g *Game) writeMap16Stage(now time.Time) {
	s := &g.map16Stage
	if !g.ROMVerified || g.IsResetting() {
		return
	}
	q := g.getQueue()
	if q == nil {
		return
	}
	var list []map16StagedEntry
	if g.SyncOverworld {
		list = g.map16StagedEntries()
	}
	if s.writtenOK && equalStaged(list, s.written) && now.Sub(s.writtenTime) < map16StageRefresh {
		return
	}

	// write the table the game does not use, then select it:
	toB := !s.writtenB
	addr := map16StageAAddr
	if toB {
		addr = map16StageBAddr
	}
	data := make([]byte, 0, len(list)*map16StageEntry+2)
	for _, e := range list {
		data = binary.LittleEndian.AppendUint16(data, e.Area)
		data = binary.LittleEndian.AppendUint16(data, e.Pos)
		data = binary.LittleEndian.AppendUint16(data, e.Tile)
	}
	data = binary.LittleEndian.AppendUint16(data, map16StageAreaEnd)
	pak, err := lorom.BusAddressToPak(addr)
	if err != nil {
		return
	}
	selPak, _ := lorom.BusAddressToPak(map16StageSelAddr)
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
		log.Printf("alttp: map16: error enqueuing staged changes write: %v\n", err)
		return
	}
	if !s.writtenOK || !equalStaged(list, s.written) {
		log.Printf("alttp: map16: staged %d remote changes for area loads\n", len(list))
	}
	s.written = append(s.written[:0], list...)
	s.writtenB, s.writtenOK, s.writtenTime = toB, true, now
}

// emitMap16StageInit emits the init code that empties both staged tables. Runs with A, X and Y 16-bit.
func emitMap16StageInit(a *asm.Emitter) {
	a.Comment("empty the staged map16 change tables:")
	a.LDA_imm16_w(map16StageAreaEnd)
	a.STA_long(map16StageAAddr)
	a.STA_long(map16StageBAddr)
	a.LDA_imm16_w(0x0000)
	a.STA_long(map16StageSelAddr)
}

// map16StageHookSite checks the code at the start of Overworld_HandleOverlaysAndBombDoors: ok if it is the vanilla
// code (map16StageHookExpected) or a JSL to a randomizer's routine that replaces it (z3randomizer's HardcodedRocks),
// whose address is returned as chain.
func map16StageHookSite(rom *snes.ROM, addr uint32) (ok bool, chain uint32) {
	pak, err := lorom.BusAddressToPak(addr)
	if err != nil || int(pak)+len(map16StageHookExpected) > len(rom.Contents) {
		return false, 0
	}
	c := rom.Contents[pak : pak+uint32(len(map16StageHookExpected))]
	if bytes.Equal(c, map16StageHookExpected) {
		return true, 0
	}
	if c[0] == 0x22 {
		return true, uint32(c[1]) | uint32(c[2])<<8 | uint32(c[3])<<16
	}
	return false, 0
}

// emitMap16StageHook emits the routine that the patched first instructions of Overworld_HandleOverlaysAndBombDoors
// call with JSL, in 16-bit mode: it writes the staged changes for the area being loaded ($8A) to $7E2000, then runs
// the instructions it replaced (map16StageHookExpected), or jumps to chain, the randomizer's routine that the JSL
// called before, which returns to the game. A and X are not preserved (the replaced code loads both), Y and P are.
func emitMap16StageHook(a *asm.Emitter, chain uint32) {
	a.AssumeREP(0x30)
	a.Label("map16StageHook")
	a.PHP()
	a.PHB()
	a.REP(0x30)
	a.PEA(uint16(map16StageSelAddr>>16) * 0x0101)
	a.PLB()
	a.PLB()
	a.Comment("X = the selected table:")
	a.LDX_imm16_w(uint16(map16StageAAddr & 0xFFFF))
	a.LDA_abs(uint16(map16StageSelAddr & 0xFFFF))
	a.AND_imm16_w(0x00FF)
	a.BEQ("m16s_loop")
	a.LDX_imm16_w(uint16(map16StageBAddr & 0xFFFF))
	a.Label("m16s_loop")
	a.LDA_abs_x(0x0000)
	a.CMP_imm16_w(map16StageAreaEnd)
	a.BEQ("m16s_done")
	a.CMP_long(0x7E008A)
	a.BNE("m16s_next")
	a.PHX()
	a.LDA_abs_x(0x0004)
	a.PHA()
	a.LDA_abs_x(0x0002)
	a.AND_imm16_w(0x1FFE)
	a.TAX()
	a.PLA()
	a.STA_long_x(0x7E2000)
	a.PLX()
	a.Label("m16s_next")
	a.TXA()
	a.CLC()
	a.ADC_imm16_w(map16StageEntry)
	a.TAX()
	a.Comment("never past the end of the table B:")
	a.CPX_imm16_w(uint16((map16StageEnd - map16StageEntry) & 0xFFFF))
	a.BCC("m16s_loop")
	a.Label("m16s_done")
	a.PLB()
	a.PLP()
	if chain != 0 {
		a.Comment("the randomizer's routine the JSL called before; it returns to the game:")
		a.JML(chain)
		return
	}
	a.Comment("the instructions the JSL replaced:")
	a.EmitBytes(map16StageHookExpected)
	a.RTL()
}
