package games

type Game interface {
	Title() string
	Description() string

	IsRunning() bool

	Start()
	Stop()
}
