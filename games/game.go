package games

import "o2/snes"

type Game interface {
	ROM() *snes.ROM
	SNES() snes.Queue

	Title() string
	Description() string

	Load()

	IsRunning() bool

	Start()
	Stop()
}
