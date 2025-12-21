package notification

import (
	"fmt"
	"sync"
)

var (
	notifierTypes = make(map[string]NotifierType)
	mu            sync.RWMutex
)

// Register adds a notifier type to the registry
func Register(nt NotifierType) {
	mu.Lock()
	defer mu.Unlock()
	notifierTypes[nt.Name()] = nt
}

// Get retrieves a notifier type by name
func Get(name string) (NotifierType, bool) {
	mu.RLock()
	defer mu.RUnlock()
	nt, ok := notifierTypes[name]
	return nt, ok
}

// List returns all registered notifier type names
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(notifierTypes))
	for name := range notifierTypes {
		names = append(names, name)
	}
	return names
}

// CreateNotifier creates a notifier instance from type and options
func CreateNotifier(typeName, name string, options map[string]string) (Notifier, error) {
	nt, ok := Get(typeName)
	if !ok {
		return nil, fmt.Errorf("unknown notifier type: %s (available: %v)", typeName, List())
	}
	return nt.Create(name, options)
}
