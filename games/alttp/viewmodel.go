package alttp

import (
	"fmt"
	"log"
	"o2/interfaces"
	"o2/util"
	"slices"
	"strings"
	"time"
)

func (g *Game) NotifyView() {
	if g.shouldUpdatePlayersList {
		g.updatePlayersList()
		g.clean = false
	}
	if g.clean {
		return
	}

	// update the public serializable ViewModel:
	g.clean = true

	// copy into the view model:
	g.PlayerColor = g.local.PlayerColor

	// notify view of changes:
	if g.viewModels != nil {
		g.viewModels.NotifyView("game", g)
	}
}

func (g *Game) ClearNotificationHistory() {
	viewModels := g.viewModels
	if viewModels == nil {
		return
	}

	viewModels.NotifyView("game/notification/history", make([]string, 0, 200))
}

// TimestampedNotification is json serializable
type TimestampedNotification struct {
	Timestamp string `json:"t"`
	Message   string `json:"m"`
}

func (g *Game) PushNotification(notification string) {
	g.Notifications.Publish(notification)

	viewModels := g.viewModels
	if viewModels == nil {
		return
	}

	// record history of Notifications:
	historyVM, ok := viewModels.GetViewModel("game/notification/history")
	if !ok {
		historyVM = make([]TimestampedNotification, 0, 200)
	}

	history, ok := historyVM.([]TimestampedNotification)
	if !ok {
		history = make([]TimestampedNotification, 0, 200)
	}

	// prepend timestamp
	b := make([]byte, 0, len("2006-01-02 15:04:05.000 - ")+len(notification))
	if g.getQueue() != nil && !g.lastServerTime.IsZero() {
		b = append(b, 'S')
		b = g.lastServerTime.AppendFormat(b, "2006-01-02 15:04:05.000")
	} else {
		b = append(b, 'C')
		b = time.Now().AppendFormat(b, "2006-01-02 15:04:05.000")
	}
	timestampedNotification := TimestampedNotification{
		Timestamp: string(b),
		Message:   notification,
	}

	// append the notification:
	history = append(history, timestampedNotification)
	viewModels.NotifyView("game/notification/history", history)
}

type PlayerViewModel struct {
	Index int    `json:"index"`
	Team  int    `json:"team"`
	Name  string `json:"name"`
	Color uint16 `json:"color"`

	Location    int    `json:"location"`
	Overworld   string `json:"overworld"`
	Underworld  string `json:"underworld"`
	DungeonName string `json:"dungeonName"`

	// the server time of day in UTC, with a Z suffix, and the same with the date for a tooltip:
	AbsStart     string `json:"gameStart"`
	AbsStartFull string `json:"gameStartFull"`
	AbsFinish    string `json:"gameFinish"`
	RelStart     string `json:"relStart"`
	RelFinish    string `json:"relFinish"`

	// run timing (see RUN_TIMING.md): lag frames since the start, the time added for running fast, and the flags:
	LagFrames   uint32 `json:"lagFrames"`
	Penalty     string `json:"penalty"`
	TimingFlags uint8  `json:"timingFlags"`
	// what the flags mean, for a tooltip:
	TimingNotes string `json:"timingNotes"`
	// "" before the player sent any run timing (e.g. an older client), "verified" or "unverified":
	Verification string `json:"verification"`
	// why the player is unverified:
	Unverified string `json:"unverified"`

	// the player's info carries another game protocol version: its messages are dropped and only its index, team
	// and name are known:
	Incompatible bool `json:"incompatible"`
}

// RunWarningViewModel warns when players don't pay the same per frame (RUN_TIMING.md, Equal cost).
type RunWarningViewModel struct {
	Warning string `json:"warning"`
}

// FormatTime formats the time of day in UTC, with a Z suffix and no date.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format("15:04:05.000Z")
}

// FormatTimeFull formats the date and time in UTC, for a tooltip.
func FormatTimeFull(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format("2006-01-02 15:04:05.000 UTC")
}

