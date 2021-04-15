package games

type Game interface {
	Title() string
	Description() string

	IsRunning() bool
	Stopped() <-chan struct{}

	Start()
	Stop()
}
