package storage

import (
	"context"
	"io"
	"time"
)

// BackupFile represents a stored backup file
type BackupFile struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Storage defines the interface for backup storage backends
type Storage interface {
	// Store saves backup data with the given key
	Store(ctx context.Context, key string, reader io.Reader) error

	// List returns all backups matching the prefix
	List(ctx context.Context, prefix string) ([]BackupFile, error)

	// Delete removes a backup
	Delete(ctx context.Context, key string) error

	// Get retrieves a backup for reading
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// StorageType creates Storage instances from configuration.
// Each storage backend implements this interface to provide factory functionality.
type StorageType interface {
	// Name returns the type identifier ("local", "s3", etc.)
	Name() string

	// Create instantiates storage from pool configuration options
	Create(poolName string, options map[string]string) (Storage, error)
}
