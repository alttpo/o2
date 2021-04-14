package alttp

import (
	"encoding/binary"
	"log"
)

type Module uint8

func (m Module) IsOverworld() bool {
	return m == 0x09 || m == 0x0B
}

func (m Module) IsInGame() bool {
	// bad modules to be in:
	// 0x00 - Triforce / Zelda startup screens
	// 0x01 - File Select screen
	// 0x02 - Copy Player Mode
	// 0x03 - Erase Player Mode
	// 0x04 - Name Player Mode
	// 0x05 - Loading Game Mode
	// 0x06 - Pre Dungeon Mode
	if m < 0x07 {
		return false
	}
	// 0x1B - Screen to select where to start from (House, sanctuary, etc.)
	if m > 0x1A {
		return false
	}
	// 0x14 - Attract Mode
	if m == 0x14 {
		return false
	}
	// 0x17 - Quitting mode (save and quit)
	if m == 0x17 {
		return false
	}

	// good modules:
	// 0x07 - Dungeon Mode
	// 0x08 - Pre Overworld Mode
	// 0x09 - Overworld Mode
	// 0x0A - Pre Overworld Mode (special overworld)
	// 0x0B - Overworld Mode (special overworld)
	// 0x0C - ???? I think we can declare this one unused, almost with complete certainty.
	// 0x0D - Blank Screen
	// 0x0E - Text Mode/Item Screen/Map
	// 0x0F - Closing Spotlight
	// 0x10 - Opening Spotlight
	// 0x11 - Happens when you fall into a hole from the OW.
	// 0x12 - Death Mode
	// 0x13 - Boss Victory Mode (refills stats)
	// 0x15 - Module for Magic Mirror
	// 0x16 - Module for refilling stats after boss.
	// 0x18 - Ganon exits from Agahnim's body. Chase Mode.
	// 0x19 - Triforce Room scene
	// 0x1A - End sequence
	return true
}

type Player struct {
	g *Game

	Index int
	TTL   int

	Team uint8
	Name string

	Frame uint8

	Module       Module
	SubModule    uint8
	SubSubModule uint8
	Location     uint32

	X uint16
	Y uint16

	Dungeon         uint16
	DungeonEntrance uint16

	LastOverworldX uint16
	LastOverworldY uint16

	XOffs int16
	YOffs int16

	PlayerColor uint16

	SRAM [0x500]byte
}

func (p *Player) SetTTL(ttl int) {
	if p.TTL <= 0 && ttl > 0 {
		// Activating new player:
		p.g.activePlayersClean = false
	}

	p.TTL = ttl
}

func (p *Player) DecTTL() {
	if p.TTL <= 0 {
		return
	}

	p.TTL--
	if p.TTL <= 0 {
		log.Printf("[%02x]: %s left\n", uint8(p.Index), p.Name)
		p.g.activePlayersClean = false
	}
}

func (p *Player) sramU16(offset uint16) uint16 {
	return binary.LittleEndian.Uint16(p.SRAM[offset : offset+2])
}
