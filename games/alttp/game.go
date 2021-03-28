package alttp

import (
	"log"
	"o2/snes"
	"strings"
)

// implements game.Game
type Game struct {
	rom   *snes.ROM
	queue snes.Queue

	running bool

	wram [0x20000]byte

	lastGameFrame uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame    uint64 // total frame count since start of local game
	serverFrame   uint64 // total frame count according to server (taken from first player to enter group)
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

func (g *Game) handleRead(rsp snes.ReadOrWriteResponse) {
	//log.Printf("\n%s\n", hex.Dump(rsp.Data))

	// copy data read into our wram array:
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	if rsp.Address >= 0xF50000 && rsp.Address < 0xF70000 {
		copy(g.wram[rsp.Address-0xF50000:], rsp.Data)
	}
}

// run in a separate goroutine
func (g *Game) run() {
	readCompletion := make(chan snes.ReadOrWriteResponse)

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	cmdReadMain := g.queue.MakeReadCommands(snes.ReadRequest{Address: 0xF50010, Size: 0xF0, Completion: readCompletion})
	//cmdReadItems := g.queue.MakeReadCommands(snes.ReadRequest{Address: 0xF5F340, Size: 0xF0, Completion: readCompletion})

	g.queue.EnqueueMulti(cmdReadMain)

	for {
		select {
		case rsp := <-readCompletion:
			if !g.IsRunning() {
				break
			}

			// requeue the main read:
			g.queue.EnqueueMulti(cmdReadMain)
			g.handleRead(rsp)

			// increment frame timer:
			if g.wram[0x1A] != g.lastGameFrame {
				lastFrame := uint64(g.lastGameFrame)
				nextFrame := uint64(g.wram[0x1A])
				if nextFrame < lastFrame {
					nextFrame += 256
				}
				g.localFrame += nextFrame - lastFrame
				g.lastGameFrame = g.wram[0x1A]
				log.Printf("%08x\n", g.localFrame)
			}

			//now := time.Now()
			//log.Printf("%v, %v\n", now.Sub(lastQueued).Microseconds(), rsp.Data[0x0A])

			break
		}
	}
}

func (g *Game) Stop() {
	g.running = false
}
