package interfaces

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
