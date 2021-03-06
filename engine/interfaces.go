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
type CommandExecutor interface {
	CreateArgs() CommandArgs
	Execute(args CommandArgs) error
}

type ViewModelCommandHandler interface {
	CommandExecutor(command string) (CommandExecutor, error)
}

// ViewModel implements this
type ViewCommandHandler interface {
	CommandExecutor(view, command string) (CommandExecutor, error)

	NotifyViewTo(viewNotifier ViewNotifier)
}

// notifies view of a modified view model:
type ViewNotifier interface {
	NotifyView(view string, viewModel interface{})
}