func FormatDur(start, end time.Time) string {
	if start.IsZero() {
		return ""
	}
	if end.IsZero() {
		return ""
	}

	var dur time.Duration = end.Sub(start)
	return fmt.Sprintf("%02d:%02d:%02d.%03d",
		dur/time.Hour,
		(dur/time.Minute)%60,
		(dur/time.Second)%60,
		(dur/time.Millisecond)%1000,
	)
}

type RunTimesViewModel struct {
	StartTime      string `json:"startTime"`
	StartTimeFull  string `json:"startTimeFull"`
	FinishTime     string `json:"finishTime"`
	FinishTimeFull string `json:"finishTimeFull"`
}

// runExtent finds the earliest start and the latest finish (plus its penalty) over the players who started, and how
// many of them started and finished. Players that are away (their server connection dropped, or ours did) still count:
// their start is kept until a group reset.
func (g *Game) runExtent() (minStart, maxFinish time.Time, started, finished int) {
	count := func(p *Player) {
		if p.GameStartTime.IsZero() {
			return
		}
		started++
		if minStart.IsZero() || p.GameStartTime.Before(minStart) {
			minStart = p.GameStartTime
		}
		if !p.GameFinishTime.IsZero() {
			finished++
			// the time a player saved by running fast is added to its finish:
			finish := p.GameFinishTime.Add(g.playerRunTiming(p).Penalty)
			if finish.After(maxFinish) {
				maxFinish = finish
			}
		}
	}
	inPlayers := false
	for i := range g.players {
		p := &g.players[i]
		if p == g.local {
			inPlayers = true
		}
		count(p)
	}
	if g.local != nil && !inPlayers {
		// the local player waiting for its index from the server:
		count(g.local)
	}
	return
}

// updateRunExtent keeps the run's start and finish from the players' times, and reports whether they changed.
func (g *Game) updateRunExtent() (changed bool) {
	minStart, maxFinish, started, finished := g.runExtent()
	changed = !minStart.Equal(g.runMinStart) || !maxFinish.Equal(g.runMaxFinish)
	g.runMinStart, g.runMaxFinish, g.runPlayersFinished = minStart, maxFinish, finished
	g.runPlayersStarted = started
	return
}

// runTimerText is the run timer: from the earliest start to the latest finish once every player that started has
// finished, else to now. It is empty and does not run while no player has a start time.
func (g *Game) runTimerText() string {
	g.updateRunExtent()
	if g.runPlayersStarted == 0 || g.runMinStart.IsZero() {
		return ""
	}
	endTime := g.ServerNow()
	if g.runPlayersFinished == g.runPlayersStarted {
		endTime = g.runMaxFinish
	}
	return FormatDur(g.runMinStart, endTime)
}

func (g *Game) updateRunTimer() {
	if g.viewModels == nil {
		return
	}
	g.viewModels.NotifyView("game/run/timer", g.runTimerText())
}

