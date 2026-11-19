package alttp

import (
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"o2/snes"
)

// Secrets under objects lifted by remote players
//
// When Link lifts a bush, rock or sign in the overworld (Overworld_HandleLiftableTiles) the game looks its position up
// in the area's hidden items (Overworld_RevealSecret), which sets the low byte of $0B9C to the item's secret index (0
// if none). Sprite_SpawnThrowableTerrain then spawns the carried object and, under it, the secret: for bushes without
// a hidden item Overworld_SubstituteAlternateSecret may pick a green rupee, an enemy or a bee instead, and in the
// overworld Sprite_SpawnSecret only spawns anything half of the time and picks a random prize for secret 4. Lifting a
// pot in the underworld looks it up in the room's pot items instead (RevealPotItem), which also marks the item taken
// in $7EF580 (except keys, whose sprite checks the room's key flags).
//
// The map16 and manipulable object syncs only show the remote player's lifted object as gone. When a remote player
// lifts one while the local player is in the same area or room, the ROM routine the generated update routine calls
// (map16Update or manipUpdate) first does the same lookup at that position (Overworld_RevealSecret or RevealPotItem) and calls Sprite_SpawnThrowableTerrain_silently
// there like the game, so the local player gets the same item (or the game's own random prize) to pick up, then
// removes the carried object sprite it spawned. $0B9C, $0314 and $0FB2 are restored afterwards.

// owLiftable is an overworld tile that Overworld_HandleLiftableTiles reveals a secret under when lifted, and the type
// of thrown object (the index into LinkToss_liftable_tiles) the game spawns for it.
type owLiftable struct {
	tile uint16
	toss uint8
}

// owLiftableTileOperands are the addresses of the tile operands of the CMP instructions Overworld_HandleLiftableTiles
// uses to find the small objects that reveal secrets (bush, dark bush, light rock, dark rock, sign), in US and JP
// ROMs. The tile numbers differ between them (the dark bush is $072A in US ROMs and $0727 in JP ROMs).
var owLiftableTileOperands = [...]uint32{0x1BBFE7, 0x1BBFEF, 0x1BBFF7, 0x1BBFFC, 0x1BC001}

const (
	// operands of Overworld_GetLiftableTileType's `LDA.l Map16Definitions,X` and `LDA.l OverworldTileTypes,X`:
	owMap16DefinitionsOperand   = uint32(0x1BC041)
	owOverworldTileTypesOperand = uint32(0x1BC04F)
)

// linkTossLiftableTiles is LinkToss_liftable_tiles: the tile types Link can lift, by thrown object type.
var linkTossLiftableTiles = [...]uint8{0x54, 0x52, 0x50, 0xFF, 0x51, 0x53, 0x55, 0x56, 0x57}

func romBusReadU8(rom *snes.ROM, addr uint32) (uint8, bool) {
	pak, err := lorom.BusAddressToPak(addr)
	if err != nil || int(pak) >= len(rom.Contents) {
		return 0, false
	}
	return rom.Contents[pak], true
}

func romBusReadU16(rom *snes.ROM, addr uint32) (uint16, bool) {
	lo, ok1 := romBusReadU8(rom, addr)
	hi, ok2 := romBusReadU8(rom, addr+1)
	return uint16(lo) | uint16(hi)<<8, ok1 && ok2
}

func romBusReadU24(rom *snes.ROM, addr uint32) (uint32, bool) {
	lo, ok1 := romBusReadU16(rom, addr)
	bank, ok2 := romBusReadU8(rom, addr+2)
	return uint32(lo) | uint32(bank)<<16, ok1 && ok2
}

// readOwLiftables reads the small liftable objects from the ROM's code, and their thrown object types the way
// Overworld_GetLiftableTileType finds them: the tile type (OverworldTileTypes) of the top left 8x8 tile of the map16
// tile (Map16Definitions), looked up in LinkToss_liftable_tiles.
func readOwLiftables(rom *snes.ROM) (list []owLiftable) {
	defs, ok1 := romBusReadU24(rom, owMap16DefinitionsOperand)
	types, ok2 := romBusReadU24(rom, owOverworldTileTypesOperand)
	if !ok1 || !ok2 {
		return nil
	}
	for _, op := range owLiftableTileOperands {
		tile, ok := romBusReadU16(rom, op)
		if !ok {
			return nil
		}
		chr, ok := romBusReadU16(rom, defs+uint32(tile)<<3)
		if !ok {
			continue
		}
		typ, ok := romBusReadU8(rom, types+uint32(chr&0x01FF))
		if !ok {
			continue
		}
		for k, t := range linkTossLiftableTiles {
			if t == typ {
				list = append(list, owLiftable{tile: tile, toss: uint8(k)})
				break
			}
		}
	}
	return
}

