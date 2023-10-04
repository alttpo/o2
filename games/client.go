package games

type Client interface {
	Group() []byte
	IsConnected() bool
	Write() chan<- []byte
	Read() <-chan []byte
}