func (g *Game) updatePlayersList() {
	g.shouldUpdatePlayersList = false

	if g.viewModels == nil {
		return
	}

	activePlayers := g.ActivePlayers()

	// the run's start and finish, from every player that started, including those that are away:
	updateRunTimes := g.updateRunExtent()

	common, costsDiffer := g.commonRunCost(activePlayers)

	playerViewModels := make([]*PlayerViewModel, 0, len(activePlayers))
	for _, p := range activePlayers {
		// build player view model:
		name := p.Name()

		// give the player a sensible name:
		if name == "" {
			name = fmt.Sprintf("player #%02x", p.Index())
		}

		dungeonName := "N/A"
		dungeonNumber := p.Dungeon >> 1
		if dungeonNumber < uint16(len(dungeonNames)) {
			dungeonName = dungeonNames[dungeonNumber]
		}

		underworldName := "N/A"
		if name, ok := underworldNames[p.DungeonRoom]; ok {
			underworldName = name
		}

		overworldName := "N/A"
		if name, ok := overworldNames[p.OverworldArea]; ok {
			overworldName = name
		}

		rt := g.playerRunTiming(p)

		verification, unverified := "", ""
		if rt.OK {
			verification = "verified"
			if reasons := g.verifyPlayer(p, common); len(reasons) > 0 {
				verification, unverified = "unverified", strings.Join(reasons, "; ")
			}
		}

		playerViewModels = append(playerViewModels, &PlayerViewModel{
			Index: p.Index(),
			Team:  int(p.Team),
			Name:  name,
			Color: p.PlayerColor,

			Location:    int(p.Location),
			Overworld:   overworldName,
			Underworld:  underworldName,
			DungeonName: dungeonName,

			AbsStart:     FormatTime(p.GameStartTime),
			AbsStartFull: FormatTimeFull(p.GameStartTime),
			AbsFinish:    FormatTime(p.GameFinishTime),

			LagFrames:   rt.LagFrames,
			Penalty:     formatPenalty(rt.Penalty),
			TimingFlags: rt.Flags,
			TimingNotes: timingFlagNames(rt.Flags),

			Verification: verification,
			Unverified:   unverified,
		})
	}

	for i := range playerViewModels {
		p := activePlayers[i]
		playerViewModels[i].RelStart = FormatDur(g.runMinStart, p.GameStartTime)
		playerViewModels[i].RelFinish = FormatDur(g.runMinStart, p.GameFinishTime)
	}

	// players that run an incompatible version, after the others:
	playerViewModels = g.appendIncompatiblePlayers(playerViewModels, activePlayers)

	// send the players list:
	g.viewModels.NotifyView("game/players", playerViewModels)
	warning := ""
	if costsDiffer {
		warning = "players run with different patched ROMs or gates, so their frames don't cost the same"
	}
	g.viewModels.NotifyView("game/run/warning", RunWarningViewModel{Warning: warning})
	if updateRunTimes {
		g.viewModels.NotifyView("game/run/abs", RunTimesViewModel{
			StartTime:      FormatTime(g.runMinStart),
			StartTimeFull:  FormatTimeFull(g.runMinStart),
			FinishTime:     FormatTime(g.runMaxFinish),
			FinishTimeFull: FormatTimeFull(g.runMaxFinish),
		})
	}
}

// appendIncompatiblePlayers lists the players whose player-info carries another game protocol version, unless they
// are still active from before.
func (g *Game) appendIncompatiblePlayers(vms []*PlayerViewModel, activePlayers []*Player) []*PlayerViewModel {
	active := make(map[int]bool, len(activePlayers))
	for _, p := range activePlayers {
		active[p.Index()] = true
	}
	for i := range g.net.info {
		pi := &g.net.info[i]
		if !pi.known || pi.compatible || active[i] {
			continue
		}
		name := pi.name
		if name == "" {
			name = fmt.Sprintf("player #%02x", i)
		}
		vms = append(vms, &PlayerViewModel{
			Index:        i,
			Team:         int(pi.team),
			Name:         name,
			Incompatible: true,
		})
	}
	return vms
}

func (g *Game) CommandFor(command string) (interfaces.Command, error) {
	switch command {
	case "reset":
		return &resetCmd{g}, nil
	case "setField":
		return &setFieldCmd{g}, nil
	case "asm":
		return &sendCustomAsmCmd{g}, nil
	case "fixSmallKeys":
		return &fixSmallKeysCmd{g}, nil
	case "debug":
		return &debugCmd{g}, nil
	case "debugGfx":
		return &debugGfxCmd{g}, nil
	default:
		return nil, fmt.Errorf("no handler for command=%s", command)
	}
}

type resetCmd struct{ g *Game }

func (r *resetCmd) CreateArgs() interfaces.CommandArgs { return nil }

