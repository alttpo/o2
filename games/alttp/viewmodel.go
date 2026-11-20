package alttp

import (
	"fmt"
	"log"
	"o2/interfaces"
	"o2/util"
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

	AbsStart  string `json:"gameStart"`
	AbsFinish string `json:"gameFinish"`
	RelStart  string `json:"relStart"`
	RelFinish string `json:"relFinish"`

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
}

// RunWarningViewModel warns when players don't pay the same per frame (RUN_TIMING.md, Equal cost).
type RunWarningViewModel struct {
	Warning string `json:"warning"`
}

func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format("15:04:05.000")
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
		(dur/time.Microsecond)%1000,
	)
}

type RunTimesViewModel struct {
	StartTime  string `json:"startTime"`
	FinishTime string `json:"finishTime"`
}

func (g *Game) updateRunTimer() {
	if g.viewModels == nil {
		return
	}
	if g.runMinStart.IsZero() {
		return
	}

	endTime := g.ServerNow()
	activePlayers := g.ActivePlayers()
	if g.runPlayersFinished == len(activePlayers) {
		// all players finished, use maxFinish time:
		endTime = g.runMaxFinish
	}
	runTimer := FormatDur(g.runMinStart, endTime)
	g.viewModels.NotifyView("game/run/timer", runTimer)
}

func (g *Game) updatePlayersList() {
	g.shouldUpdatePlayersList = false

	if g.viewModels == nil {
		return
	}

	activePlayers := g.ActivePlayers()

	// run timer logic:
	g.runPlayersFinished = 0
	updateMinStart := false
	updateMaxFinish := false

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

		// determine minimum start time and maximum finish time among all players:
		if !p.GameStartTime.IsZero() {
			if g.runMinStart.IsZero() {
				g.runMinStart = p.GameStartTime
				updateMinStart = true
			} else if p.GameStartTime.Before(g.runMinStart) {
				g.runMinStart = p.GameStartTime
				updateMinStart = true
			}
		}
		rt := g.playerRunTiming(p)
		if !p.GameFinishTime.IsZero() {
			g.runPlayersFinished++
			// the time a player saved by running fast is added to its finish:
			finish := p.GameFinishTime.Add(rt.Penalty)
			if g.runMaxFinish.IsZero() {
				g.runMaxFinish = finish
				updateMaxFinish = true
			} else if finish.After(g.runMaxFinish) {
				g.runMaxFinish = finish
				updateMaxFinish = true
			}
		}

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

			AbsStart:  FormatTime(p.GameStartTime),
			AbsFinish: FormatTime(p.GameFinishTime),

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

	// send the players list:
	g.viewModels.NotifyView("game/players", playerViewModels)
	warning := ""
	if costsDiffer {
		warning = "players run with different patched ROMs or gates, so their frames don't cost the same"
	}
	g.viewModels.NotifyView("game/run/warning", RunWarningViewModel{Warning: warning})
	if updateMinStart || updateMaxFinish {
		g.viewModels.NotifyView("game/run/abs", RunTimesViewModel{
			StartTime:  FormatTime(g.runMinStart),
			FinishTime: FormatTime(g.runMaxFinish),
		})
	}
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
	SyncSfx             *bool `json:"syncSfx"`
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
	if f.SyncSfx != nil {
		g.SyncSfx = *f.SyncSfx
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
