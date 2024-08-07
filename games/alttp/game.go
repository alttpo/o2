package alttp

import (
	"encoding/json"
	"log"
	"o2/engine"
	"o2/games"
	"o2/interfaces"
	"o2/snes"
	"o2/util"
	"strings"
	"sync"
	"time"
	"unsafe"
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
	client games.Client
	// configurationSystem will only be nil until provided
	configurationSystem interfaces.ConfigurationSystem
	// viewModels can be nil at any time
	viewModels interfaces.ViewModelContainer

	priorityReadsMu sync.Mutex
	priorityReads   [3][]snes.Read

	deserTable []DeserializeFunc

	// Notifications publishes notifications about game events intended for the player to see
	Notifications interfaces.ObservableImpl

	stateLock sync.Mutex

	local   *Player
	players [MaxPlayers]Player

	activePlayersClean    bool
	activePlayers         []*Player
	remotePlayers         []*Player
	remoteSyncablePlayers []games.SyncablePlayer

	lastServerTime     time.Time // server's clock when the echo message arrived at server
	lastServerSentTime time.Time // our local clock when we sent the echo message
	lastServerRecvTime time.Time // our local clock when we received the echo reply

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
	lastUpdateFrame  uint8
	lastUpdateTime   time.Time
	updateGenerators []games.AsmExecConfirmer
	generated        map[uint32]struct{}
	cooldownTime     time.Time

	customAsmLock sync.Mutex
	customAsm     []byte

	locHashTTL int
	locHash    uint64

	// game-valid memory:
	wram          [0x20000]byte
	wramLastFrame [0x20000]byte
	wramFresh     [0x20000]bool
	//sram          [0x10000]byte
	notFirstFrame bool

	syncing       bool
	lastModule    int
	lastSubModule int

	syncable        [0x10000]games.SyncStrategy
	syncableOffsMin uint32
	syncableOffsMax uint32

	underworld [0x128]syncableUnderworld
	overworld  [0xC0]syncableOverworld

	romFunctions map[romFunction]uint32

	lastGameFrame      uint8 // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	monotonicFrameTime uint8 // always increments by 1 whenever game frame increases by any amount N

	shouldUpdatePlayersList bool

	runMinStart        time.Time
	runMaxFinish       time.Time
	runPlayersFinished int

	colorPendingUpdate int
	colorUpdatedTo     uint16
	last15             uint8

	// serializable ViewModel:
	clean            bool
	IsCreated        bool   `json:"isCreated"`
	GameName         string `json:"gameName"`
	PlayerColor      uint16 `json:"playerColor"`
	SyncItems        bool   `json:"syncItems"`
	SyncDungeonItems bool   `json:"syncDungeonItems"`
	SyncProgress     bool   `json:"syncProgress"`
	SyncHearts       bool   `json:"syncHearts"`
	SyncSmallKeys    bool   `json:"syncSmallKeys"`
	SyncUnderworld   bool   `json:"syncUnderworld"`
	SyncOverworld    bool   `json:"syncOverworld"`
	SyncChests       bool   `json:"syncChests"`
	lastSyncChests   bool
	SyncTunicColor   bool `json:"syncTunicColor"`
}

func (f *Factory) NewGame(rom *snes.ROM) games.Game {
	return NewGame(rom)
}

