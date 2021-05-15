package engine

import (
	"fmt"
	"log"
	"net"
	"o2/interfaces"
	"o2/util/env"
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

type ServerConfiguration struct {
	HostName   string `json:"hostName"`
	GroupName  string `json:"groupName"`
	Team       uint8  `json:"team"`
	PlayerName string `json:"playerName"`
}

func (v *ServerViewModel) LoadConfiguration(config *ServerConfiguration) {
	if config == nil {
		log.Printf("serverviewmodel: loadConfiguration: no config\n")
		return
	}

	args := &setFieldArgs{
		HostName:   new(string),
		GroupName:  new(string),
		Team:       new(uint8),
		PlayerName: new(string),
	}
	*args.HostName = config.HostName
	*args.GroupName = config.GroupName
	*args.Team = config.Team
	*args.PlayerName = config.PlayerName

	cmd := setFieldCmd{v}
	err := cmd.Execute(args)
	if err != nil {
		log.Printf("serverviewmodel: loadConfiguration: setField command failed: %v\n", err)
		return
	}
}

func (v *ServerViewModel) SaveConfiguration(config *ServerConfiguration) {
	if config == nil {
		log.Printf("serverviewmodel: saveConfiguration: no config\n")
		return
	}

	config.HostName = v.HostName
	config.GroupName = v.GroupName
	config.PlayerName = v.PlayerName
	config.Team = v.Team
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
		err = fmt.Errorf("serverviewmodel: no command '%s' found", command)
	}
	return
}

// Commands

type ServerConnectCommand struct{ v *ServerViewModel }

func (ce *ServerConnectCommand) CreateArgs() interfaces.CommandArgs { return nil }
func (ce *ServerConnectCommand) Execute(_ interfaces.CommandArgs) error {
	v := ce.v
	vm := v.root

	log.Println("serverviewmodel: connect()")

	defer func() {
		vm.UpdateAndNotifyView()
		vm.SaveConfiguration()
	}()

	if v.IsConnected {
		return nil
	}

	host, port, err := net.SplitHostPort(v.HostName)
	if err != nil {
		if addrError, ok := err.(*net.AddrError); ok {
			if addrError.Err == "missing port in address" {
				host = v.HostName
				port = defaultServerPort
			}
		} else {
			return err
		}
	}

	hostPort := net.JoinHostPort(host, port)
	addr, err := net.ResolveUDPAddr("udp", hostPort)
	if err != nil {
		log.Printf("serverviewmodel: %v\n", err)
		return err
	}

	err = vm.client.Connect(addr)
	v.IsConnected = vm.client.IsConnected()
	v.MarkDirty()
	if err != nil {
		log.Printf("serverviewmodel: %v\n", err)
		return nil
	}

	log.Printf("client: set group '%s'\n", v.GroupName)
	vm.client.SetGroup(v.GroupName)

	vm.client.SetHostName(v.HostName)

	return nil
}

type ServerDisconnectCommand struct{ v *ServerViewModel }

func (ce *ServerDisconnectCommand) CreateArgs() interfaces.CommandArgs { return nil }
func (ce *ServerDisconnectCommand) Execute(_ interfaces.CommandArgs) error {
	v := ce.v
	vm := v.root

	log.Println("serverviewmodel: disconnect()")

	vm.client.Disconnect()
	vm.serverViewModel.IsConnected = vm.client.IsConnected()

	vm.serverViewModel.MarkDirty()
	vm.UpdateAndNotifyView()
	vm.SaveConfiguration()

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

	vm := c.v.root
	game := vm.game

	if f.HostName != nil {
		c.v.HostName = *f.HostName
		c.v.MarkDirty()
	}
	if f.GroupName != nil {
		c.v.GroupName = *f.GroupName
		client := vm.client
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

	vm.UpdateAndNotifyView()
	vm.SaveConfiguration()

	return nil
}

var defaultServerPort string

func init() {
	defaultServerPort = env.GetOrDefault("O2_DEFAULT_SERVER_PORT", "4590")
}
