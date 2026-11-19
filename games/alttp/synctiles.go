package alttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/alttpo/snes/asm"
	"o2/snes"
)

// Flying floor tile synchronization
//
// Some dungeon rooms have a tile room overlord (type $14, Overlord14_TileRoom) that, while it is on screen, lifts the
// room's floor tiles one after another and flings them at Link: 128 frames after the room loads and then every 96
// frames it spawns the next of 22 tile sprites ($94) at a fixed position (TileRoom_SpawnTile). The tile erases its floor
// tile right away (UpdateTilemapWithCommonObject with object 6), rises and flies at Link. After the 22nd tile the
// overlord frees its slot. The state is the overlord's $0B28+k (tiles spawned) and its timer $0B30+k, counting down
// from 0 (first tile) or $E0 to $80. A player entering a room (or the quadrant of the overlord) that another player is
// already in started from the beginning, so the two players saw different tiles flying at different times.
//
// Shared with alttpo (FlyingTiles.as) in message 0x1D, broadcastSector to the room's sector: `room u16`, `count u8`
// (tiles spawned, 22 once the overlord is done), `timer u8`. It is sent in the dungeon module once the room's tile
// room overlord was seen in this room visit.
//
// A player takes the state of a remote player in the same room that is further along by more than a few frames (more
// tiles spawned, or the same number and more time elapsed), while the local overlord is on screen and so ticking:
// it erases the floor tiles the remote player's overlord already lifted (at most 4 per frame, since every erased tile
// queues a VRAM upload) and takes over the remote count and timer, or frees the overlord when the remote one is done.
// The tiles already flying for the remote player are not spawned here.

const (
	overlordType  = uint16(0x0B00)
	overlordXLo   = uint16(0x0B08)
	overlordXHi   = uint16(0x0B10)
	overlordYLo   = uint16(0x0B18)
	overlordYHi   = uint16(0x0B20)
	overlordGen1  = uint16(0x0B28)
	overlordGen2  = uint16(0x0B30)
	overlordSlots = 8

	// overlordTileRoom is Overlord14_TileRoom's type:
	overlordTileRoom = uint8(0x14)
	// flyingTilesCount is the number of tiles the tile room overlord spawns:
	flyingTilesCount = 22
	// flyingTilesAhead is how many frames further along a remote player's overlord must be for us to take its state;
	// the remote state is always a few frames old, which must not make two players keep taking each other's state:
	flyingTilesAhead = 8
	// flyingTilesPerFrame is the most floor tiles erased by one update routine:
	flyingTilesPerFrame = 4
)

// flyingTileX and flyingTileY are the floor tile positions TileRoom_SpawnTile spawns the tiles at, in spawn order
// (kSpawnFlyingTile_X/Y); the tile sprite erases the floor tile at these coordinates in the overlord's quadrant.
var flyingTileX = [flyingTilesCount]uint8{
	0x70, 0x80, 0x60, 0x90, 0x90, 0x60, 0x70, 0x80, 0x80, 0x70, 0x50, 0xA0, 0xA0, 0x50, 0x50, 0xA0,
	0xA0, 0x50, 0x70, 0x80, 0x80, 0x70,
}
var flyingTileY = [flyingTilesCount]uint8{
	0x80, 0x80, 0x70, 0x90, 0x70, 0x90, 0x60, 0xA0, 0x60, 0xA0, 0x60, 0xB0, 0x60, 0xB0, 0x80, 0x90,
	0x80, 0x90, 0x70, 0x90, 0x70, 0x90,
}

// flyingTilesProgress orders tile room overlord states by how far along they are: the number of tiles spawned and the
// frames elapsed since the last one (or since the room loaded).
func flyingTilesProgress(count, timer uint8) int {
	if count >= flyingTilesCount {
		return flyingTilesCount << 8
	}
	start := uint8(0xE0)
	if count == 0 {
		start = 0x00
	}
	return int(count)<<8 | int(start-timer)
}

// flyingTilesRemote is a remote player's tile room overlord state as received.
type flyingTilesRemote struct {
	valid bool
	room  uint16
	count uint8
	timer uint8
}

