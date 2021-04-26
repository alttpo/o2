package alttp

import (
	"o2/client"
	"o2/engine"
	"o2/games"
	"o2/interfaces"
	"o2/snes"
	"strings"
	"sync"
	"time"
)

// MaxPlayers can extend to 65536 theoretical max due to use of uint16 for player indexes in protocol
const MaxPlayers = 256

// Game implements game.Game
type Game struct {
	// rom cannot be nil
	rom *snes.ROM

	// queue can be nil at any time
	queue snes.Queue
	// client can be nil at any time
	client *client.Client
	// viewModels can be nil at any time
	viewModels       interfaces.ViewModelContainer
	nextNotification string

	local   *Player
	players [MaxPlayers]Player

	activePlayersClean bool
	activePlayers      []*Player

	running bool
	stopped chan struct{}

	readResponseLock sync.Mutex
	readResponse     []snes.Response

	readComplete chan []snes.Response

	lastReadCompleted time.Time
	notFirstWRAMRead  bool

	nextUpdateA      bool
	updateLock       sync.Mutex
	updateStage      int
	lastUpdateTarget uint32

	customAsmLock sync.Mutex
	customAsm     []byte

	locHashTTL int
	locHash    uint64

	// staging area to read data into first before validating e.g. not in reset state or in SD2SNES menu, etc.:
	wramStaging [0x20000]byte
	// game-valid memory:
	wram [0x20000]byte
	sram [0x10000]byte

	invalid bool

	syncableItems  map[uint16]SyncableItem
	underworld     [0x128]syncableBitU16
	overworld      [0xC0]syncableBitU8
	syncableBitU16 map[uint16]*syncableBitU16

	romFunctions map[romFunction]uint32

	lastGameFrame      uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame         uint64 // total frame count since start of local game
	serverFrame        uint64 // total frame count according to server (taken from first player to enter group)
	monotonicFrameTime uint8  // always increments by 1 whenever game frame increases by any amount N

	shouldUpdatePlayersList bool

	// serializable ViewModel:
	clean                   bool
	IsCreated               bool   `json:"isCreated"`
	GameName                string `json:"gameName"`
	SyncItems               bool   `json:"syncItems"`
	SyncDungeonItems        bool   `json:"syncDungeonItems"`
	SyncProgress            bool   `json:"syncProgress"`
	SyncHearts              bool   `json:"syncHearts"`
	SyncSmallKeys           bool   `json:"syncSmallKeys"`
	SyncUnderworld          bool   `json:"syncUnderworld"`
	SyncOverworld           bool   `json:"syncOverworld"`
	SyncChests              bool   `json:"syncChests"`
	lastSyncChests          bool
}

func (f *Factory) NewGame(rom *snes.ROM) games.Game {
	if rom == nil {
		panic("alttp: rom cannot be nil")
	}

	g := &Game{
		rom:              rom,
		running:          false,
		stopped:          make(chan struct{}),
		readComplete:     make(chan []snes.Response, 256),
		romFunctions:     make(map[romFunction]uint32),
		lastUpdateTarget: 0xFFFFFF,
		// ViewModel:
		IsCreated:        true,
		GameName:         gameName,
		SyncItems:        true,
		SyncDungeonItems: true,
		SyncProgress:     true,
		SyncHearts:       true,
		SyncSmallKeys:    true,
		SyncUnderworld:   true,
		SyncOverworld:    true,
		SyncChests:       true,
		lastSyncChests:   false,
	}

	g.fillRomFunctions()

	return g
}

func (g *Game) Title() string {
	return "ALTTP"
}

func (g *Game) Description() string {
	return strings.TrimRight(string(g.rom.Header.Title[:]), " ")
}

func (g *Game) ProvideQueue(queue snes.Queue)       { g.queue = queue }
func (g *Game) ProvideClient(client *client.Client) { g.client = client }
func (g *Game) ProvideViewModelContainer(container interfaces.ViewModelContainer) {
	g.viewModels = container
}

// Notify is called by root ViewModel
func (g *Game) Notify(key string, value interface{}) {
	//log.Printf("alttp: notify('%s', '%+v')\n", key, value)
	switch key {
	case "team":
		g.local.Team = value.(uint8)
		g.updatePlayersList()
		break
	case "playerName":
		g.local.Name = value.(string)
		g.updatePlayersList()
		break
	}
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
	g.local = &Player{g: g, Index: -1}
	local := g.local
	local.WRAM = make(map[uint16]*SyncableWRAM)

	// preserve last-set info:
	serverViewModelIntf, ok := g.viewModels.GetViewModel("server")
	if ok && serverViewModelIntf != nil {
		serverViewModel := serverViewModelIntf.(*engine.ServerViewModel)
		local.Name = serverViewModel.PlayerName
		local.Team = serverViewModel.Team
	}

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

	go func() {
		// run the game loop:
		g.run()

		// notify that the game is stopped:
		close(g.stopped)
	}()
}

func (g *Game) Stopped() <-chan struct{} {
	return g.stopped
}

func (g *Game) Stop() {
	// signal to stop the game:
	g.running = false

	// wait until stopped:
	<-g.stopped
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
