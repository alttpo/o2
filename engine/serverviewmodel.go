package engine

import "fmt"

type ServerViewModel struct {
	commands map[string]Command

	c *ViewModel

	isDirty bool

	IsConnected bool   `json:"isConnected"`
	HostName    string `json:"hostName"`
	GroupName   string `json:"groupName"`
	PlayerName  string `json:"playerName"`
	TeamNumber  uint16 `json:"teamNumber"`
}

func NewServerViewModel(c *ViewModel) *ServerViewModel {
	v := &ServerViewModel{
		c: c,
		IsConnected: false,
		HostName: "alttp.online",
		GroupName: "group",
		PlayerName: "player",
		TeamNumber: 0,
	}

	v.commands = map[string]Command{
		"connect":    &ServerConnectCommand{v},
		"disconnect": &ServerDisconnectCommand{v},
		"update":     &ServerUpdateCommand{v}, // update host, group, player, team
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

func (v *ServerViewModel) CommandFor(command string) (ce Command, err error) {
	var ok bool
	ce, ok = v.commands[command]
	if !ok {
		err = fmt.Errorf("no command '%s' found", command)
	}
	return
}

// Commands

type ServerConnectCommand struct{ v *ServerViewModel }

func (ce *ServerConnectCommand) CreateArgs() CommandArgs     { return nil }
func (ce *ServerConnectCommand) Execute(_ CommandArgs) error { return ce.v.Connect() }

type ServerDisconnectCommand struct{ v *ServerViewModel }

func (ce *ServerDisconnectCommand) CreateArgs() CommandArgs     { return nil }
func (ce *ServerDisconnectCommand) Execute(_ CommandArgs) error { return ce.v.Disconnect() }

type ServerUpdateCommand struct{ v *ServerViewModel }
type ServerUpdateCommandArgs struct {
	HostName   string `json:"hostName"`
	GroupName  string `json:"groupName"`
	PlayerName string `json:"playerName"`
	TeamNumber uint16 `json:"teamNumber"`
}

func (ce *ServerUpdateCommand) CreateArgs() CommandArgs { return &ServerUpdateCommandArgs{} }
func (ce *ServerUpdateCommand) Execute(args CommandArgs) error {
	return ce.v.UpdateData(args.(*ServerUpdateCommandArgs))
}

func (v *ServerViewModel) Connect() error {
	defer v.c.UpdateAndNotifyView()

	v.IsConnected = true
	v.MarkDirty()
	return nil
}

func (v *ServerViewModel) Disconnect() error {
	defer v.c.UpdateAndNotifyView()

	v.IsConnected = false
	v.MarkDirty()
	return nil
}

func (v *ServerViewModel) UpdateData(args *ServerUpdateCommandArgs) error {
	defer v.c.UpdateAndNotifyView()

	v.HostName = args.HostName
	v.GroupName = args.GroupName
	v.PlayerName = args.PlayerName
	v.TeamNumber = args.TeamNumber
	v.MarkDirty()
	return nil
}
