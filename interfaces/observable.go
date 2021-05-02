package interfaces

type Observer interface {
	Notify(object interface{})
}

type Observable interface {
	Subscribe(observer Observer)
	Unsubscribe(observer Observer)
}

type ObserverList []Observer

type ObservableImpl struct {
	observers ObserverList
}

func (o *ObservableImpl) Subscribe(observer Observer) {
	o.observers = append(o.observers, observer)
}

func (o *ObservableImpl) Unsubscribe(observer Observer) {
	if o.observers == nil {
		return
	}
	for i := len(o.observers); i > 0; i-- {
		if o.observers[i] == observer {
			o.observers = append(o.observers[0:i], o.observers[i+1:]...)
		}
	}
}

func (o *ObservableImpl) Publish(object interface{}) {
	for _, observer := range o.observers {
		observer.Notify(object)
	}
}

type ObserverImpl func(object interface{})

func (o ObserverImpl) Notify(object interface{}) {
	o(object)
}
