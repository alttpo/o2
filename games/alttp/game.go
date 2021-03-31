package alttp

import (
	"log"
	"o2/client"
	"o2/client/protocol01"
	"o2/client/protocol02"
	"o2/snes"
	"strings"
)

type ReadOp int

const (
	ReadMain ReadOp = iota
	ReadInventory

	ReadCount
)

// implements game.Game
type Game struct {
	rom   *snes.ROM
	queue snes.Queue
	client *client.Client

	running bool

	reads   [ReadCount]snes.Read
	cmdSeqs [ReadCount]snes.CommandSequence

	wram [0x20000]byte

	lastGameFrame uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame    uint64 // total frame count since start of local game
	serverFrame   uint64 // total frame count according to server (taken from first player to enter group)
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

func (g *Game) handleRead(rsp snes.Response) {
	//log.Printf("\n%s\n", hex.Dump(rsp.Data))

	// copy data read into our wram array:
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	if rsp.Address >= 0xF50000 && rsp.Address < 0xF70000 {
		copy(g.wram[rsp.Address-0xF50000:], rsp.Data)
	}
}

// run in a separate goroutine
func (g *Game) run() {
	q := g.queue

	readCompletion := make(chan snes.Response)

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	g.reads[ReadMain] = snes.Read{Address: 0xF50010, Size: 0xF0, Extra: g.readMainComplete}
	g.reads[ReadInventory] = snes.Read{Address: 0xF5F340, Size: 0xF0, Extra: nil}

	for i, r := range g.reads {
		r.Completion = readCompletion
		g.cmdSeqs[i] = q.MakeReadCommands(r)
	}

	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)
	q.EnqueueMulti(g.cmdSeqs[ReadMain])

	for {
		select {
		// wait for SNES memory read completion:
		case rsp := <-readCompletion:
			if !g.IsRunning() {
				break
			}

			// copy the data into our wram shadow:
			g.handleRead(rsp)

			complete := rsp.Extra.(func())
			if complete != nil {
				complete()
			}

			break

		// wait for network message from server:
		case msg := <-g.client.Read():
			err := g.handleNetMessage(msg)
			if err != nil {
				break
			}

			//g.queue.Enqueue(g.queue.MakeWriteCommands(snes.Write{
			//	Address:    0,
			//	Size:       0,
			//	Data:       nil,
			//	Extra:      nil,
			//	Completion: nil,
			//}))
			break
		}
	}
}

func (g *Game) Stop() {
	g.running = false
}

func (g *Game) readMainComplete() {
	q := g.queue

	// requeue the main read:
	q.EnqueueMulti(g.cmdSeqs[ReadMain])

	// did game frame change?
	if g.wram[0x1A] == g.lastGameFrame {
		return
	}

	// increment frame timer:
	lastFrame := uint64(g.lastGameFrame)
	nextFrame := uint64(g.wram[0x1A])
	if nextFrame < lastFrame {
		nextFrame += 256
	}
	g.localFrame += nextFrame - lastFrame
	g.lastGameFrame = g.wram[0x1A]

	log.Printf("%08x\n", g.localFrame)

	if g.localFrame&31 == 0 {
		// TODO: send inventory update to server
	}
}

func (g *Game) handleNetMessage(msg []byte) (err error) {
	var protocol uint8

	r, err := client.ParseHeader(msg, &protocol)
	if err != nil {
		return
	}

	switch protocol {
	case 1:
		var header protocol01.Header
		err = protocol01.Parse(r, &header)
		if err != nil {
			return
		}
		return
	case 2:
		var header protocol02.Header
		err = protocol02.Parse(r, &header)
		if err != nil {
			return
		}
		return
	default:
		return
	}
}
