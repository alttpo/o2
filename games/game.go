package games

import "o2/interfaces"

type Game interface {
	// Observable interface provides changes as a ViewModel
	interfaces.Observable

	Title() string
	Description() string

	Load()

	IsRunning() bool

	Start()
	Stop()
}
