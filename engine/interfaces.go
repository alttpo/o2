package engine

type Object map[string]interface{}

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
type Command interface {
	CreateArgs() CommandArgs
	Execute(args CommandArgs) error
}

// Specific ViewModel implements this
type ViewModelCommandHandler interface {
	CommandFor(command string) (Command, error)
}

// Root ViewModel implements this
type ViewCommandHandler interface {
	CommandFor(view, command string) (Command, error)

	NotifyViewTo(viewNotifier ViewNotifier)
}

// notifies view of a modified view model:
type ViewNotifier interface {
	NotifyView(view string, viewModel interface{})
}
