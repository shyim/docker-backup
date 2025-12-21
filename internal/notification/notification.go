package notification

import (
	"context"
	"time"
)

// Event represents a backup event that can be notified
type Event struct {
	Type          EventType
	ContainerName string
	BackupType    string
	BackupKey     string
	Size          int64
	Duration      time.Duration
	Error         error
	Timestamp     time.Time
}

// EventType represents the type of backup event
type EventType string

const (
	EventBackupStarted   EventType = "backup_started"
	EventBackupCompleted EventType = "backup_completed"
	EventBackupFailed    EventType = "backup_failed"
	EventRestoreStarted  EventType = "restore_started"
	EventRestoreCompleted EventType = "restore_completed"
	EventRestoreFailed   EventType = "restore_failed"
)

// Notifier defines the interface for notification providers
type Notifier interface {
	// Name returns the notifier instance name
	Name() string

	// Type returns the notifier type (e.g., "telegram", "discord")
	Type() string

	// Send sends a notification for the given event
	Send(ctx context.Context, event Event) error
}

// NotifierType creates Notifier instances from configuration
type NotifierType interface {
	// Name returns the type identifier ("telegram", "slack", etc.)
	Name() string

	// Create instantiates a notifier from configuration options
	Create(name string, options map[string]string) (Notifier, error)
}