func NewGame(rom *snes.ROM) (g *Game) {
	if rom == nil {
		panic("alttp: rom cannot be nil")
	}

	g = &Game{
		rom:                   rom,
		running:               false,
		stopped:               make(chan struct{}),
		readComplete:          make(chan []snes.Response, 8),
		romFunctions:          make(map[romFunction]uint32),
		lastUpdateTarget:      0xFFFFFF,
		lastServerSentTime:    time.Now(),
		lastServerRecvTime:    time.Now(),
		activePlayers:         make([]*Player, 0, MaxPlayers),
		remotePlayers:         make([]*Player, 0, MaxPlayers),
		remoteSyncablePlayers: make([]games.SyncablePlayer, 0, MaxPlayers),
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

	g.initSerde()
	g.fillRomFunctions()

	return g
}

func (g *Game) Name() string {
	return gameName
}

func (g *Game) Title() string {
	return "ALTTP"
}

func (g *Game) Description() string {
	return strings.TrimRight(string(g.rom.Header.Title[:]), " ")
}

func (g *Game) LoadConfiguration(config json.RawMessage) {
	// kind of dirty to just unmarshal the public `json` tagged fields, but it works:
	err := json.Unmarshal(config, g)
	if err != nil {
		log.Printf("alttp: loadConfiguration: %v\n", err)
		return
	}
	g.IsCreated = true
	g.local.PlayerColor = g.PlayerColor
}

func (g *Game) ConfigurationModel() interface{} {
	// kind of dirty to just marshal the public `json` tagged fields, but it works:
	return g
}

func (g *Game) FirstFrame() {
	// must reset any state waiting on connected device:
	g.updateStage = 0
	g.colorPendingUpdate = 0
	g.lastUpdateTarget = 0xFFFFFF
	g.updateGenerators = make([]games.AsmExecConfirmer, 0, 20)
	g.notFirstFrame = false
	g.lastGameFrame = 0xFF

	// an impossible color in 15-bit BGR:
	g.colorUpdatedTo = 0xffff

	// don't set WRAM timestamps on first read from SNES:
	g.notFirstWRAMRead = false

	// initialize WRAM:
	for i := range g.wram {
		g.wram[i] = 0x00
		g.wramLastFrame[i] = 0x00
		g.wramFresh[i] = false
	}
}

func (g *Game) ProvideQueue(queue snes.Queue) {
	g.queue = queue

	g.FirstFrame()
}
func (g *Game) ProvideClient(client games.Client) {
	g.client = client
}
func (g *Game) ProvideViewModelContainer(container interfaces.ViewModelContainer) {
	g.viewModels = container
	if g.viewModels != nil {
		g.viewModels.NotifyView("game", g)
	}
}
func (g *Game) ProvideConfigurationSystem(configurationSystem interfaces.ConfigurationSystem) {
	g.configurationSystem = configurationSystem
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
		g.local.NameF = value.(string)
		g.updatePlayersList()
		break
	}
}

func (g *Game) IsRunning() bool {
	return g.running
}

func (g *Game) Reset() {
	g.stateLock.Lock()
	defer g.stateLock.Unlock()

	g.clean = false

	g.FirstFrame()

	g.ClearNotificationHistory()

	// clear out players array:
	for i := range g.players {
		g.players[i] = Player{IndexF: -1, PlayerColor: 0x12ef}
		g.players[i].SRAM.data = new([0x500]byte)
		g.players[i].SRAM.fresh = new([0x500]bool)
	}

	// create a temporary Player instance until we get our Index assigned from the server:
	g.local = &Player{IndexF: -1, PlayerColor: 0x12ef}
	local := g.local
	local.WRAM = make(map[uint16]*SyncableWRAM)
	local.SRAM.data = (*[0x500]byte)(unsafe.Pointer(&g.wram[0xF000]))
	local.SRAM.fresh = (*[0x500]bool)(unsafe.Pointer(&g.wramFresh[0xF000]))

	if g.viewModels != nil {
		// preserve last-set info:
		serverViewModelIntf, ok := g.viewModels.GetViewModel("server")
		if ok && serverViewModelIntf != nil {
			serverViewModel := serverViewModelIntf.(*engine.ServerViewModel)
			local.NameF = serverViewModel.PlayerName
			local.Team = serverViewModel.Team
		}
	}

	g.initSync()
}

