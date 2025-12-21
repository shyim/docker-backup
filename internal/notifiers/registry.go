// Package notifiers provides notification provider implementations.
// Import this package to register all built-in notifier types.
package notifiers

import (
	// Import all notifier types for self-registration
	_ "github.com/shyim/docker-backup/internal/notifiers/discord"
	_ "github.com/shyim/docker-backup/internal/notifiers/telegram"
)
