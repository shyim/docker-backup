package notification

import (
	"context"
	"log/slog"
	"sync"
)

// Manager manages multiple notifiers and dispatches events
type Manager struct {
	notifiers map[string]Notifier
	mu        sync.RWMutex
}

// NewManager creates a new notification manager
func NewManager() *Manager {
	return &Manager{
		notifiers: make(map[string]Notifier),
	}
}

// AddNotifier adds a notifier to the manager
func (m *Manager) AddNotifier(name string, notifier Notifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifiers[name] = notifier
}

// Notify sends an event to specified notifiers (or none if providers is empty)
func (m *Manager) Notify(ctx context.Context, event Event, providers []string) {
	if len(providers) == 0 {
		return // No notifications configured for this container
	}

	m.mu.RLock()
	notifiers := make(map[string]Notifier)
	for _, name := range providers {
		if notifier, ok := m.notifiers[name]; ok {
			notifiers[name] = notifier
		} else {
			slog.Warn("notification provider not found",
				"provider", name,
				"container", event.ContainerName,
			)
		}
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for name, notifier := range notifiers {
		wg.Add(1)
		go func(n string, notif Notifier) {
			defer wg.Done()
			if err := notif.Send(ctx, event); err != nil {
				slog.Warn("notification failed",
					"notifier", n,
					"event", event.Type,
					"container", event.ContainerName,
					"error", err,
				)
			}
		}(name, notifier)
	}
	wg.Wait()
}

// NotifierCount returns the number of registered notifiers
func (m *Manager) NotifierCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.notifiers)
}

// NotifierInfo contains information about a notifier for display
type NotifierInfo struct {
	Name string
	Type string
}

// ListNotifiers returns information about all registered notifiers
func (m *Manager) ListNotifiers() []NotifierInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]NotifierInfo, 0, len(m.notifiers))
	for name, notifier := range m.notifiers {
		result = append(result, NotifierInfo{
			Name: name,
			Type: notifier.Type(),
		})
	}
	return result
}