// owLiftablesFor returns the small liftable objects of the ROM, reading them once.
func (g *Game) owLiftablesFor() []owLiftable {
	if !g.owLiftablesRead {
		g.owLiftablesRead = true
		g.owLiftables = readOwLiftables(g.rom)
	}
	return g.owLiftables
}

// isOwLiftable reports whether tile is a small liftable object.
func (g *Game) isOwLiftable(tile uint16) bool {
	for _, l := range g.owLiftablesFor() {
		if l.tile == tile {
			return true
		}
	}
	return false
}

// emitSpawnSecretSub emits a subroutine (JSR, 16-bit A/X/Y in and out) that spawns the secret the game put in $0B9C
// at $00/$02 (X/Y) like Sprite_SpawnThrowableTerrain does, with Y = the thrown object type, then removes the carried
// object sprite it spawned. It restores $0314 and $0FB2, which Sprite_SpawnThrowableTerrain sets. Returns the
// subroutine's address.
func emitSpawnSecretSub(a *asm.Emitter, fnSpawn uint32) uint16 {
	addr := uint16(a.PC())
	a.Comment("spawn the secret in $0B9C at $00/$02 like lifting an object of type Y does:")
	a.LDA_abs(0x0314)
	a.PHA()
	a.LDA_abs(0x0FB2)
	a.PHA()
	a.SEP(0x30)
	a.TYA()
	a.JSL(fnSpawn)
	a.Comment("remove the carried object sprite, if one was spawned in slot X:")
	a.TXA()
	a.BMI("ss_none")
	a.STZ_abs_x(0x0DD0)
	a.Label("ss_none")
	a.REP(0x30)
	a.PLA()
	a.STA_abs(0x0FB2)
	a.PLA()
	a.STA_abs(0x0314)
	a.RTS()
	return addr
}

// emitOwRevealSub emits a subroutine (JSR, 16-bit A/X/Y in and out) that reveals the secret under the small liftable
// object at tilemap offset X like lifting it does, if $7E2000,X still holds one. spawnSub is the subroutine emitted by
// emitSpawnSecretSub. Returns the subroutine's address.
func emitOwRevealSub(a *asm.Emitter, liftables []owLiftable, fnReveal, fnRTL uint32, spawnSub uint16) uint16 {
	addr := uint16(a.PC())
	a.Comment("reveal the secret under the object at X if it is still there:")
	a.LDA_long_x(0x7E2000)
	for k, l := range liftables {
		a.LDY_imm16_w(uint16(l.toss))
		a.CMP_imm16_w(l.tile)
		if k == len(liftables)-1 {
			a.BEQ("or_found")
			a.RTS()
		} else {
			a.BEQ("or_found")
		}
	}
	a.Label("or_found")
	a.PHY()
	a.LDA_abs(0x0B9C)
	a.PHA()
	a.PHX()
	a.JSRBanked(fnReveal, fnRTL)
	a.PLX()
	a.Comment("$00 = X = (X & $7E) + $070C << 3, $02 = Y = (X & $1F80) >> 3 + $0708, like Overworld_GetLinkMap16Coords:")
	a.TXA()
	a.AND_imm16_w(0x007E)
	a.CLC()
	a.ADC_abs(0x070C)
	a.ASL()
	a.ASL()
	a.ASL()
	a.STA_dp(0x00)
	a.TXA()
	a.AND_imm16_w(0x1F80)
	a.LSR()
	a.LSR()
	a.LSR()
	a.CLC()
	a.ADC_abs(0x0708)
	a.STA_dp(0x02)
	a.Comment("the thrown object type:")
	a.LDA_sr(0x03)
	a.TAY()
	a.JSR_abs(spawnSub)
	a.PLA()
	a.STA_abs(0x0B9C)
	a.PLY()
	a.RTS()
	return addr
}

// potCoords returns the coordinates of the top left of the underworld tilemap position pos in room. Rooms are
// 512x512 pixels at (room & $0F, (room & $F0) >> 4) and the tilemap is 64x64 8x8 tiles.
func potCoords(room, pos uint16) (x, y uint16) {
	x = (room&0x0F)<<9 + (pos&0x007E)<<2
	y = (room&0xF0)<<5 + (pos&0x1F80)>>4
	return
}
