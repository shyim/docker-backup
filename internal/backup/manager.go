package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/shyim/docker-backup/internal/config"
	"github.com/shyim/docker-backup/internal/docker"
	"github.com/shyim/docker-backup/internal/notification"
	"github.com/shyim/docker-backup/internal/retention"
	"github.com/shyim/docker-backup/internal/scheduler"
	"github.com/shyim/docker-backup/internal/storage"
)

// Manager orchestrates the backup process
type Manager struct {
	dockerClient *docker.Client
	poolManager  *storage.PoolManager
	scheduler    *scheduler.Scheduler
	retention    *retention.Manager
	notifyMgr    *notification.Manager
	config       *config.Config
	watcher      *docker.Watcher

	// Track active containers
	containers map[string]*config.ContainerConfig
	mu         sync.RWMutex
}

// NewManager creates a new backup manager
func NewManager(
	dockerClient *docker.Client,
	poolManager *storage.PoolManager,
	sched *scheduler.Scheduler,
	retention *retention.Manager,
	notifyMgr *notification.Manager,
	cfg *config.Config,
) *Manager {
	m := &Manager{
		dockerClient: dockerClient,
		poolManager:  poolManager,
		scheduler:    sched,
		retention:    retention,
		notifyMgr:    notifyMgr,
		config:       cfg,
		containers:   make(map[string]*config.ContainerConfig),
	}

	m.watcher = docker.NewWatcher(dockerClient, m.handleEvent, cfg.PollInterval)

	return m
}

// Start begins watching for containers and managing backups
func (m *Manager) Start(ctx context.Context) error {
	// Initial sync
	if err := m.syncContainers(ctx); err != nil {
		return fmt.Errorf("initial container sync failed: %w", err)
	}

	// Start watching for events
	m.watcher.Start(ctx)

	return nil
}

func (m *Manager) handleEvent(ctx context.Context, event events.Message) {
	switch event.Action {
	case "start":
		containerID := event.Actor.ID
		slog.Debug("container started", "container_id", containerID)
		m.addContainer(ctx, containerID)

	case "stop", "die":
		containerID := event.Actor.ID
		slog.Debug("container stopped", "container_id", containerID)
		m.removeContainer(containerID)

	case "sync":
		if err := m.syncContainers(ctx); err != nil {
			slog.Error("container sync failed", "error", err)
		}
	}
}

// syncContainers scans for containers and updates scheduled jobs
func (m *Manager) syncContainers(ctx context.Context) error {
	containers, err := m.dockerClient.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Track which containers we've seen
	seen := make(map[string]bool)

	for _, container := range containers {
		seen[container.ID] = true

		cfg, err := config.ParseLabels(container.ID, container.Name, container.Labels)
		if err != nil {
			slog.Warn("failed to parse container labels",
				"container", container.Name,
				"error", err,
			)
			continue
		}

		if !cfg.Enabled {
			continue
		}

		m.mu.RLock()
		existingCfg, exists := m.containers[container.ID]
		m.mu.RUnlock()

		if exists {
			if configsEqual(existingCfg.Backups, cfg.Backups) {
				continue
			}
		}

		m.scheduleContainer(ctx, container.ID, cfg)
	}

	// Remove containers that no longer exist
	m.mu.Lock()
	for containerID := range m.containers {
		if !seen[containerID] {
			cfg := m.containers[containerID]
			// Remove all jobs for this container
			for _, backup := range cfg.Backups {
				jobKey := m.makeJobKey(containerID, backup.Name)
				m.scheduler.RemoveJob(jobKey)
			}
			delete(m.containers, containerID)
			slog.Info("removed backup schedule for stopped container", "container_id", containerID)
		}
	}
	m.mu.Unlock()

	// Count total backup configs
	m.mu.RLock()
	totalConfigs := 0
	for _, cfg := range m.containers {
		totalConfigs += len(cfg.Backups)
	}
	m.mu.RUnlock()

	slog.Info("container sync complete",
		"total_containers", len(containers),
		"backup_enabled", len(m.containers),
		"backup_configs", totalConfigs,
	)

	return nil
}

