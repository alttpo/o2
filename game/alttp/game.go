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
		path, err := rc.UploadROM(g.rom.Name, g.rom.Contents)
		// TODO: handle errors
		_ = err
		err = rc.BootROM(path)
		// TODO: handle errors
		_ = err
	}

	g.conn.SubmitRead([]snes.ReadRequest{
		{
			Address:   0xF50010,
			Size:      0xF0,
			Completed: nil,
		},
	})
}

func (g *Game) Stop() {
	g.stopping = true
	g.conn.Close()
}
