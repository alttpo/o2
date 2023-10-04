package games

import (
	"encoding/json"
	"o2/interfaces"
	"o2/snes"
)

type Game interface {
	SyncableGame
	interfaces.KeyValueNotifier

	// Name returns the factory name that instantiated this Game instance
	Name() string

	ProvideConfigurationSystem(configurationSystem interfaces.ConfigurationSystem)
	LoadConfiguration(config json.RawMessage)
	ConfigurationModel() interface{}

	ProvideQueue(queue snes.Queue)
	ProvideClient(client Client)
	ProvideViewModelContainer(container interfaces.ViewModelContainer)

	Notify(key string, value interface{})

	Title() string
	Description() string

	IsRunning() bool
	Stopped() <-chan struct{}

	Reset()
	Start()
	Stop()
}