// configsEqual compares two slices of BackupConfig for equality
func configsEqual(a, b []config.BackupConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].BackupType != b[i].BackupType ||
			a[i].Schedule != b[i].Schedule ||
			a[i].Retention != b[i].Retention ||
			a[i].Storage != b[i].Storage {
			return false
		}
	}
	return true
}

// addContainer adds a single container to the backup schedule
func (m *Manager) addContainer(ctx context.Context, containerID string) {
	container, err := m.dockerClient.GetContainer(ctx, containerID)
	if err != nil {
		slog.Warn("failed to get container info", "container_id", containerID, "error", err)
		return
	}

	cfg, err := config.ParseLabels(container.ID, container.Name, container.Labels)
	if err != nil {
		slog.Debug("container not configured for backup", "container", container.Name, "error", err)
		return
	}

	if !cfg.Enabled {
		return
	}

	m.scheduleContainer(ctx, containerID, cfg)
}

// removeContainer removes a container from the backup schedule
func (m *Manager) removeContainer(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg, exists := m.containers[containerID]; exists {
		// Remove all jobs for this container
		for _, backup := range cfg.Backups {
			jobKey := m.makeJobKey(containerID, backup.Name)
			m.scheduler.RemoveJob(jobKey)
		}
		delete(m.containers, containerID)
		slog.Info("removed backup schedule", "container_id", containerID)
	}
}

// makeJobKey creates a composite key for scheduler jobs
// Format: containerID:configName
func (m *Manager) makeJobKey(containerID, configName string) string {
	return containerID + ":" + configName
}

// scheduleContainer schedules backups for a container
func (m *Manager) scheduleContainer(ctx context.Context, containerID string, cfg *config.ContainerConfig) {
	// Remove any existing jobs for this container first
	m.mu.Lock()
	if existingCfg, exists := m.containers[containerID]; exists {
		for _, backup := range existingCfg.Backups {
			jobKey := m.makeJobKey(containerID, backup.Name)
			m.scheduler.RemoveJob(jobKey)
		}
	}
	// Store config
	m.containers[containerID] = cfg
	m.mu.Unlock()

	// Schedule each backup config
	for _, backup := range cfg.Backups {
		m.scheduleBackupConfig(ctx, containerID, cfg, backup)
	}
}

// scheduleBackupConfig schedules a single backup configuration
func (m *Manager) scheduleBackupConfig(ctx context.Context, containerID string, cfg *config.ContainerConfig, backup config.BackupConfig) {
	// Validate backup type exists
	backupType, ok := Get(backup.BackupType)
	if !ok {
		slog.Error("unknown backup type",
			"container", cfg.ContainerName,
			"config", backup.Name,
			"type", backup.BackupType,
			"available", List(),
		)
		return
	}

	// Verify storage pool exists
	storagePool := backup.Storage
	_, err := m.poolManager.GetForContainer(storagePool)
	if err != nil {
		slog.Error("storage pool not found",
			"container", cfg.ContainerName,
			"config", backup.Name,
			"storage", storagePool,
			"error", err,
		)
		return
	}

	// Create composite job key
	jobKey := m.makeJobKey(containerID, backup.Name)

	// Capture backup config by value for closure
	backupCfg := backup

	// Create backup job
	job := func(jobCtx context.Context) {
		m.runBackup(jobCtx, containerID, cfg, backupCfg, backupType)
	}

	// Schedule the job
	if err := m.scheduler.AddJob(jobKey, backup.Schedule, job); err != nil {
		slog.Error("failed to schedule backup",
			"container", cfg.ContainerName,
			"config", backup.Name,
			"schedule", backup.Schedule,
			"error", err,
		)
		return
	}

	slog.Info("scheduled backup",
		"container", cfg.ContainerName,
		"config", backup.Name,
		"type", backup.BackupType,
		"schedule", backup.Schedule,
		"retention", backup.Retention,
		"storage", backup.Storage,
	)
}

