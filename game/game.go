package game

import "o2/snes"

type Game interface {
	ROM() *snes.ROM
	SNES() snes.Conn

	Title() string
	Description() string

	Start()
	Stop() <-chan struct{}
}
