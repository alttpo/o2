package alttp

type Module uint8

func (m Module) IsOverworld() bool {
	return m == 0x09 || m == 0x0B
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
		p.g.activePlayersClean = false
	}
}
