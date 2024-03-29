package games

import (
	"o2/snes"
	"sort"
	"sync"
)

type Factory interface {
	// determines if this ROM is or should be supported by this provider
	IsROMSupported(rom *snes.ROM) bool

	// determines if the provider can play the specific ROM with a reason why not
	CanPlay(rom *snes.ROM) (ok bool, whyNot string)

	Patcher(rom *snes.ROM) Patcher

	NewGame(rom *snes.ROM) Game
}

type Patcher interface {
	Patch() error
}

var (
	factoriesMu sync.RWMutex
	factories   = make(map[string]Factory)
)

// Register makes a Factory available by the provided name.
// If Register is called twice with the same name or if factory is nil,
// it panics.
func Register(name string, factory Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if factory == nil {
		panic("factory: Register factory is nil")
	}
	if _, dup := factories[name]; dup {
		panic("factory: Register called twice for factory " + name)
	}
	factories[name] = factory
}

func unregisterAllFactories() {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	// For tests.
	factories = make(map[string]Factory)
}

// Factories returns a list of the registered factories.
func Factories() []Factory {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	list := make([]Factory, 0, len(factories))
	for _, factory := range factories {
		list = append(list, factory)
	}
	return list
}

// FactoryNames returns a sorted list of the names of the registered factories.
func FactoryNames() []string {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	list := make([]string, 0, len(factories))
	for name := range factories {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

func FactoryByName(name string) (Factory, bool) {
	d, ok := factories[name]
	return d, ok
}