func (r *resetCmd) Execute(_ interfaces.CommandArgs) error {
	log.Println("alttp: reset game requested")
	// initiate the group-wide reset state machine on the game loop:
	r.g.RequestReset()
	return nil
}

type fixSmallKeysCmd struct{ g *Game }

func (r *fixSmallKeysCmd) CreateArgs() interfaces.CommandArgs { return nil }

func (r *fixSmallKeysCmd) Execute(_ interfaces.CommandArgs) error {
	log.Println("alttp: fix small keys")
	r.g.FixSmallKeys()

	// notify view of new values:
	r.g.NotifyView()

	r.g.PushNotification("fix small keys")
	return nil
}

type setFieldCmd struct{ g *Game }
type setFieldArgs struct {
	PlayerColor *uint16 `json:"playerColor"`
	// Checkboxes:
	SyncItems        *bool `json:"syncItems"`
	SyncDungeonItems *bool `json:"syncDungeonItems"`
	SyncProgress     *bool `json:"syncProgress"`
	SyncHearts       *bool `json:"syncHearts"`
	SyncSmallKeys    *bool `json:"syncSmallKeys"`
	SyncUnderworld   *bool `json:"syncUnderworld"`
	SyncOverworld    *bool `json:"syncOverworld"`
	SyncChests       *bool `json:"syncChests"`
	// local sync (see Game):
	SyncUnderworldLocal *bool `json:"syncUnderworldLocal"`
	SyncOverworldLocal  *bool `json:"syncOverworldLocal"`
	SyncTorches         *bool `json:"syncTorches"`
	SyncEnemiesKilled   *bool `json:"syncEnemiesKilled"`
	KillDeadEnemies     *bool `json:"killDeadEnemies"`
	SyncSfx             *bool `json:"syncSfx"`
	DisableBgMusic      *bool `json:"disableBgMusic"`
	SyncTunicColor      *bool `json:"syncTunicColor"`
	SyncSprites         *bool `json:"syncSprites"`
}

func (c *setFieldCmd) CreateArgs() interfaces.CommandArgs { return &setFieldArgs{} }

func (c *setFieldCmd) Execute(args interfaces.CommandArgs) error {
	f, ok := args.(*setFieldArgs)
	if !ok {
		return fmt.Errorf("invalid args type for command")
	}

	g := c.g

	if f.SyncItems != nil {
		g.SyncItems = *f.SyncItems
		g.clean = false
	}
	if f.SyncDungeonItems != nil {
		g.SyncDungeonItems = *f.SyncDungeonItems
		g.clean = false
	}
	if f.SyncProgress != nil {
		g.SyncProgress = *f.SyncProgress
		g.clean = false
	}
	if f.SyncHearts != nil {
		g.SyncHearts = *f.SyncHearts
		g.clean = false
	}
	if f.SyncSmallKeys != nil {
		g.SyncSmallKeys = *f.SyncSmallKeys
		g.clean = false
	}
	if f.SyncOverworld != nil {
		g.SyncOverworld = *f.SyncOverworld
		g.clean = false
	}
	if f.SyncUnderworld != nil {
		g.SyncUnderworld = *f.SyncUnderworld
		g.clean = false
	}
	if f.SyncChests != nil {
		g.SyncChests = *f.SyncChests
		g.clean = false
	}
	if f.SyncUnderworldLocal != nil {
		g.SyncUnderworldLocal = *f.SyncUnderworldLocal
		g.clean = false
	}
	if f.SyncOverworldLocal != nil {
		g.SyncOverworldLocal = *f.SyncOverworldLocal
		g.clean = false
	}
	if f.SyncTorches != nil {
		g.SyncTorches = *f.SyncTorches
		g.clean = false
	}
	if f.SyncEnemiesKilled != nil {
		g.SyncEnemiesKilled = *f.SyncEnemiesKilled
		g.clean = false
	}
	if f.KillDeadEnemies != nil {
		g.KillDeadEnemies = *f.KillDeadEnemies
		g.clean = false
	}
	if f.SyncSfx != nil {
		g.SyncSfx = *f.SyncSfx
		g.clean = false
	}
	if f.DisableBgMusic != nil {
		g.DisableBgMusic = *f.DisableBgMusic
		g.clean = false
	}
	if f.SyncTunicColor != nil {
		g.SyncTunicColor = *f.SyncTunicColor
		g.clean = false
	}
	if f.SyncSprites != nil {
		g.SyncSprites = *f.SyncSprites
		g.clean = false
	}
	if f.PlayerColor != nil {
		g.local.PlayerColor = *f.PlayerColor
		g.shouldUpdatePlayersList = true
		g.clean = false
	}

	// save configuration:
	configurationSystem := g.configurationSystem
	if configurationSystem != nil {
		configurationSystem.SaveConfiguration()
	}
	// notify view of new values:
	g.NotifyView()

	return nil
}