// getNotifyProviders returns the notification providers to use for a backup
// It prefers per-config notify, falls back to container-level notify
func (m *Manager) getNotifyProviders(cfg *config.ContainerConfig, backup config.BackupConfig) []string {
	if len(backup.Notify) > 0 {
		return backup.Notify
	}
	return cfg.Notify
}

// runBackup executes a backup for a specific container and backup config
func (m *Manager) runBackup(ctx context.Context, containerID string, cfg *config.ContainerConfig, backup config.BackupConfig, backupType BackupType) {
	startTime := time.Now()
	notifyProviders := m.getNotifyProviders(cfg, backup)

	slog.Info("starting backup",
		"container", cfg.ContainerName,
		"config", backup.Name,
		"type", backup.BackupType,
	)

	// Get fresh container info
	container, err := m.dockerClient.GetContainer(ctx, containerID)
	if err != nil {
		slog.Error("failed to get container info for backup",
			"container", cfg.ContainerName,
			"error", err,
		)
		m.notify(ctx, notification.Event{
			Type:          notification.EventBackupFailed,
			ContainerName: cfg.ContainerName,
			BackupType:    backup.BackupType,
			Error:         err,
			Timestamp:     time.Now(),
		}, notifyProviders)
		return
	}

	if !container.Running {
		slog.Warn("container not running, skipping backup",
			"container", cfg.ContainerName,
		)
		return
	}

	// Validate container has required config
	if err := backupType.Validate(container); err != nil {
		slog.Error("container validation failed",
			"container", cfg.ContainerName,
			"error", err,
		)
		m.notify(ctx, notification.Event{
			Type:          notification.EventBackupFailed,
			ContainerName: cfg.ContainerName,
			BackupType:    backup.BackupType,
			Error:         err,
			Timestamp:     time.Now(),
		}, notifyProviders)
		return
	}

	// Get storage
	store, err := m.poolManager.GetForContainer(backup.Storage)
	if err != nil {
		slog.Error("failed to get storage",
			"container", cfg.ContainerName,
			"error", err,
		)
		m.notify(ctx, notification.Event{
			Type:          notification.EventBackupFailed,
			ContainerName: cfg.ContainerName,
			BackupType:    backup.BackupType,
			Error:         err,
			Timestamp:     time.Now(),
		}, notifyProviders)
		return
	}

	// Generate backup key using config name
	key := m.generateBackupKey(cfg.ContainerName, backup.Name, backupType.FileExtension(), time.Now())

	// Create buffer for backup data
	var buf bytes.Buffer

	// Perform backup
	if err := backupType.Backup(ctx, container, m.dockerClient, &buf); err != nil {
		slog.Error("backup failed",
			"container", cfg.ContainerName,
			"error", err,
		)
		m.notify(ctx, notification.Event{
			Type:          notification.EventBackupFailed,
			ContainerName: cfg.ContainerName,
			BackupType:    backup.BackupType,
			BackupKey:     key,
			Error:         err,
			Timestamp:     time.Now(),
		}, notifyProviders)
		return
	}

	// Store backup
	if err := store.Store(ctx, key, &buf); err != nil {
		slog.Error("failed to store backup",
			"container", cfg.ContainerName,
			"key", key,
			"error", err,
		)
		m.notify(ctx, notification.Event{
			Type:          notification.EventBackupFailed,
			ContainerName: cfg.ContainerName,
			BackupType:    backup.BackupType,
			BackupKey:     key,
			Error:         err,
			Timestamp:     time.Now(),
		}, notifyProviders)
		return
	}

	duration := time.Since(startTime)
	slog.Info("backup completed",
		"container", cfg.ContainerName,
		"config", backup.Name,
		"key", key,
		"size", buf.Len(),
		"duration", duration,
	)

	// Send success notification
	m.notify(ctx, notification.Event{
		Type:          notification.EventBackupCompleted,
		ContainerName: cfg.ContainerName,
		BackupType:    backup.BackupType,
		BackupKey:     key,
		Size:          int64(buf.Len()),
		Duration:      duration,
		Timestamp:     time.Now(),
	}, notifyProviders)

	// Apply retention policy
	prefix := fmt.Sprintf("%s/%s/", cfg.ContainerName, backup.Name)
	deleted, err := m.retention.Enforce(ctx, backup.Storage, prefix, backup.Retention)
	if err != nil {
		slog.Warn("retention enforcement failed",
			"container", cfg.ContainerName,
			"error", err,
		)
	} else if deleted > 0 {
		slog.Info("retention policy applied",
			"container", cfg.ContainerName,
			"config", backup.Name,
			"deleted", deleted,
		)
	}
}

