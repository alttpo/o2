package alttp

import (
	"fmt"
	"log"
	"o2/interfaces"
	"o2/util"
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
	if g.queue != nil && !g.lastServerTime.IsZero() {
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

	GameTimer string `json:"timer"`
}

func (g *Game) updatePlayersList() {
	g.shouldUpdatePlayersList = false

	activePlayers := g.ActivePlayers()

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

		gameTimer := p.FormatTimer(g.ServerNow())

		playerViewModels = append(playerViewModels, &PlayerViewModel{
			Index: p.Index(),
			Team:  int(p.Team),
			Name:  name,
			Color: p.PlayerColor,

			Location:    int(p.Location),
			Overworld:   overworldName,
			Underworld:  underworldName,
			DungeonName: dungeonName,

			GameTimer: gameTimer,
		})
	}

	if g.viewModels != nil {
		// send the players list:
		g.viewModels.NotifyView("game/players", playerViewModels)
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
	log.Println("alttp: reset game")
	r.g.SoftReset()

	// notify view of new values:
	r.g.NotifyView()

	r.g.PushNotification("reset game")
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
	SyncTunicColor   *bool `json:"syncTunicColor"`
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
	if f.SyncTunicColor != nil {
		g.SyncTunicColor = *f.SyncTunicColor
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
