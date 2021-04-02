package interfaces

type Observer interface {
	Notify(object interface{})
}

type Observable interface {
	Subscribe(observer Observer)
	Unsubscribe(observer Observer)
}

type ObserverList map[Observer]Observer