// notify sends a notification event to specified providers
func (m *Manager) notify(_ context.Context, event notification.Event, providers []string) {
	if m.notifyMgr != nil && len(providers) > 0 {
		// Use a detached context with timeout to ensure notifications complete
		// even if the original context is canceled
		notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		go func() {
			defer cancel()
			m.notifyMgr.Notify(notifyCtx, event, providers)
		}()
	}
}

// generateBackupKey creates a unique key for the backup file
// Format: container-name/config-name/YYYY-MM-DD/HHMMSS<extension>
func (m *Manager) generateBackupKey(containerName, path string, extension string, t time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%s%s",
		containerName,
		path,
		t.Format("2006-01-02"),
		t.Format("150405"),
		extension,
	)
}

// findContainerConfig looks up a container config by container name
func (m *Manager) findContainerConfig(ctx context.Context, containerName string) (*config.ContainerConfig, string, error) {
	// First check tracked containers
	m.mu.RLock()
	for id, c := range m.containers {
		if c.ContainerName == containerName {
			cfg := c
			m.mu.RUnlock()
			return cfg, id, nil
		}
	}
	m.mu.RUnlock()

	// If not found in tracked containers, try to find it in Docker
	containers, err := m.dockerClient.ListContainers(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list containers: %w", err)
	}

	for _, container := range containers {
		if container.Name == containerName {
			cfg, err := config.ParseLabels(container.ID, container.Name, container.Labels)
			if err != nil {
				return nil, "", fmt.Errorf("failed to parse container labels: %w", err)
			}
			return cfg, container.ID, nil
		}
	}

	return nil, "", fmt.Errorf("container %q not found", containerName)
}

// findBackupConfig finds a specific backup config within a container config
func (m *Manager) findBackupConfig(cfg *config.ContainerConfig, configName string) (*config.BackupConfig, error) {
	for i := range cfg.Backups {
		if cfg.Backups[i].Name == configName {
			return &cfg.Backups[i], nil
		}
	}
	return nil, fmt.Errorf("backup config %q not found in container %q", configName, cfg.ContainerName)
}

// getStorageFromBackupKey extracts config name from backup key and returns storage pool
func (m *Manager) getStorageForBackupKey(cfg *config.ContainerConfig, backupKey string) (storage.Storage, error) {
	// Extract config name from key: container-name/config-name/date/time.ext
	parts := strings.Split(backupKey, "/")
	if len(parts) < 2 {
		// Fall back to first backup config's storage
		if len(cfg.Backups) > 0 {
			return m.poolManager.GetForContainer(cfg.Backups[0].Storage)
		}
		return nil, fmt.Errorf("invalid backup key format")
	}

	configPath := parts[1] // This is either config name or backup type

	// Find matching backup config
	for _, backup := range cfg.Backups {
		keyPath := backup.BackupType
		if backup.Name != "" {
			keyPath = backup.Name
		}
		if keyPath == configPath {
			return m.poolManager.GetForContainer(backup.Storage)
		}
	}

	// Fall back to first backup config's storage
	if len(cfg.Backups) > 0 {
		return m.poolManager.GetForContainer(cfg.Backups[0].Storage)
	}

	return nil, fmt.Errorf("no backup config found for key %q", backupKey)
}