// SoftReset keeps local player settings but clears all state
func (g *Game) SoftReset() {
	g.stateLock.Lock()
	defer g.stateLock.Unlock()

	g.clean = false

	g.FirstFrame()

	g.ClearNotificationHistory()

	var backupLocal Player
	if g.local != nil {
		backupLocal = *g.local
	}
	if g.viewModels != nil {
		// preserve last-set info:
		serverViewModelIntf, ok := g.viewModels.GetViewModel("server")
		if ok && serverViewModelIntf != nil {
			serverViewModel := serverViewModelIntf.(*engine.ServerViewModel)
			backupLocal.NameF = serverViewModel.PlayerName
			backupLocal.Team = serverViewModel.Team
		}
		backupLocal.PlayerColor = g.PlayerColor
	}

	// clear out players array:
	for i := range g.players {
		g.players[i] = Player{IndexF: -1, PlayerColor: 0x12ef}
		g.players[i].SRAM.data = new([0x500]byte)
		g.players[i].SRAM.fresh = new([0x500]bool)
	}

	// create a temporary Player instance until we get our Index assigned from the server:
	g.local = &Player{IndexF: -1, PlayerColor: 0x12ef}
	local := g.local
	local.WRAM = make(map[uint16]*SyncableWRAM)
	local.SRAM.data = (*[0x500]byte)(unsafe.Pointer(&g.wram[0xF000]))
	local.SRAM.fresh = (*[0x500]bool)(unsafe.Pointer(&g.wramFresh[0xF000]))
	local.NameF = backupLocal.NameF
	local.PlayerColor = backupLocal.PlayerColor
	local.Team = backupLocal.Team

	g.initSync()
}

func (g *Game) Start() {
	if g.running {
		return
	}
	g.running = true

	g.NotifyView()

	go func() {
		defer func() {
			if err := recover(); err != nil {
				util.LogPanic(err)
			}

			// notify that the game is stopped:
			close(g.stopped)
		}()

		// run the game loop:
		g.run()
	}()

	go g.sendReads()
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

func (g *Game) LocalPlayer() *Player {
	return g.local
}

func (g *Game) ActivePlayers() []*Player {
	if !g.activePlayersClean {
		g.activePlayers = g.activePlayers[:0]
		g.remotePlayers = g.remotePlayers[:0]
		g.remoteSyncablePlayers = g.remoteSyncablePlayers[:]

		for i, p := range g.players {
			if p.Index() < 0 {
				continue
			}
			if p.TTL() <= 0 {
				continue
			}

			g.activePlayers = append(g.activePlayers, &g.players[i])

			if g.local != &g.players[i] {
				g.remotePlayers = append(g.remotePlayers, &g.players[i])
				g.remoteSyncablePlayers = append(g.remoteSyncablePlayers, &g.players[i])
			}
		}

		g.activePlayersClean = true
	}

	return g.activePlayers
}

func (g *Game) RemotePlayers() []*Player {
	g.ActivePlayers()
	return g.remotePlayers
}

func (g *Game) LocalSyncablePlayer() games.SyncablePlayer {
	return g.local
}

func (g *Game) RemoteSyncablePlayers() []games.SyncablePlayer {
	g.ActivePlayers()
	return g.remoteSyncablePlayers
}

func (g *Game) ServerNow() time.Time {
	return g.lastServerTime.Add(time.Now().Sub(g.lastServerRecvTime))
}

// ServerSNESTimestamp returns ServerNow() time in milliseconds quantized to idealized SNES framerate
func (g *Game) ServerSNESTimestamp() time.Time {
	// SNES master clock ~= 1.89e9/88 Hz
	// SNES runs 1 scanline every 1364 master cycles
	// Frames are 262 scanlines in non-interlace mode (1 scanline takes 1360 clocks every other frame)
	// 1 frame takes 357366 master clocks
	const snes_frame_clocks = (261 * 1364) + 1362

	const snes_frame_nanoclocks = snes_frame_clocks * 1_000_000_000

	// 1 frame takes 0.016639356613757 seconds
	// 0.016639356613757 seconds = 16,639,356.613757 nanoseconds

	const snes_frame_time_nanoseconds_int = int64(snes_frame_nanoclocks) / (int64(1.89e9) / int64(88))

	st := g.ServerNow()
	sn := (st.UnixNano() / snes_frame_time_nanoseconds_int) * snes_frame_time_nanoseconds_int
	return time.Unix(0, sn)
}
