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
	// queue is replaced (or cleared on disconnect) by other goroutines; access it with getQueue/setQueue:
	queueLock sync.Mutex
	queue     snes.Queue
	// client can be nil at any time
	client games.Client
	// configurationSystem will only be nil until provided
	configurationSystem interfaces.ConfigurationSystem
	// viewModels can be nil at any time
	viewModels interfaces.ViewModelContainer
	// the players' positions last sent to the web UI's map, and when (see notifyPositions):
	positionsSent     []PlayerPositionViewModel
	positionsSentTime time.Time

	priorityReadsMu sync.Mutex
	priorityReads   [3][]snes.Read

	deserTable map[MessageType]DeserializeFunc
	// unknown message types already logged, per player index and message type:
	unknownMsgLogged map[uint32]struct{}

	badVersionLogLimit uint8

	// Notifications publishes notifications about game events intended for the player to see
	Notifications interfaces.ObservableImpl

	stateLock sync.Mutex

	local   *Player
	players [MaxPlayers]Player

	activePlayersClean    bool
	activePlayers         []*Player
	remotePlayers         []*Player
	remoteSyncablePlayers []games.SyncablePlayer

	lastServerTime     time.Time // server's clock when the last received message passed through the server
	lastServerSentTime time.Time // our local clock when we sent the echo message
	lastServerRecvTime time.Time // our local clock when we received the last message
	// the offset of the server's clock from ours, from echo round trips; see clock.go:
	clock serverClock

	running bool
	stopped chan struct{}

	readResponseLock sync.Mutex
	readResponse     []snes.Response

	readComplete chan []snes.Response

	// readErrLock guards the read failure state, which backs off reads while the SNES returns errors (e.g. while an
	// emulator has no game loaded) and collapses repeated identical errors in the log:
	readErrLock      sync.Mutex
	readBackoffUntil time.Time
	readErr          string
	readErrRepeats   int

	lastReadCompleted time.Time
	notFirstWRAMRead  bool

	nextUpdateA      bool
	updateLock       sync.Mutex
	updateStage      int
	lastUpdateTarget uint32
	// updData collects the tables of the update routine being generated; see updatedata.go:
	updData          *updateData
	lastUpdateFrame  uint8
	lastUpdateTime   time.Time
	updateGenerators []games.AsmExecConfirmer
	generated        map[uint32]struct{}
	cooldownTime     time.Time

	customAsmLock sync.Mutex
	customAsm     []byte

	// the debug page's tools (see debug.go):
	debug debugLocal
	// what was sent, for the debug page (see bandwidth.go):
	bandwidth bandwidthStats
	// the state messages sent when they change (see send.go):
	sendState sendState

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
	// owLiftables are the overworld objects that reveal secrets when lifted, read from the ROM once; see syncsecrets.go:
	owLiftables     []owLiftable
	owLiftablesRead bool
	// the map16 tiles of a bombed opening's halves, read from the ROM once; see syncmap16.go:
	bombDoorLeft, bombDoorRight uint16
	bombDoorRead                bool

	lastGameFrame      uint8 // copy of wram[$001A] in-game frame counter of vanilla ALTTP game
	monotonicFrameTime uint8 // always increments by 1 whenever game frame increases by any amount N

	shouldUpdatePlayersList bool

	linkSprites linkSpriteSync
	// local and queued remote sound effects (see sfx.go):
	sfx sfxLocal
	// the Disable BG Music setting written to the game (see music.go):
	music musicState
	// remote map16 changes staged for area loads (see map16stage.go):
	map16Stage map16Stage
	// remote manipulable objects staged for room loads (see manipstage.go):
	manipStage  stagedTable
	swordShield swordShieldGfx

	// remote players drawn by the game; see remotesprites.go:
	remoteSprites remoteSpritesLocal
	// the local player's run timing; see runtiming.go:
	runTiming runTimingLocal

	smallKeys smallKeysLocal
	hearts    heartsLocal

	// overworld area flags ($7EF280..$7EF33F) last read, to send them as soon as they change:
	owFlags      [0xC0]uint8
	owFlagsKnown [0xC0]bool
	owFlagsSend  bool

	crystalSwitch crystalSwitchLocal
	starSwitch    starSwitchLocal
	flyingTiles   flyingTilesLocal
	roomState     roomStateLocal
	bossPrize     bossPrizeLocal
	enemiesKilled enemiesKilledLocal

	map16    map16Local
	manip    manipLocal
	torches  torchesLocal
	owEvents owEventsLocal

	runMinStart        time.Time
	runMaxFinish       time.Time
	runPlayersFinished int
	runPlayersStarted  int

	// the local player's lost connections (see runrestore.go):
	absenceLock sync.Mutex
	absence     runAbsence

	// group-wide reset state machine; only accessed from the game loop goroutine:
	reset         resetState
	resetRequests chan struct{}

	// patchSignature is the signature in the ROM o2 patched, which the SNES must have in SRAM; see signature.go:
	patchSignature   patchSignature
	patchSignatureOK bool
	// romCheckStart is when o2 started waiting for the SNES to return the patch signature:
	romCheckStart time.Time
	// readCycle counts read cycles, to read the patch signature periodically:
	readCycle uint8

	// the player colors last read from SRAM (see linkcolor.go), whether they were read since o2 last wrote them, and
	// how many reads to ignore after writing them:
	linkColorSRAM      [2]uint16
	linkColorFresh     bool
	linkColorSkipReads int
	last15             uint8

	// serializable ViewModel:
	clean     bool
	IsCreated bool   `json:"isCreated"`
	GameName  string `json:"gameName"`
	// ROMVerified is set once the SNES is confirmed to be running the ROM o2 patched; ROMStatus explains why not:
	ROMVerified      bool   `json:"romVerified"`
	ROMStatus        string `json:"romStatus"`
	PlayerColor      uint16 `json:"playerColor"`
	SyncItems        bool   `json:"syncItems"`
	SyncDungeonItems bool   `json:"syncDungeonItems"`
	SyncProgress     bool   `json:"syncProgress"`
	SyncHearts       bool   `json:"syncHearts"`
	SyncSmallKeys    bool   `json:"syncSmallKeys"`
	// SyncUnderworld and SyncOverworld ("Underworld (Global)", "Overworld (Global)") share the dungeon room flags and
	// the overworld area flags in SRAM, which every player keeps for good:
	SyncUnderworld bool `json:"syncUnderworld"`
	SyncOverworld  bool `json:"syncOverworld"`
	// SyncUnderworldLocal ("Underworld (Local)") shares what players in the same room do while they are there:
	// manipulable objects (pots, blocks, pegs, big rocks) and the items under pots, crystal switches, star tiles, room
	// state (trap doors, chests, keys, key doors, switches) and flying floor tiles:
	SyncUnderworldLocal bool `json:"syncUnderworldLocal"`
	// SyncOverworldLocal ("Overworld (Local)") shares what players in the same area do while they are there: map16
	// changes (lifted bushes and rocks, bombed walls, ...) and the secrets under them, and the area's events (entrance
	// animations, the desert statues, the lumberjacks' tree):
	SyncOverworldLocal bool `json:"syncOverworldLocal"`
	// SyncEnemiesKilled ("Enemy Dead List") shares which enemies players in the same room or area killed, so
	// that they are not spawned again; see syncenemieskilled.go:
	SyncEnemiesKilled bool `json:"syncEnemiesKilled"`
	// KillDeadEnemies ("Kill Dead Enemies") kills the local enemies that the dead list of the players in the same
	// room or area says are dead, as soon as that list is updated; needs SyncEnemiesKilled:
	KillDeadEnemies bool `json:"killDeadEnemies"`
	// SyncTorches shares the lit torches of the room:
	SyncTorches bool `json:"syncTorches"`
	// SyncSfx plays the sound effects of players in the same location:
	SyncSfx bool `json:"syncSfx"`
	// DisableBgMusic stops the game from playing its background music; see music.go:
	DisableBgMusic bool `json:"disableBgMusic"`
	SyncChests     bool `json:"syncChests"`
	lastSyncChests bool
	SyncTunicColor bool `json:"syncTunicColor"`
	// SyncSprites draws remote players in the same room or area; see remotesprites.go:
	SyncSprites bool `json:"syncSprites"`
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
		resetRequests:         make(chan struct{}, 1),
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
		// local sync:
		SyncUnderworldLocal: true,
		SyncOverworldLocal:  true,
		SyncTorches:         true,
		SyncEnemiesKilled:   true,
		KillDeadEnemies:     true,
		SyncSfx:             true,
		SyncChests:          true,
		lastSyncChests:      false,
		SyncSprites:         true,
	}

	g.patchSignature, g.patchSignatureOK = readPatchSignature(rom)
	g.ROMStatus = romStatusWaiting

	g.initSerde()
	g.fillRomFunctions()
	g.initSpriteSync()
	g.initSwordShieldGfx()

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
	g.linkColorFresh = false
	g.linkColorSkipReads = 0
	g.lastUpdateTarget = 0xFFFFFF
	g.updateGenerators = make([]games.AsmExecConfirmer, 0, 20)
	g.notFirstFrame = false
	g.lastGameFrame = 0xFF

	// don't set WRAM timestamps on first read from SNES:
	g.notFirstWRAMRead = false

	// SNES state may have changed while disconnected; re-read small key counts without attributing changes:
	g.invalidateSmallKeys(false)
	g.invalidateHearts(false)
	g.owFlagsKnown = [0xC0]bool{}
	g.resetCrystalSwitch()
	g.resetStarSwitch()
	g.resetEnemiesKilled()
	g.resetFlyingTiles()
	g.resetRoomState()
	g.resetMap16()
	g.resetManip()
	g.resetTorches()
	g.resetBossPrize()
	g.resetOwEvents()
	// rewrite the remote sprites table:
	g.remoteSprites = remoteSpritesLocal{}

	// initialize WRAM:
	for i := range g.wram {
		g.wram[i] = 0x00
		g.wramLastFrame[i] = 0x00
		g.wramFresh[i] = false
	}
}

func (g *Game) getQueue() snes.Queue {
	g.queueLock.Lock()
	defer g.queueLock.Unlock()
	return g.queue
}

func (g *Game) setQueue(queue snes.Queue) {
	g.queueLock.Lock()
	defer g.queueLock.Unlock()
	g.queue = queue
}

func (g *Game) ProvideQueue(queue snes.Queue) {
	g.setQueue(queue)
	g.noteSNESQueue(queue != nil)
	// the SNES may be running a different ROM now:
	g.resetPatchSignature()

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
	if offset, ok := g.clock.offset(); ok {
		return time.Now().Add(offset)
	}
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