// ListBackups lists all backups for a container by name.
func (m *Manager) ListBackups(ctx context.Context, containerName string) ([]storage.BackupFile, error) {
	cfg, _, err := m.findContainerConfig(ctx, containerName)
	if err != nil {
		return nil, err
	}

	// Collect backups from all storage pools used by this container
	var allBackups []storage.BackupFile
	seenPools := make(map[string]bool)

	for _, backup := range cfg.Backups {
		storagePool := backup.Storage
		if seenPools[storagePool] {
			continue
		}
		seenPools[storagePool] = true

		store, err := m.poolManager.GetForContainer(storagePool)
		if err != nil {
			slog.Warn("failed to get storage pool", "pool", storagePool, "error", err)
			continue
		}

		// List backups with prefix for this container
		prefix := fmt.Sprintf("%s/", containerName)
		backups, err := store.List(ctx, prefix)
		if err != nil {
			slog.Warn("failed to list backups", "pool", storagePool, "error", err)
			continue
		}

		allBackups = append(allBackups, backups...)
	}

	return allBackups, nil
}

// GetBackup retrieves a backup for reading/downloading.
func (m *Manager) GetBackup(ctx context.Context, containerName, backupKey string) (io.ReadCloser, error) {
	cfg, _, err := m.findContainerConfig(ctx, containerName)
	if err != nil {
		return nil, err
	}

	// Get storage for this backup key
	store, err := m.getStorageForBackupKey(cfg, backupKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage: %w", err)
	}

	return store.Get(ctx, backupKey)
}

// RestoreBackup restores a specific backup to a container.
func (m *Manager) RestoreBackup(ctx context.Context, containerName, backupKey string) error {
	cfg, containerID, err := m.findContainerConfig(ctx, containerName)
	if err != nil {
		return err
	}

	// Extract config name from key to find backup type
	parts := strings.Split(backupKey, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid backup key format")
	}
	configPath := parts[1]

	// Find matching backup config to get backup type
	var backupCfg *config.BackupConfig
	for i := range cfg.Backups {
		keyPath := cfg.Backups[i].BackupType
		if cfg.Backups[i].Name != "" {
			keyPath = cfg.Backups[i].Name
		}
		if keyPath == configPath {
			backupCfg = &cfg.Backups[i]
			break
		}
	}

	if backupCfg == nil {
		// Fall back to first backup config
		if len(cfg.Backups) > 0 {
			backupCfg = &cfg.Backups[0]
		} else {
			return fmt.Errorf("no backup configuration found")
		}
	}

	// Get backup type
	backupType, ok := Get(backupCfg.BackupType)
	if !ok {
		return fmt.Errorf("unknown backup type %q", backupCfg.BackupType)
	}

	// Get storage
	store, err := m.getStorageForBackupKey(cfg, backupKey)
	if err != nil {
		return fmt.Errorf("failed to get storage: %w", err)
	}

	// Get container info
	container, err := m.dockerClient.GetContainer(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	if !container.Running {
		return fmt.Errorf("container %q is not running", containerName)
	}

	// Validate container
	if err := backupType.Validate(container); err != nil {
		return fmt.Errorf("container validation failed: %w", err)
	}

	// Get the backup data
	reader, err := store.Get(ctx, backupKey)
	if err != nil {
		return fmt.Errorf("failed to get backup: %w", err)
	}
	defer reader.Close()

	// Restore the backup
	startTime := time.Now()
	slog.Info("starting restore", "container", containerName, "key", backupKey)

	notifyProviders := m.getNotifyProviders(cfg, *backupCfg)

	if err := backupType.Restore(ctx, container, m.dockerClient, reader); err != nil {
		m.notify(ctx, notification.Event{
			Type:          notification.EventRestoreFailed,
			ContainerName: containerName,
			BackupType:    backupCfg.BackupType,
			BackupKey:     backupKey,
			Error:         err,
			Timestamp:     time.Now(),
		}, notifyProviders)
		return fmt.Errorf("restore failed: %w", err)
	}

	duration := time.Since(startTime)
	slog.Info("restore completed", "container", containerName, "key", backupKey, "duration", duration)

	m.notify(ctx, notification.Event{
		Type:          notification.EventRestoreCompleted,
		ContainerName: containerName,
		BackupType:    backupCfg.BackupType,
		BackupKey:     backupKey,
		Duration:      duration,
		Timestamp:     time.Now(),
	}, notifyProviders)

	return nil
}

// DeleteBackup deletes a specific backup for a container.
func (m *Manager) DeleteBackup(ctx context.Context, containerName, backupKey string) error {
	cfg, _, err := m.findContainerConfig(ctx, containerName)
	if err != nil {
		return err
	}

	// Get storage for this backup key
	store, err := m.getStorageForBackupKey(cfg, backupKey)
	if err != nil {
		return fmt.Errorf("failed to get storage: %w", err)
	}

	// Delete the backup
	if err := store.Delete(ctx, backupKey); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}

	slog.Info("backup deleted", "container", containerName, "key", backupKey)
	return nil
}

