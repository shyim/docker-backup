// Package storages provides storage backend implementations.
// Import this package to register all built-in storage types.
package storages

import (
	// Import all storage backends for self-registration
	_ "github.com/shyim/docker-backup/internal/storages/local"
	_ "github.com/shyim/docker-backup/internal/storages/s3"
)
