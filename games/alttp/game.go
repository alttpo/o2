package alttp

import (
	"o2/client"
	"o2/games"
	"o2/interfaces"
	"o2/snes"
	"strings"
	"sync"
)

// MaxPlayers can extend to 65536 theoretical max due to use of uint16 for player indexes in protocol
const MaxPlayers = 256

// Game implements game.Game
type Game struct {
	rom          *snes.ROM
	queue        snes.Queue
	client       *client.Client
	viewNotifier interfaces.ViewNotifier

	localIndex int // index into the players array that local points to (or -1 if not connected)
	local      *Player
	players    [MaxPlayers]Player

	activePlayersClean bool
	activePlayers      []*Player

	running bool

	readQueue             []snes.Read
	readResponse          []snes.Response
	readCompletionChannel chan []snes.Response

	nextUpdateA      bool
	updateLock       sync.Mutex
	updateStage      int
	lastUpdateTarget uint32

	customAsmLock sync.Mutex
	customAsm     []byte

	locHashTTL int
	locHash    uint64

	wram [0x20000]byte
	sram [0x10000]byte

	syncableItems map[uint16]SyncableItem

	romFunctions map[romFunction]uint32

	lastGameFrame      uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame         uint64 // total frame count since start of local game
	serverFrame        uint64 // total frame count according to server (taken from first player to enter group)
	monotonicFrameTime uint8  // always increments by 1 whenever game frame increases by any amount N

	// serializable ViewModel:
	clean            bool
	IsCreated        bool   `json:"isCreated"`
	Team             uint8  `json:"team"`
	PlayerName       string `json:"playerName"`
	SyncItems        bool   `json:"syncItems"`
	SyncDungeonItems bool   `json:"syncDungeonItems"`
	SyncProgress     bool   `json:"syncProgress"`
	SyncHearts       bool   `json:"syncHearts"`
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
		romFunctions:          make(map[romFunction]uint32),
		lastUpdateTarget:      0xFFFFFF,
		// ViewModel:
		IsCreated:        true,
		SyncItems:        true,
		SyncDungeonItems: true,
		SyncProgress:     true,
		SyncHearts:       true,
	}

	g.fillRomFunctions()

	return g, nil
}

func (g *Game) Title() string {
	return "ALTTP"
}

func (g *Game) Description() string {
	return strings.TrimRight(string(g.rom.Header.Title[:]), " ")
}

func (g *Game) IsRunning() bool {
	return g.running
}

func (g *Game) Reset() {
	g.clean = false

	// clear out players array:
	for i := range g.players {
		g.players[i] = Player{g: g}
	}

	// create a temporary Player instance until we get our Index assigned from the server:
	g.localIndex = -1
	g.local = &Player{g: g, Index: -1}
	// preserve last-set info:
	g.local.Name = g.PlayerName
	g.local.Team = g.Team

	// initialize WRAM to non-zero values:
	for i := range g.wram {
		g.wram[i] = 0xFF
	}

	g.initSync()

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

func (g *Game) ActivePlayers() []*Player {
	if !g.activePlayersClean {
		g.activePlayers = make([]*Player, 0, len(g.activePlayers))
		for i, p := range g.players {
			if p.Index < 0 {
				continue
			}
			if p.TTL <= 0 {
				continue
			}

			g.activePlayers = append(g.activePlayers, &g.players[i])
		}
		g.activePlayersClean = true
	}

	return g.activePlayers
}
