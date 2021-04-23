package interfaces

type CommandArgs interface{}

// ViewModeler allows a view model to provide a custom json.Marshal-able instance of itself to provide to the view
type ViewModeler interface {
	ViewModel() interface{}
}

// Command is a generic RPC command that can be requested for execution by the view with JSON arguments
type Command interface {
	// CreateArgs instantiates a JSON object that can be json.Unmarshal-ed into by the view to provide
	// named arguments for the command
	CreateArgs() CommandArgs
	// Execute executes the command given the arguments provided by the view
	Execute(args CommandArgs) error
}

// ViewModelCommandHandler returns a Command for the current view - ViewModels implement this
type ViewModelCommandHandler interface {
	CommandFor(command string) (Command, error)
}

// ViewCommandHandler handles commands requested by the view - the root ViewModel implements this
type ViewCommandHandler interface {
	CommandFor(view, command string) (Command, error)

	NotifyViewTo(viewNotifier ViewNotifier)
}

// ViewNotifier notifies view of a modified view model:
type ViewNotifier interface {
	NotifyView(view string, viewModel interface{})
}

// ViewModelContainer allows callers to get the view model value by name and to provide a new value to the view
type ViewModelContainer interface {
	// ViewNotifier NotifyView sets the viewModel and notifies the subscriber:
	ViewNotifier

	// SetViewModel sets the viewModel without notifying subscribers:
	SetViewModel(view string, viewModel interface{})
	// GetViewModel gets the last-set viewModel instance:
	GetViewModel(view string) (interface{}, bool)
}

// KeyValueNotifier notifies receiver of an updated key,value pair:
type KeyValueNotifier interface {
	Notify(key string, value interface{})
}
