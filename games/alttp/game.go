package alttp

import (
	"o2/client"
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
	readResponse          []snes.Response
	readCompletionChannel chan []snes.Response

	wram      [0x20000]byte
	wramDirty [0x20000]bool

	lastGameFrame uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame    uint64 // total frame count since start of local game
	serverFrame   uint64 // total frame count according to server (taken from first player to enter group)

	// serializable ViewModel:
	clean      bool
	IsCreated  bool   `json:"isCreated"`
	Team       uint8  `json:"team"`
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
		readCompletionChannel: make(chan []snes.Response, 8),
		IsCreated:             true,
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
		cmds.EnqueueTo(g.queue)
		rc.MakeBootROMCommands(path).EnqueueTo(g.queue)
	}
}

func (g *Game) IsRunning() bool {
	return g.running
}

func (g *Game) Reset() {
	g.clean = false

	// clear out players array:
	for i := range g.players {
		g.players[i] = Player{}
	}

	// create a temporary Player instance until we get our Index assigned from the server:
	g.localIndex = -1
	g.local = &Player{Index: -1}
	// preserve last-set info:
	g.local.Name = g.PlayerName
	g.local.Team = g.Team

	// initialize WRAM to non-zero values:
	for i := range g.wram {
		g.wram[i] = 0xFF
	}

	// inform the view:
	g.notifyView()
}

func (g *Game) Start() {
	if g.running {
		return
	}
	g.running = true

	g.Reset()

	go g.run()
}

func (g *Game) Stop() {
	g.running = false
}