// TriggerBackup triggers an immediate backup for a container by name.
// If configName is empty and there's only one backup config, it uses that.
// If configName is empty and there are multiple configs, it runs all of them.
func (m *Manager) TriggerBackup(ctx context.Context, containerName string, configName ...string) error {
	cfg, containerID, err := m.findContainerConfig(ctx, containerName)
	if err != nil {
		return err
	}

	if !cfg.Enabled {
		return fmt.Errorf("container %q does not have backup enabled", containerName)
	}

	// Determine which configs to run
	var configsToRun []config.BackupConfig

	if len(configName) > 0 && configName[0] != "" {
		// Run specific config
		backupCfg, err := m.findBackupConfig(cfg, configName[0])
		if err != nil {
			return err
		}
		configsToRun = []config.BackupConfig{*backupCfg}
	} else {
		// Run all configs
		configsToRun = cfg.Backups
	}

	// Run each backup
	for _, backup := range configsToRun {
		backupType, ok := Get(backup.BackupType)
		if !ok {
			return fmt.Errorf("unknown backup type %q", backup.BackupType)
		}

		m.runBackup(ctx, containerID, cfg, backup, backupType)
	}

	return nil
}

// BackupConfigInfo contains information about a backup configuration
type BackupConfigInfo struct {
	Name       string
	BackupType string
	Schedule   string
	Retention  int
	Storage    string
}

// ContainerInfo contains information about a container for the dashboard
type ContainerInfo struct {
	ContainerID   string
	ContainerName string
	Notify        []string
	Backups       []BackupConfigInfo
}

// GetContainers returns information about all tracked containers
func (m *Manager) GetContainers() []ContainerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ContainerInfo, 0, len(m.containers))
	for id, cfg := range m.containers {
		info := ContainerInfo{
			ContainerID:   id,
			ContainerName: cfg.ContainerName,
			Notify:        cfg.Notify,
			Backups:       make([]BackupConfigInfo, 0, len(cfg.Backups)),
		}

		for _, backup := range cfg.Backups {
			info.Backups = append(info.Backups, BackupConfigInfo{
				Name:       backup.Name,
				BackupType: backup.BackupType,
				Schedule:   backup.Schedule,
				Retention:  backup.Retention,
				Storage:    backup.Storage,
			})
		}

		result = append(result, info)
	}
	return result
}
