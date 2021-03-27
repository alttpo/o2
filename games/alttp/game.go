package alttp

import (
	"log"
	"o2/snes"
	"strings"
	"time"
)

// implements game.Game
type Game struct {
	rom   *snes.ROM
	queue snes.Queue

	running bool
}

func (g *Game) ROM() *snes.ROM {
	return g.rom
}

func (g *Game) SNES() snes.Queue {
	return g.queue
}

func (g *Game) Title() string {
	return "ALTTP"
}

func (g *Game) Description() string {
	return strings.TrimRight(string(g.rom.Header.Title[:]), " ")
}

func (g *Game) Load() {
	if rc, ok := g.queue.(snes.ROMControl); ok {
		path, cmds := rc.MakeUploadROMCommands(g.rom.Name, g.rom.Contents)
		g.queue.EnqueueMulti(cmds)
		g.queue.EnqueueMulti(rc.MakeBootROMCommands(path))
	}
}

func (g *Game) IsRunning() bool {
	return g.running
}

func (g *Game) Start() {
	if g.running {
		return
	}
	g.running = true

	go g.run()
}

// run in a separate goroutine
func (g *Game) run() {
	readResponse := make(chan snes.ReadOrWriteResponse)

	var lastQueued time.Time
	var cmdReadMain snes.CommandSequence
	cmdReadMain = g.queue.MakeReadCommands([]snes.ReadRequest{
		{
			Address:    0xF50010,
			Size:       0xF0,
			Completion: readResponse,
		},
	})

	lastQueued = time.Now()
	g.queue.EnqueueMulti(cmdReadMain)

	for {
		select {
		case rsp := <-readResponse:
			now := time.Now()
			log.Printf("%v, %v\n", now.Sub(lastQueued).Microseconds(), rsp.Data[0x0A])
			//log.Printf("\n%s\n", hex.Dump(rsp.Data))

			if g.IsRunning() {
				lastQueued = now
				g.queue.EnqueueMulti(cmdReadMain)
			}
			break
		}
	}
}

func (g *Game) Stop() {
	g.running = false
}
