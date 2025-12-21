package backup

import (
	"fmt"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]BackupType)
)

// Register adds a backup type to the registry.
// This is typically called from init() functions in backup type packages.
func Register(bt BackupType) {
	registryMu.Lock()
	defer registryMu.Unlock()

	name := bt.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("backup type %q already registered", name))
	}

	registry[name] = bt
}

// Get returns a registered backup type by name
func Get(name string) (BackupType, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	bt, ok := registry[name]
	return bt, ok
}

// List returns all registered backup type names
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
