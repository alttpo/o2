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

type ObserverHandle int

func (o *ObservableImpl) Subscribe(observer Observer) ObserverHandle {
	o.observers = append(o.observers, observer)
	return ObserverHandle(len(o.observers) - 1)
}

func (o *ObservableImpl) Unsubscribe(observer ObserverHandle) {
	if o.observers == nil {
		return
	}
	for i := len(o.observers) - 1; i >= 0; i-- {
		if i == int(observer) {
			o.observers[i] = nil
			//o.observers = append(o.observers[0:i], o.observers[i+1:]...)
		}
	}
}

func (o *ObservableImpl) Publish(object interface{}) {
	for _, observer := range o.observers {
		if observer == nil {
			continue
		}
		observer.Notify(object)
	}
}

type ObserverImpl func(object interface{})

func (o ObserverImpl) Notify(object interface{}) {
	o(object)
}
