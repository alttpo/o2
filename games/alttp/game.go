package alttp

import (
	"encoding/json"
	"log"
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
	// configurationSystem will only be nil until provided
	configurationSystem interfaces.ConfigurationSystem
	// viewModels can be nil at any time
	viewModels interfaces.ViewModelContainer

	deserTable []DeserializeFunc

	// Notifications publishes notifications about game events intended for the player to see
	Notifications interfaces.ObservableImpl

	local   *Player
	players [MaxPlayers]Player

	activePlayersClean bool
	activePlayers      []*Player

	// local clock offset from the alttp.online server:
	clockOffset  time.Duration
	clockServer  string
	clockQueried time.Time
	ntpC         chan int

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

	syncableItems  map[uint16]games.SyncStrategy
	underworld     [0x128]games.SyncableBitU16
	overworld      [0xC0]games.SyncableBitU8
	syncableBitU16 map[uint16]*games.SyncableBitU16

	romFunctions map[romFunction]uint32

	lastGameFrame      uint8  // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	localFrame         uint64 // total frame count since start of local game
	serverFrame        uint64 // total frame count according to server (taken from first player to enter group)
	monotonicFrameTime uint8  // always increments by 1 whenever game frame increases by any amount N

	shouldUpdatePlayersList bool

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
	if rom == nil {
		panic("alttp: rom cannot be nil")
	}

	g := &Game{
		rom:                rom,
		running:            false,
		stopped:            make(chan struct{}),
		readComplete:       make(chan []snes.Response, 256),
		romFunctions:       make(map[romFunction]uint32),
		lastUpdateTarget:   0xFFFFFF,
		ntpC:               make(chan int, 16),
		lastServerSentTime: time.Now(),
		lastServerRecvTime: time.Now(),
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

	//go g.ntpQueryLoop()

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

func (g *Game) ProvideQueue(queue snes.Queue) {
	g.queue = queue

	// must reset any state waiting on connected device:
	g.updateStage = 0
	g.colorPendingUpdate = 0
	g.lastUpdateTarget = 0xFFFFFF
}
func (g *Game) ProvideClient(client *client.Client) {
	g.client = client

	// indicate we want a refresh of the NTP ClockOffset:
	g.ntpC <- 0
}
func (g *Game) ProvideViewModelContainer(container interfaces.ViewModelContainer) {
	g.viewModels = container
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
	g.clean = false

	// indicate we want a refresh of the NTP ClockOffset:
	g.ntpC <- 0

	// must reset any state waiting on connected device:
	g.updateStage = 0
	g.colorPendingUpdate = 0
	g.lastUpdateTarget = 0xFFFFFF

	// an impossible color in 15-bit BGR:
	g.colorUpdatedTo = 0xffff

	// clear out players array:
	for i := range g.players {
		g.players[i] = Player{IndexF: -1, PlayerColor: 0x12ef}
	}

	// create a temporary Player instance until we get our Index assigned from the server:
	g.local = &Player{IndexF: -1, PlayerColor: 0x12ef}
	local := g.local
	local.WRAM = make(map[uint16]*SyncableWRAM)

	if g.viewModels != nil {
		// preserve last-set info:
		serverViewModelIntf, ok := g.viewModels.GetViewModel("server")
		if ok && serverViewModelIntf != nil {
			serverViewModel := serverViewModelIntf.(*engine.ServerViewModel)
			local.NameF = serverViewModel.PlayerName
			local.Team = serverViewModel.Team
		}
	}

	// initialize WRAM to non-zero values:
	for i := range g.wram {
		g.wram[i] = 0xFF
	}

	g.initSync()
}

func (g *Game) Start() {
	if g.running {
		return
	}
	g.running = true

	g.NotifyView()

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

func (g *Game) LocalPlayer() *Player {
	return g.local
}

func (g *Game) ActivePlayers() []*Player {
	if !g.activePlayersClean {
		g.activePlayers = make([]*Player, 0, len(g.activePlayers))

		for i, p := range g.players {
			if p.Index() < 0 {
				continue
			}
			if p.TTL() <= 0 {
				continue
			}

			g.activePlayers = append(g.activePlayers, &g.players[i])
		}

		g.activePlayersClean = true
	}

	return g.activePlayers
}

func (g *Game) RemotePlayers() []*Player {
	activePlayers := g.ActivePlayers()
	remotePlayers := make([]*Player, 0, len(activePlayers))
	for _, p := range activePlayers {
		if p == g.LocalPlayer() {
			continue
		}
		remotePlayers = append(remotePlayers, p)
	}
	return remotePlayers
}

func (g *Game) LocalSyncablePlayer() games.SyncablePlayer {
	return g.local
}

func (g *Game) RemoteSyncablePlayers() []games.SyncablePlayer {
	activePlayers := g.ActivePlayers()
	remotePlayers := make([]games.SyncablePlayer, 0, len(activePlayers))
	for _, p := range activePlayers {
		if p == g.LocalPlayer() {
			continue
		}
		remotePlayers = append(remotePlayers, p)
	}
	return remotePlayers
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
