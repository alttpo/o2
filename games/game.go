package games

type Game interface {
	Title() string
	Description() string

	Load()

	IsRunning() bool

	Start()
	Stop()
}