type sendCustomAsmCmd struct{ g *Game }
type sendCustomAsmArgs struct {
	Code util.HexBytes `json:"code"`
}

func (c *sendCustomAsmCmd) CreateArgs() interfaces.CommandArgs { return &sendCustomAsmArgs{} }

func (c *sendCustomAsmCmd) Execute(args interfaces.CommandArgs) error {
	f, ok := args.(*sendCustomAsmArgs)
	if !ok {
		return fmt.Errorf("invalid args type for command")
	}

	// prepare the custom asm for the next frame update:
	c.g.customAsmLock.Lock()
	// custom asm must only RTS early if conditions are not satisfied to execute yet.
	// code inserted after custom asm performs clean up and prevents the routine from running again.
	// input conditions are SEP #$30 and program bank = $71
	c.g.customAsm = f.Code
	c.g.customAsmLock.Unlock()

	return nil
}

// PlayerPositionViewModel is where a player is, for the web UI's map, which places the player the way alttpo's
// WorldMapWindow does.
type PlayerPositionViewModel struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Color uint16 `json:"color"`
	// Local is set for the player of this o2 client, whose location picks the map shown:
	Local bool `json:"local"`

	Module   uint8  `json:"module"`
	Location uint32 `json:"location"`
	X        uint16 `json:"x"`
	Y        uint16 `json:"y"`
	// where the player last was on the overworld, to place them on the overworld maps while in a cave or house:
	LastOverworldX uint16 `json:"lastOverworldX"`
	LastOverworldY uint16 `json:"lastOverworldY"`
}

// positionsInterval is the least time between updates of the players' positions on the web UI's map:
const positionsInterval = 50 * time.Millisecond

// notifyPositions sends the players' positions to the web UI's map when they changed, at most every
// positionsInterval.
func (g *Game) notifyPositions(now time.Time) {
	if g.viewModels == nil || g.local == nil {
		return
	}
	if now.Sub(g.positionsSentTime) < positionsInterval {
		return
	}

	players := g.ActivePlayers()
	positions := make([]PlayerPositionViewModel, 0, len(players)+1)
	add := func(p *Player) {
		name := p.Name()
		if name == "" {
			name = fmt.Sprintf("player #%02x", p.Index())
		}
		positions = append(positions, PlayerPositionViewModel{
			Index:          p.Index(),
			Name:           name,
			Color:          p.PlayerColor,
			Local:          p == g.local,
			Module:         uint8(p.Module),
			Location:       p.Location,
			X:              p.X,
			Y:              p.Y,
			LastOverworldX: p.LastOverworldX,
			LastOverworldY: p.LastOverworldY,
		})
	}
	hasLocal := false
	for _, p := range players {
		hasLocal = hasLocal || p == g.local
		add(p)
	}
	// without a server connection the local player is not one of the active players:
	if !hasLocal {
		add(g.local)
	}

	if slices.Equal(positions, g.positionsSent) {
		return
	}
	g.positionsSent = positions
	g.positionsSentTime = now
	g.viewModels.NotifyView("game/positions", positions)
}
