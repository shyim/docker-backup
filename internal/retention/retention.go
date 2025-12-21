package retention

import (
	"context"
	"log/slog"
	"sort"

	"github.com/shyim/docker-backup/internal/storage"
)

// Manager handles retention policy enforcement
type Manager struct {
	poolManager *storage.PoolManager
}

// New creates a new retention manager
func New(poolManager *storage.PoolManager) *Manager {
	return &Manager{
		poolManager: poolManager,
	}
}

func (m *Manager) Enforce(ctx context.Context, storageName, prefix string, keepCount int) (int, error) {
	store, err := m.poolManager.GetForContainer(storageName)
	if err != nil {
		return 0, err
	}

	// List all backups for this prefix
	files, err := store.List(ctx, prefix)
	if err != nil {
		return 0, err
	}

	if len(files) <= keepCount {
		return 0, nil // Nothing to delete
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].LastModified.After(files[j].LastModified)
	})

	// Delete old backups
	deleted := 0
	for i := keepCount; i < len(files); i++ {
		file := files[i]
		if err := store.Delete(ctx, file.Key); err != nil {
			slog.Warn("failed to delete old backup",
				"key", file.Key,
				"error", err,
			)
			continue
		}
		deleted++
		slog.Info("deleted old backup",
			"key", file.Key,
			"age", file.LastModified,
		)
	}

	return deleted, nil
}
