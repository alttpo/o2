package alttp

import (
	"encoding/binary"
	"fmt"
	"log"
)

type Module uint8

func (m Module) IsOverworld() bool {
	return m == 0x09 || m == 0x0B
}

func (m Module) IsDungeon() bool {
	return m == 0x07
}

type SyncableWRAM struct {
	Name          string
	Size          uint8
	Timestamp     uint32
	Value         uint16
	ValueUsed     uint16
	IsWriting     bool
	ValueExpected uint16
}

type Player struct {
	Index int
	TTL   int

	Team uint8
	Name string

	Frame uint8

	Module       Module
	PriorModule  Module
	SubModule    uint8
	SubSubModule uint8

	OverworldArea uint16
	DungeonRoom   uint16
	Location      uint32

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

	WRAM            map[uint16]*SyncableWRAM
	showJoinMessage bool
}

func (g *Game) SetTTL(p *Player, ttl int) {
	joined := false
	if p.TTL <= 0 && ttl > 0 {
		joined = true
	}

	p.TTL = ttl
	if joined {
		g.PlayerJoined(p)
	}
}

func (g *Game) DecTTL(p *Player, amount int) {
	if p.TTL <= 0 {
		return
	}

	p.TTL -= amount
	if p.TTL <= 0 {
		g.PlayerLeft(p)
	}
}

func (g *Game) PlayerJoined(p *Player) {
	// Activating new player:
	p.showJoinMessage = true
	g.activePlayersClean = false
	g.shouldUpdatePlayersList = true
}

func (g *Game) PlayerLeft(p *Player) {
	// Player left the game:
	p.TTL = 0
	p.showJoinMessage = false

	log.Printf("alttp: player[%02x]: %s left\n", uint8(p.Index), p.Name)
	g.PushNotification(fmt.Sprintf("%s left", p.Name))

	// refresh the ActivePlayers():
	g.activePlayersClean = false

	// refresh the players list
	g.shouldUpdatePlayersList = true
}

func (p *Player) sramU16(offset uint16) uint16 {
	if offset >= 0x500 {
		return 0xFFFF
	}
	return binary.LittleEndian.Uint16(p.SRAM[offset : offset+2])
}

func (p *Player) IsInDungeon() bool {
	if p.IsDungeon() {
		return true
	}
	return p.Location&(1<<16) != 0
}

func (p *Player) IsInGame() bool {
	return p.isModuleInGame(p.Module)
}

func (p *Player) isModuleInGame(m Module) bool {
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
	if m == 0x0E {
		if p.PriorModule != 0x0E {
			return p.isModuleInGame(p.PriorModule)
		} else {
			return true
		}
	}
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

func (p *Player) IsDungeon() bool {
	return p.Module == 0x07
}