// flyingTilesLocal tracks the local player's tile room overlord state.
type flyingTilesLocal struct {
	// valid is true once the tile room overlord was seen in room during this room visit:
	valid bool
	room  uint16
	count uint8
	timer uint8

	// active is true while the overlord is in slot; read its tables every frame then:
	active bool
	slot   uint8
	// xHi and yHi are the overlord's quadrant, where its tiles are:
	xHi, yHi uint8
	// onScreen is true while the overlord is on screen, so it ticks:
	onScreen bool

	// inflight is the update routine awaiting confirmation of execution, if any:
	inflight *flyingTilesUpdate
}

// resetFlyingTiles forgets the tile room overlord state; it is re-read from the SNES.
func (g *Game) resetFlyingTiles() {
	ft := &g.flyingTiles
	if ft.inflight != nil {
		ft.inflight.cancelled = true
	}
	*ft = flyingTilesLocal{}
}

// enqueueFlyingTilesRead reads the overlord tables in the dungeon module. Called with the SRAM reads every 8 frames to
// find a tile room overlord, and with the WRAM reads while one is active.
func (g *Game) enqueueFlyingTilesRead(q []snes.Read, always bool) []snes.Read {
	if !g.SyncUnderworldLocal || g.local.Module != 0x07 {
		return q
	}
	if !always && !g.flyingTiles.active {
		return q
	}
	return g.readEnqueue(q, 0xF50000+uint32(overlordType), uint8(overlordGen2+overlordSlots-overlordType), nil)
}

// readFlyingTiles samples the tile room overlord of the current room. Called when WRAM is read from the SNES.
func (g *Game) readFlyingTiles() {
	local := g.local
	ft := &g.flyingTiles

	if local.Module == 0x0E {
		// menus pause the overlords; keep the state:
		return
	}
	if local.Module != 0x07 {
		g.resetFlyingTiles()
		return
	}
	if !g.wramFresh[0xA0] || !g.wramFresh[0xA1] {
		return
	}
	room := g.wramU16(0xA0)
	if ft.room != room {
		// every room load reloads the overlords:
		g.resetFlyingTiles()
		ft.room = room
	}
	if ft.inflight != nil {
		return
	}
	// room loads happen in other submodules:
	if local.SubModule != 0x00 {
		return
	}
	for i := uint16(0); i < overlordGen2+overlordSlots-overlordType; i++ {
		if !g.wramFresh[uint32(overlordType+i)] {
			return
		}
	}

	slot := -1
	for k := uint16(0); k < overlordSlots; k++ {
		if g.wram[overlordType+k] == overlordTileRoom {
			slot = int(k)
			break
		}
	}
	if slot < 0 {
		if ft.active {
			// the overlord freed its slot after the last tile:
			ft.active = false
			ft.onScreen = false
			ft.count = flyingTilesCount
			ft.timer = 0
			log.Printf("alttp: tiles: local: room $%03x done\n", room)
		}
		return
	}

	k := uint16(slot)
	if !ft.valid || ft.slot != uint8(slot) {
		log.Printf("alttp: tiles: local: room $%03x has a tile room overlord in slot %d\n", room, slot)
	}
	ft.valid = true
	ft.active = true
	ft.slot = uint8(slot)
	ft.count = g.wram[overlordGen1+k]
	ft.timer = g.wram[overlordGen2+k]
	ft.xHi = g.wram[overlordXHi+k]
	ft.yHi = g.wram[overlordYHi+k]

	// Overlord14_TileRoom only ticks while its position is on screen:
	x := (uint16(g.wram[overlordXLo+k]) | uint16(ft.xHi)<<8) - g.wramU16(0xE2)
	y := (uint16(g.wram[overlordYLo+k]) | uint16(ft.yHi)<<8) - g.wramU16(0xE8)
	ft.onScreen = x&0xFF00 == 0 && y&0xFF00 == 0
}

// flyingTilesWinner returns the remote player in the same room furthest along, if further along than us.
func (g *Game) flyingTilesWinner() (winner *Player, count, timer uint8) {
	local := g.local
	ft := &g.flyingTiles

	best := flyingTilesProgress(ft.count, ft.timer) + flyingTilesAhead
	for _, p := range g.RemotePlayers() {
		if p.Team != local.Team {
			continue
		}
		if p.Module != 0x07 || p.Location&0x010000 == 0 || p.DungeonRoom != ft.room {
			continue
		}
		rt := &p.FlyingTiles
		if !rt.valid || rt.room != ft.room {
			continue
		}
		if pr := flyingTilesProgress(rt.count, rt.timer); pr > best {
			best = pr
			winner, count, timer = p, rt.count, rt.timer
		}
	}
	return
}

