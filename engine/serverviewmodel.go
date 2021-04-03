package engine

import (
	"fmt"
	"log"
	"o2/interfaces"
)

type ServerViewModel struct {
	commands map[string]interfaces.Command

	c *ViewModel

	isDirty bool

	IsConnected bool   `json:"isConnected"`
	HostName    string `json:"hostName"`
	GroupName   string `json:"groupName"`
}

func NewServerViewModel(c *ViewModel) *ServerViewModel {
	v := &ServerViewModel{
		c:           c,
		IsConnected: false,
		HostName:    "alttp.online",
		GroupName:   "group",
	}

	v.commands = map[string]interfaces.Command{
		"connect":    &ServerConnectCommand{v},
		"disconnect": &ServerDisconnectCommand{v},
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
	HostName  string `json:"hostName"`
	GroupName string `json:"groupName"`
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

	vm := v.c

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
	defer v.c.UpdateAndNotifyView()

	v.c.ConnectServer()
	return nil
}

func (v *ServerViewModel) Disconnect() error {
	defer v.c.UpdateAndNotifyView()

	v.c.DisconnectServer()
	return nil
}
