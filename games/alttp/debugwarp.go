package alttp

import (
	"log"
	"o2/snes"

	"github.com/alttpo/snes/asm"
)

// Warps of the debug page
//
// A warp goes through the iris like a door does: the game's module $0F closes the spotlight and then runs the module
// in $010C, which loads the place:
//
//   - to an underworld entrance, module $06 (Module_PreDungeon) loads entrance $010E, like walking into a door of the
//     overworld (Overworld_Entrance);
//   - to an overworld exit, module $08 (LoadOverworldFromDungeon) loads the exit of room $A0 from the underworld exit
//     table, like walking out of a dungeon (Dung_HandleExitToOverworld).
//
// The page names the places by their exit's index in the underworld exit table (UnderworldExitData). Randomizers that
// shuffle entrances rewrite the table's exits to where they put the entrances, so the warps follow them. The exit's
// area also tells the world of the place, which the warp sets ($7EF3CA) so that the game loads the right world.

const (
	// UnderworldExitData.room_id, 79 words, followed by .overworld_id, 79 bytes:
	romExitRoomsUS = 0x02DD8A
	romExitRoomsJP = 0x02DAEE
	exitTableCount = 0x4F
	// the last entrance of the entrance table:
	maxEntrance = 0x84
)

func (g *Game) romExitRooms() uint32 {
	if g.rom.Header.DestinationCode == snes.RegionNorthAmerica && g.rom.Header.MaskROMVersion == 0 {
		return romExitRoomsUS
	}
	return romExitRoomsJP
}

// debugExit returns the room and the overworld area of exit i of the ROM's underworld exit table.
func (g *Game) debugExit(i uint8) (room uint16, area uint8, ok bool) {
	base := g.romExitRooms()
	room, ok1 := g.romU16(base + uint32(i)*2)
	area, ok2 := g.romU8(base + exitTableCount*2 + uint32(i))
	return room, area, ok1 && ok2
}

// exitUsesTable reports whether LoadOverworldFromDungeon takes the exit of a room from the exit table; it takes the
// exit of the other rooms (the houses $0100..$017F but Link's house) from where Link entered them.
func exitUsesTable(room uint16) bool {
	return room < 0x100 || room == 0x104
}

// debugCanWarp reports whether Link is on his feet in the overworld or the underworld, where a door could be taken.
// Called with the state lock held.
func (g *Game) debugCanWarp() bool {
	m := g.wram[0x10]
	return (m == 0x07 || m == 0x09) && g.wram[0x11] == 0
}

func (g *Game) emitDebugWarp(a *asm.Emitter, op *debugOp, lbl func(string) string) {
	room, area, ok := g.debugExit(op.V)
	if !ok {
		log.Printf("alttp: debug: warp: exit $%02x is not in the ROM\n", op.V)
		return
	}
	if op.W == "exit" && !exitUsesTable(room) {
		log.Printf("alttp: debug: warp: exit $%02x of room $%03x is not in the exit table\n", op.V, room)
		return
	}
	world := uint8(0)
	if area >= 0x40 {
		world = 0x40
	}

	// the update routine also runs in the menus; take the door only from the overworld or the underworld:
	done, go_, keys := lbl("done"), lbl("go"), lbl("keys")
	a.LDA_dp(0x10)
	a.CMP_imm8_b(0x07)
	a.BEQ(go_)
	a.CMP_imm8_b(0x09)
	a.BNE(done)
	a.Label(go_)
	a.LDA_dp(0x11)
	a.BNE(done)

	// keep the small keys of the dungeon Link leaves, like SaveDungeonKeys:
	a.LDA_abs(0x040C)
	a.CMP_imm8_b(0xFF)
	a.BEQ(keys)
	a.CMP_imm8_b(0x02)
	a.BNE(lbl("hc"))
	a.LDA_imm8_b(0x00)
	a.Label(lbl("hc"))
	a.LSR()
	a.TAX()
	a.LDA_long(0x7EF36F)
	a.STA_long_x(0x7EF37C)
	a.Label(keys)

	a.LDA_imm8_b(world)
	a.STA_long(0x7EF3CA)

	if op.W == "entrance" {
		// Dungeon_LoadEntrance keeps where Link entered from the overworld, for the exits of the houses, unless $010A
		// is set; from the underworld, keep the last one instead of the underworld's coordinates:
		over := lbl("over")
		a.LDA_dp(0x1B)
		a.BEQ(over)
		a.LDA_imm8_b(0x01)
		a.STA_abs(0x010A)
		a.Label(over)
		a.LDA_imm8_b(op.E)
		a.STA_abs(0x010E)
		a.STZ_abs(0x010F)
		a.LDA_imm8_b(0x06)
		a.STA_abs(0x010C)
	} else {
		a.LDA_imm8_b(uint8(room))
		a.STA_dp(0xA0)
		a.LDA_imm8_b(uint8(room >> 8))
		a.STA_dp(0xA1)
		a.LDA_imm8_b(0x08)
		a.STA_abs(0x010C)
	}

	// Link stands, like Overworld_Entrance does before the iris:
	a.STZ_dp(0x4D)
	a.STZ_dp(0x46)
	a.STZ_dp(0x5D)
	a.LDA_imm8_b(0x0F)
	a.STA_dp(0x10)
	a.STZ_dp(0x11)
	a.STZ_dp(0xB0)
	a.Label(done)
}
