package storage

import (
	"fmt"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]StorageType)
)

// Register adds a storage type to the registry.
// This is typically called from init() functions in storage backend packages.
func Register(st StorageType) {
	registryMu.Lock()
	defer registryMu.Unlock()

	name := st.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("storage type %q already registered", name))
	}

	registry[name] = st
}

// Get returns a registered storage type by name
func Get(name string) (StorageType, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	st, ok := registry[name]
	return st, ok
}

// List returns all registered storage type names
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
