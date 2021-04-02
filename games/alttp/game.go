package alttp

import (
	"bytes"
	"log"
	"o2/client"
	"o2/client/protocol02"
	"o2/games"
	"o2/interfaces"
	"o2/snes"
	"strings"
)

// can extend to 65536 theoretical max due to use of uint16 for player indexes in protocol
const MaxPlayers = 256

// implements game.Game
type Game struct {
	rom          *snes.ROM
	queue        snes.Queue
	client       *client.Client
	viewNotifier interfaces.ViewNotifier

	localIndex int // index into the players array that local points to (or -1 if not connected)
	local      *Player
	players    [MaxPlayers]Player

	running bool

	readQueue             []snes.Read
	readCompletionChannel chan snes.Response

	wram [0x20000]byte

	lastGameFrame uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame    uint64 // total frame count since start of local game
	serverFrame   uint64 // total frame count according to server (taken from first player to enter group)

	// serializable ViewModel:
	clean      bool
	Team       int    `json:"team"`
	PlayerName string `json:"playerName"`
}

func (f *Factory) NewGame(
	queue snes.Queue,
	rom *snes.ROM,
	client *client.Client,
	viewNotifier interfaces.ViewNotifier,
) (games.Game, error) {
	g := &Game{
		rom:                   rom,
		queue:                 queue,
		client:                client,
		viewNotifier:          viewNotifier,
		running:               false,
		readCompletionChannel: make(chan snes.Response, 8),
	}

	// initialize WRAM to non-zero values:
	for i := range g.wram {
		g.wram[i] = 0xFF
	}

	return g, nil
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

	// create a temporary Player instance until we get our Index assigned from the server:
	g.localIndex = -1
	g.local = &Player{Index: -1}

	go g.run()
}

func (g *Game) Stop() {
	g.running = false
}

func (g *Game) readEnqueue(addr uint32, size uint8, complete func()) {
	g.readQueue = append(g.readQueue, snes.Read{
		Address:    addr,
		Size:       size,
		Extra:      complete,
		Completion: g.readCompletionChannel,
	})
}

func (g *Game) readSubmit() {
	sequence := g.queue.MakeReadCommands(g.readQueue...)
	g.queue.EnqueueMulti(sequence)

	// TODO: consider just clearing length instead to avoid realloc
	g.readQueue = nil
}

func (g *Game) handleSNESRead(rsp snes.Response) {
	//log.Printf("\n%s\n", hex.Dump(rsp.Data))

	// copy data read into our wram array:
	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	if rsp.Address >= 0xF50000 && rsp.Address < 0xF70000 {
		copy(g.wram[rsp.Address-0xF50000:], rsp.Data)
	}
}

// run in a separate goroutine
func (g *Game) run() {
	// for more consistent response times from fx pak pro, adjust firmware.im3 to patch bInterval from 2ms to 1ms.
	// 0x1EA5D = 01 (was 02)

	// $F5-F6:xxxx is WRAM, aka $7E-7F:xxxx
	g.readEnqueue(0xF50010, 0xF0, g.readMainComplete)
	g.readSubmit()

	//readInventory: snes.Read{Address: 0xF5F340, Size: 0xF0, Extra: nil}

	for {
		select {
		// wait for SNES memory read completion:
		case rsp := <-g.readCompletionChannel:
			if !g.IsRunning() {
				break
			}

			// copy the data into our wram shadow:
			g.handleSNESRead(rsp)

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

func (g *Game) readMainComplete() {
	defer g.readSubmit()

	// requeue the main read:
	g.readEnqueue(0xF50010, 0xF0, g.readMainComplete)

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

	if g.localIndex < 0 {
		// request our player index:
		m := protocol02.MakePacket(g.client.Group(), protocol02.RequestIndex, uint16(0))
		g.Send(m)
	}

	if g.localFrame&31 == 0 {
		// TODO: send inventory update to server
	}
}

func (g *Game) Send(m *bytes.Buffer) {
	if !g.client.IsConnected() {
		return
	}

	g.client.Write() <- m.Bytes()
}
