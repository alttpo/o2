package engine

type GameViewModel struct {
	root      *ViewModel
	lastModel interface{}
}

func NewGameViewModel(root *ViewModel) *GameViewModel {
	return &GameViewModel{
		root: root,
	}
}

func (vm *GameViewModel) ViewModel() interface{} {
	return vm.lastModel
}

func (vm *GameViewModel) GameCreated() {
	vm.root.game.Subscribe(vm)
}

func (vm *GameViewModel) Notify(model interface{}) {
	vm.lastModel = model
	vm.root.NotifyViewOf("game", model)
}
