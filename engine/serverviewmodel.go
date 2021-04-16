package engine

import (
	"fmt"
	"log"
	"o2/interfaces"
)

type ServerViewModel struct {
	commands map[string]interfaces.Command

	root *ViewModel

	isDirty bool

	IsConnected bool   `json:"isConnected"`
	HostName    string `json:"hostName"`
	GroupName   string `json:"groupName"`
	Team        uint8  `json:"team"`
	PlayerName  string `json:"playerName"`
}

func (v *ServerViewModel) Update() {
	game := v.root.game
	if game != nil {
		game.Notify("team", v.Team)
		game.Notify("playerName", v.PlayerName)
	}
}

func NewServerViewModel(root *ViewModel) *ServerViewModel {
	v := &ServerViewModel{
		root:        root,
		IsConnected: false,
		HostName:    "alttp.online",
		GroupName:   "group",
	}

	v.commands = map[string]interfaces.Command{
		"connect":    &ServerConnectCommand{v},
		"disconnect": &ServerDisconnectCommand{v},
		"setField":   &setFieldCmd{v},
	}

	return v
}

func (v *ServerViewModel) IsDirty() bool {
	return v.isDirty
}

func (v *ServerViewModel) ClearDirty() {
	v.isDirty = false
}

func (v *ServerViewModel) MarkDirty() {
	v.isDirty = true
}

func (v *ServerViewModel) CommandFor(command string) (ce interfaces.Command, err error) {
	var ok bool
	ce, ok = v.commands[command]
	if !ok {
		err = fmt.Errorf("no command '%s' found", command)
	}
	return
}

// Commands

type ServerConnectCommand struct{ v *ServerViewModel }

func (ce *ServerConnectCommand) CreateArgs() interfaces.CommandArgs { return nil }
func (ce *ServerConnectCommand) Execute(args interfaces.CommandArgs) error {
	v := ce.v
	vm := v.root

	defer vm.UpdateAndNotifyView()

	if v.IsConnected {
		return nil
	}

	err := vm.client.Connect(v.HostName)
	v.IsConnected = vm.client.IsConnected()
	v.MarkDirty()
	if err != nil {
		log.Print(err)
		return nil
	}

	vm.client.SetGroup(v.GroupName)

	return nil
}

type ServerDisconnectCommand struct{ v *ServerViewModel }

func (ce *ServerDisconnectCommand) CreateArgs() interfaces.CommandArgs     { return nil }
func (ce *ServerDisconnectCommand) Execute(_ interfaces.CommandArgs) error { return ce.v.Disconnect() }

func (v *ServerViewModel) Connect() error {
	defer v.root.UpdateAndNotifyView()

	v.root.ConnectServer()
	return nil
}

func (v *ServerViewModel) Disconnect() error {
	defer v.root.UpdateAndNotifyView()

	v.root.DisconnectServer()
	return nil
}

type setFieldCmd struct{ v *ServerViewModel }
type setFieldArgs struct {
	HostName   *string `json:"hostName"`
	GroupName  *string `json:"groupName"`
	Team       *uint8  `json:"team"`
	PlayerName *string `json:"playerName"`
}

func (c *setFieldCmd) CreateArgs() interfaces.CommandArgs { return &setFieldArgs{} }

func (c *setFieldCmd) Execute(args interfaces.CommandArgs) error {
	f, ok := args.(*setFieldArgs)
	if !ok {
		return fmt.Errorf("invalid args type for command")
	}

	game := c.v.root.game

	if f.HostName != nil {
		c.v.HostName = *f.HostName
		c.v.MarkDirty()
	}
	if f.GroupName != nil {
		c.v.GroupName = *f.GroupName
		client := c.v.root.client
		if client != nil {
			client.SetGroup(c.v.GroupName)
		}
		c.v.MarkDirty()
	}
	if f.Team != nil {
		c.v.Team = *f.Team
		if game != nil {
			game.Notify("team", c.v.Team)
		}
		c.v.MarkDirty()
	}
	if f.PlayerName != nil {
		c.v.PlayerName = *f.PlayerName
		if game != nil {
			game.Notify("playerName", c.v.PlayerName)
		}
		c.v.MarkDirty()
	}

	c.v.root.UpdateAndNotifyView()

	return nil
}
