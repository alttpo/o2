package alttp

import (
	"fmt"
	"o2/interfaces"
)

func (g *Game) notifyView() {
	if g.clean {
		return
	}

	// update the public serializable ViewModel:
	g.clean = true

	// notify view of changes:
	g.viewModels.NotifyView("game", g)
}

func (g *Game) pushNotification(notification string) {
	// record history of notifications:
	historyVM, ok := g.viewModels.GetViewModel("game/notification/history")
	if !ok {
		historyVM = make([]string, 0, 200)
	}

	history, ok := historyVM.([]string)
	if !ok {
		history = make([]string, 0, 200)
	}

	// append the notification:
	history = append(history, notification)
	g.viewModels.NotifyView("game/notification/history", history)
}

type PlayerViewModel struct {
	Index    int    `json:"index"`
	Team     int    `json:"team"`
	Name     string `json:"name"`
	Location int    `json:"location"`

	// TODO: more player details
}

func (g *Game) updatePlayersList() {
	activePlayers := g.ActivePlayers()

	playerViewModels := make([]*PlayerViewModel, 0, len(activePlayers))
	for _, p := range activePlayers {
		// build player view model:
		name := p.Name

		// give the player a sensible name:
		if name == "" {
			name = fmt.Sprintf("player #%02x", p.Index)
		}

		playerViewModels = append(playerViewModels, &PlayerViewModel{
			Index:    p.Index,
			Team:     int(p.Team),
			Location: int(p.Location),
			Name:     name,
		})
	}

	// send the players list:
	g.viewModels.NotifyView("game/players", playerViewModels)
}

func (g *Game) CommandFor(command string) (interfaces.Command, error) {
	switch command {
	case "setField":
		return &setFieldCmd{g}, nil
	case "asm":
		return &sendCustomAsmCmd{g}, nil
	default:
		return nil, fmt.Errorf("no handler for command=%s", command)
	}
}

type setFieldCmd struct{ g *Game }
type setFieldArgs struct {
	// Checkboxes:
	SyncItems        *bool `json:"syncItems"`
	SyncDungeonItems *bool `json:"syncDungeonItems"`
	SyncProgress     *bool `json:"syncProgress"`
	SyncHearts       *bool `json:"syncHearts"`
	SyncSmallKeys    *bool `json:"syncSmallKeys"`
	SyncUnderworld   *bool `json:"syncUnderworld"`
	SyncOverworld    *bool `json:"syncOverworld"`
	SyncChests       *bool `json:"syncChests"`
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

	g.notifyView()

	return nil
}

type sendCustomAsmCmd struct{ g *Game }
type sendCustomAsmArgs struct {
	Code interfaces.HexBytes `json:"code"`
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
