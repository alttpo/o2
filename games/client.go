package games

type Client interface {
	Group() []byte
	Password() string
	IsConnected() bool
	Write() chan<- []byte
	Read() <-chan []byte
}
