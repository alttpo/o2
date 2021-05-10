package games

type SyncablePlayer interface {
	Index() int
	Name() string
	TTL() int

	ReadableMemory(kind MemoryKind) ReadableMemory
}
