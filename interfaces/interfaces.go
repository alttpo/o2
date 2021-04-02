package interfaces

type CommandArgs interface{}

type ViewModeler interface {
	ViewModel() interface{}
}

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
