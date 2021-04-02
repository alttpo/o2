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
	g.Team = g.local.Team
	g.PlayerName = g.local.Name
	g.clean = true

	// notify view of changes:
	g.viewNotifier.NotifyView("game", g)
}

func (g *Game) CommandFor(command string) (interfaces.Command, error) {
	switch command {
	case "setField":
		return &setFieldCmd{g}, nil
	default:
		return nil, fmt.Errorf("no handler for command=%s", command)
	}
}

type setFieldCmd struct{ g *Game }
type setFieldArgs struct {
	Team       *int    `json:"team"`
	PlayerName *string `json:"playerName"`
}

func (c *setFieldCmd) CreateArgs() interfaces.CommandArgs { return &setFieldArgs{} }

func (c *setFieldCmd) Execute(args interfaces.CommandArgs) error {
	f, ok := args.(*setFieldArgs)
	if !ok {
		return fmt.Errorf("invalid args type for command")
	}

	if f.Team != nil {
		c.g.local.Team = *f.Team
		c.g.clean = false
	}
	if f.PlayerName != nil {
		c.g.local.Name = *f.PlayerName
		c.g.clean = false
	}

	c.g.notifyView()

	return nil
}
