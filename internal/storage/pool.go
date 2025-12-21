package storage

import (
	"fmt"
	"sync"

	"github.com/shyim/docker-backup/internal/config"
)

// PoolManager manages named storage pools
type PoolManager struct {
	pools       map[string]Storage
	defaultPool string
	mu          sync.RWMutex
}

// NewPoolManager creates a pool manager from storage pool configurations
func NewPoolManager(pools map[string]*config.StoragePool, defaultPool string) (*PoolManager, error) {
	pm := &PoolManager{
		pools:       make(map[string]Storage),
		defaultPool: defaultPool,
	}

	for name, poolCfg := range pools {
		storageType, ok := Get(poolCfg.Type)
		if !ok {
			return nil, fmt.Errorf("unknown storage type %q for pool %q (available: %v)", poolCfg.Type, name, List())
		}

		storage, err := storageType.Create(name, poolCfg.Options)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage pool %q: %w", name, err)
		}

		pm.pools[name] = storage
	}

	return pm, nil
}

// Get returns a storage pool by name
func (pm *PoolManager) Get(name string) (Storage, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	storage, ok := pm.pools[name]
	if !ok {
		return nil, fmt.Errorf("storage pool %q not found", name)
	}

	return storage, nil
}

// GetDefault returns the default storage pool
func (pm *PoolManager) GetDefault() (Storage, error) {
	if pm.defaultPool == "" {
		return nil, fmt.Errorf("no default storage pool configured")
	}

	return pm.Get(pm.defaultPool)
}

func (pm *PoolManager) GetForContainer(storageName string) (Storage, error) {
	if storageName != "" {
		return pm.Get(storageName)
	}

	return pm.GetDefault()
}

// List returns all pool names
func (pm *PoolManager) List() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.pools))
	for name := range pm.pools {
		names = append(names, name)
	}
	return names
}

// PoolCount returns the number of storage pools
func (pm *PoolManager) PoolCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.pools)
}
