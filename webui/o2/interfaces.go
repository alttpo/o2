package main

type Object map[string]interface{}

// must be json serializable
type ViewModel interface {
	CommandHandler
	Dirtyable
	Initializable
	Updateable
}

type Initializable interface {
	Init()
}

type Updateable interface {
	Update()
}

type CommandHandler interface {
	HandleCommand(name string, args Object) error
}

type Dirtyable interface {
	IsDirty() bool
	ClearDirty()
}

// Controller implements this
type ViewCommandHandler interface {
	HandleCommand(view, command string, data Object) error
	NotifyViewTo(viewNotifier ViewNotifier)
}

// notifies view of a modified view model:
type ViewNotifier interface {
	NotifyView(view string, viewModel interface{})
}
