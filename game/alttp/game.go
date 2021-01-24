package alttp

import (
	"o2/snes"
	"strings"
)

// implements game.Game
type Game struct {
	rom  *snes.ROM
	conn snes.Conn
}

func (g *Game) ROM() *snes.ROM {
	return g.rom
}

func (g *Game) SNES() snes.Conn {
	return g.conn
}

func (g *Game) Title() string {
	return "ALTTP"
}

func (g *Game) Description() string {
	return strings.TrimRight(string(g.rom.Header.Title[:]), " ")
}

func (g *Game) Start() {
	panic("implement me")
}

func (g *Game) Stop() {
	panic("implement me")
}
