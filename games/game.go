package games

import (
	"o2/client"
	"o2/interfaces"
	"o2/snes"
)

type Game interface {
	ProvideQueue(queue snes.Queue)
	ProvideClient(client *client.Client)
	ProvideViewModelContainer(container interfaces.ViewModelContainer)

	Title() string
	Description() string

	IsRunning() bool
	Stopped() <-chan struct{}

	Start()
	Stop()
}
