// Package backuptypes provides backup type implementations.
// Import this package to register all built-in backup types.
package backuptypes

import (
	// Import all backup types for self-registration
	_ "github.com/shyim/docker-backup/internal/backuptypes/mysql"
	_ "github.com/shyim/docker-backup/internal/backuptypes/postgres"
	_ "github.com/shyim/docker-backup/internal/backuptypes/volume"
)