// flyingTilesUpdate is a generated update routine taking a remote tile room overlord state, awaiting confirmation.
type flyingTilesUpdate struct {
	g         *Game
	room      uint16
	count     uint8
	timer     uint8
	from      *Player
	cancelled bool
}

func (u *flyingTilesUpdate) ConfirmAsmExecuted(index uint32, value uint8) {
	g := u.g
	ft := &g.flyingTiles
	if u.cancelled || ft.inflight != u {
		return
	}
	ft.inflight = nil

	if value == crystalSwitchSkipped {
		log.Printf("alttp: tiles: update to %d tiles, timer $%02x skipped\n", u.count, u.timer)
		return
	}
	if ft.room != u.room {
		return
	}
	ft.count, ft.timer = u.count, u.timer
	if u.count >= flyingTilesCount {
		ft.active = false
		ft.onScreen = false
	}
	log.Printf("alttp: tiles: update: %d tiles, timer $%02x from player '%s'\n", u.count, u.timer, u.from.Name())
}

// AbortAsm is called when the update routine could not be confirmed to have executed.
func (u *flyingTilesUpdate) AbortAsm() {
	ft := &u.g.flyingTiles
	if u.cancelled || ft.inflight != u {
		return
	}
	// the routine checks the tile count before changing anything, so the state read next is right either way:
	ft.inflight = nil
}

// generateFlyingTilesUpdate generates an update routine to take the tile room overlord state of the remote player in
// the same room that is furthest along. It has no side effects; the caller must set inflight if the routine is emitted.
func (g *Game) generateFlyingTilesUpdate(newEmitter func() *asm.Emitter, index uint32) (u *flyingTilesUpdate, a *asm.Emitter) {
	ft := &g.flyingTiles
	local := g.local
	if !ft.valid || !ft.active || !ft.onScreen || ft.inflight != nil {
		return
	}
	if local.Module != 0x07 || local.SubModule != 0 {
		return
	}
	_, ok := g.romFunctions[fnUpdateTilemapWithCommonObject]
	if !ok {
		return
	}

	winner, count, timer := g.flyingTilesWinner()
	if winner == nil {
		return
	}
	if count > flyingTilesCount {
		count = flyingTilesCount
	}
	if count < ft.count {
		// can't happen with a remote player further along:
		return
	}

	// erase the floor tiles the remote overlord already lifted, a few per frame:
	newCount := count
	if newCount > ft.count+flyingTilesPerFrame {
		newCount = ft.count + flyingTilesPerFrame
		// keep waiting for the next tile while the remaining tiles are erased:
		timer = 0xE0
	}

	k := uint16(ft.slot)
	// the table for flyingTilesErase (see emitFlyingTilesErase):
	data := binary.LittleEndian.AppendUint16(nil, k)
	data = binary.LittleEndian.AppendUint16(data, uint16(newCount-ft.count))
	for i := ft.count; i < newCount; i++ {
		data = append(data, flyingTileX[i], flyingTileY[i])
	}
	table, ok := g.updData.add(data, updTableBaseCycles+int(newCount-ft.count)*updFlyingTileEntryCycles)
	if !ok {
		return
	}

	u = &flyingTilesUpdate{g: g, room: ft.room, count: newCount, timer: timer, from: winner}

	a = newEmitter()
	a.Comment(fmt.Sprintf("tile room overlord %d tiles -> %d, timer $%02x from %s:", ft.count, newCount, timer, winner.Name()))
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BNE("ft_skip")
	a.LDA_dp(0x11)
	a.BNE("ft_skip")
	a.Comment("still in the same room:")
	a.LDA_dp(0xA0)
	a.CMP_imm8_b(uint8(ft.room))
	a.BNE("ft_skip")
	a.LDA_dp(0xA1)
	a.CMP_imm8_b(uint8(ft.room >> 8))
	a.BNE("ft_skip")
	a.Comment("the overlord did not spawn another tile since it was read:")
	a.LDA_abs(overlordType + k)
	a.CMP_imm8_b(overlordTileRoom)
	a.BNE("ft_skip")
	a.LDA_abs(overlordGen1 + k)
	a.CMP_imm8_b(ft.count)
	a.BNE("ft_skip")
	a.BRA("ft_apply")
	a.Label("ft_skip")
	a.LDA_imm8_b(crystalSwitchSkipped)
	a.BRA("ft_report")
	a.Label("ft_apply")
	a.Comment(fmt.Sprintf("erase floor tiles %d..%d like Tile_DeleteFloor, table at $%06x:", ft.count, newCount-1, table))
	a.REP(0x30)
	emitCallUpdateROM(a, flyingTilesStubAddr, table)
	a.SEP(0x30)
	if newCount >= flyingTilesCount {
		a.Comment("the overlord is done:")
		a.STZ_abs(overlordType + k)
	}
	a.LDA_imm8_b(newCount)
	a.STA_abs(overlordGen1 + k)
	a.LDA_imm8_b(timer)
	a.STA_abs(overlordGen2 + k)
	a.LDA_imm8_b(crystalSwitchWritten)
	a.Label("ft_report")
	a.STA_long(a.GetBase() + 0x02 + index)

	return
}

