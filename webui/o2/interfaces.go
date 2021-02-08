package main

type ViewCommandHandler interface {
	HandleCommand(view, command string, data Object) error
}

// pushes a modified view model to the view:
type ViewModelPusher interface {
	PushViewModel(view string, viewModel Object)
}
