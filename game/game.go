package game

import "o2/snes"

type Game interface {
	ROM() *snes.ROM
	SNES() snes.Conn

	Title() string
	Description() string

	Load()

	IsRunning() bool

	Start()
	Stop() <-chan struct{}
}
