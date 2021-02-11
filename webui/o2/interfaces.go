package main

type Object map[string]interface{}

// must be json serializable
type ViewModel interface {
	ViewModelCommandHandler
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

type Dirtyable interface {
	IsDirty() bool
	ClearDirty()
	MarkDirty()
}

type CommandArgs interface{}
type CommandExecutor interface {
	CreateArgs() CommandArgs
	Execute(args CommandArgs) error
}

// ViewModel implements this
type ViewModelCommandHandler interface {
	CommandExecutor(command string) (CommandExecutor, error)
}

// Controller implements this
type ViewCommandHandler interface {
	CommandExecutor(view, command string) (CommandExecutor, error)

	NotifyViewTo(viewNotifier ViewNotifier)
}

// notifies view of a modified view model:
type ViewNotifier interface {
	NotifyView(view string, viewModel interface{})
}
