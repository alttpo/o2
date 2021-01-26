package alttp

import (
	"log"
	"o2/snes"
	"strings"
	"time"
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

func (g *Game) Load() {
	if rc, ok := g.conn.(snes.ROMControl); ok {
		path, cmds := rc.MakeUploadROMCommands(g.rom.Name, g.rom.Contents)
		g.conn.EnqueueMulti(cmds)
		g.conn.EnqueueMulti(rc.MakeBootROMCommands(path))
	}
}

func (g *Game) Start() {
	var lastQueued time.Time
	var cmdReadMain snes.CommandSequence
	cmdReadMain = g.conn.MakeReadCommands([]snes.ReadRequest{
		{
			Address: 0xF50010,
			Size:    0xF0,
			Completed: func(rsp snes.ReadOrWriteResponse) {
				now := time.Now()
				log.Printf("%v, %v\n", now.Sub(lastQueued).Microseconds(), rsp.Data[0x0A])
				//log.Printf("\n%s\n", hex.Dump(rsp.Data))
				lastQueued = now
				g.conn.EnqueueMulti(cmdReadMain)
			},
		},
	})

	lastQueued = time.Now()
	g.conn.EnqueueMulti(cmdReadMain)
}

func (g *Game) Stop() <-chan struct{} {
	c := make(chan struct{})

	g.stopping = true
	g.conn.EnqueueWithCallback(
		&snes.DrainQueueCommand{},
		func(error) {
			defer close(c)
			c <- struct{}{}
		})

	return c
}
