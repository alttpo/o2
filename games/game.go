package games

import (
	"o2/client"
	"o2/interfaces"
	"o2/snes"
)

type Game interface {
	interfaces.KeyValueNotifier

	ProvideQueue(queue snes.Queue)
	ProvideClient(client *client.Client)
	ProvideViewModelContainer(container interfaces.ViewModelContainer)

	Notify(key string, value interface{})

	Title() string
	Description() string

	IsRunning() bool
	Stopped() <-chan struct{}

	Start()
	Stop()
}
