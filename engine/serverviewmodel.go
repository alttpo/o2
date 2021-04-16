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
type ServerConnectCommandArgs struct {
	HostName   string `json:"hostName"`
	GroupName  string `json:"groupName"`
	Team       uint8  `json:"team"`
	PlayerName string `json:"playerName"`
}

func (ce *ServerConnectCommand) CreateArgs() interfaces.CommandArgs {
	return &ServerConnectCommandArgs{}
}
func (ce *ServerConnectCommand) Execute(args interfaces.CommandArgs) error {
	v := ce.v

	if v.IsConnected {
		return nil
	}

	ca, ok := args.(*ServerConnectCommandArgs)
	if !ok {
		return fmt.Errorf("command args not of expected type")
	}
	v.HostName = ca.HostName
	v.GroupName = ca.GroupName
	v.MarkDirty()

	vm := v.root

	err := vm.client.Connect(v.HostName, v.GroupName)
	v.IsConnected = vm.client.IsConnected()
	vm.UpdateAndNotifyView()

	if err != nil {
		log.Print(err)
		return nil
	}

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

	if f.Team != nil {
		c.v.Team = *f.Team
		if game != nil {
			game.Notify("team", c.v.Team)
		}
		c.v.isDirty = true
	}
	if f.PlayerName != nil {
		c.v.PlayerName = *f.PlayerName
		if game != nil {
			game.Notify("playerName", c.v.PlayerName)
		}
		c.v.isDirty = true
	}

	c.v.root.UpdateAndNotifyView()

	return nil
}
