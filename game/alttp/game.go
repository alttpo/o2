package alttp

import (
	"o2/snes"
	"strings"
)

// implements game.Game
type Game struct {
	rom  *snes.ROM
	conn snes.Conn

	stopping bool
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
	if rc, ok := g.conn.(snes.ROMControl); ok {
		path, cmds := rc.MakeUploadROMCommands(g.rom.Name, g.rom.Contents)
		g.conn.EnqueueMulti(cmds)
		g.conn.EnqueueMulti(rc.MakeBootROMCommands(path))
	}

	g.conn.EnqueueMulti(
		g.conn.MakeReadCommands([]snes.ReadRequest{
			{
				Address:   0xF50010,
				Size:      0xF0,
				Completed: nil,
			},
		}),
	)
}

func (g *Game) Stop() {
	g.stopping = true
	g.conn.Enqueue(&snes.CloseCommand{})
}