// SerializeFlyingTiles writes message 0x1D with the tile room overlord state, if the current room has one.
func (g *Game) SerializeFlyingTiles(w io.Writer) (err error) {
	ft := &g.flyingTiles
	if !ft.valid {
		return
	}
	_, err = w.Write([]byte{uint8(MsgFlyingTiles), uint8(ft.room), uint8(ft.room >> 8), ft.count, ft.timer})
	return
}

func (g *Game) DeserializeFlyingTiles(p *Player, r io.Reader) (err error) {
	var m struct {
		Room  uint16
		Count uint8
		Timer uint8
	}
	if err = binary.Read(r, binary.LittleEndian, &m); err != nil {
		return fmt.Errorf("error deserializing flying tiles: %w", err)
	}
	p.FlyingTiles = flyingTilesRemote{valid: true, room: m.Room, count: m.Count, timer: m.Timer}
	return nil
}

// emitFlyingTilesErase emits the routine in ROM which erases floor tiles lifted by a remote tile room overlord like
// Tile_DeleteFloor. It is called by generated flying tiles update routines in 16-bit mode (REP #$30) with JSL through
// the SRAM stub at flyingTilesStubAddr, with A = address of the table in updateDataBank:
//
//	dw slot            ; the overlord's slot, for the high bytes of its coordinates
//	dw count           ; how many tiles follow
//	db x, y ...        ; the low bytes of each tile's coordinates
//
// fnErase is UpdateTilemapWithCommonObject, called with A and X 8-bit. Returns with 16-bit registers.
func emitFlyingTilesErase(a *asm.Emitter, fnErase uint32) {
	bank := uint32(updateDataBank)
	ptr, count, slot := updScratchPtr, updScratchCount, updScratchTmp

	a.AssumeREP(0x30)
	a.Label("flyingTilesErase")
	a.TAX()
	a.LDA_long_x(bank + 0)
	a.STA_long(slot)
	a.LDA_long_x(bank + 2)
	a.STA_long(count)
	a.INX()
	a.INX()
	a.INX()
	a.INX()
	a.TXA()
	a.STA_long(ptr)
	a.Label("fe_loop")
	a.LDA_long(count)
	a.BEQ("fe_done")
	a.Comment("$00/$01 = X, $02/$03 = Y of the tile:")
	a.LDA_long(slot)
	a.TAY()
	a.LDA_long(ptr)
	a.TAX()
	a.SEP(0x20)
	a.LDA_long_x(bank + 0)
	a.STA_dp(0x00)
	a.LDA_abs_y(overlordXHi)
	a.STA_dp(0x01)
	a.LDA_long_x(bank + 1)
	a.STA_dp(0x02)
	a.LDA_abs_y(overlordYHi)
	a.STA_dp(0x03)
	a.SEP(0x30)
	a.LDY_imm8_b(0x06)
	if fnErase != 0 {
		a.JSL(fnErase)
	}
	a.REP(0x30)
	a.LDA_long(ptr)
	a.INC()
	a.INC()
	a.STA_long(ptr)
	a.LDA_long(count)
	a.DEC()
	a.STA_long(count)
	a.BRA("fe_loop")
	a.Label("fe_done")
	a.RTL()
}
