package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// BackupConfig represents a single named backup configuration
type BackupConfig struct {
	Name       string   // Config name (e.g., "db", "files")
	BackupType string   // Required: backup type (e.g., "postgres")
	Schedule   string   // Required: cron expression
	Retention  int      // Optional: defaults to 7
	Storage    string   // Optional: storage pool name
	Notify     []string // Optional: per-config notification override
}

// ContainerConfig represents parsed labels from a container
type ContainerConfig struct {
	ContainerID   string
	ContainerName string
	Enabled       bool
	Notify        []string       // Shared notification providers (container-level)
	Backups       []BackupConfig // One or more backup configurations
}

// LabelPrefix is the fixed prefix for all docker-backup labels
const LabelPrefix = "docker-backup"

// Label suffixes (appended to LabelPrefix)
const (
	LabelEnable    = "enable"
	LabelType      = "type"
	LabelSchedule  = "schedule"
	LabelRetention = "retention"
	LabelStorage   = "storage"
	LabelNotify    = "notify"
)

// reservedProperties are property names that cannot be used as config names
var reservedProperties = map[string]bool{
	LabelEnable:    true,
	LabelType:      true,
	LabelSchedule:  true,
	LabelRetention: true,
	LabelStorage:   true,
	LabelNotify:    true,
}

// ParseLabels extracts ContainerConfig from Docker container labels
func ParseLabels(containerID, containerName string, labels map[string]string) (*ContainerConfig, error) {
	cfg := &ContainerConfig{
		ContainerID:   containerID,
		ContainerName: containerName,
		Backups:       []BackupConfig{},
	}

	enableKey := LabelPrefix + "." + LabelEnable
	if val, ok := labels[enableKey]; ok {
		enabled, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %s: %w", enableKey, err)
		}
		cfg.Enabled = enabled
	}

	if !cfg.Enabled {
		return cfg, nil
	}

	cfg.Notify = parseNotifyValue(labels[LabelPrefix+"."+LabelNotify])

	backups, err := parseNamedConfigs(LabelPrefix, containerName, labels)
	if err != nil {
		return nil, err
	}
	cfg.Backups = backups

	if len(cfg.Backups) == 0 {
		return nil, fmt.Errorf("container %s has backup enabled but no backup configurations found (use docker-backup.<name>.type=... format)", containerName)
	}

	return cfg, nil
}

// parseNamedConfigs parses named backup configurations from labels
func parseNamedConfigs(prefix, containerName string, labels map[string]string) ([]BackupConfig, error) {
	// Group labels by config name
	configGroups := make(map[string]map[string]string)

	prefixDot := prefix + "."
	for key, value := range labels {
		if !strings.HasPrefix(key, prefixDot) {
			continue
		}

		// Remove prefix to get remainder (e.g., "db.type" or "enable")
		remainder := strings.TrimPrefix(key, prefixDot)
		parts := strings.SplitN(remainder, ".", 2)

		if len(parts) != 2 {
			// Single-part labels like "enable" are container-level, skip
			continue
		}

		configName := parts[0]
		property := parts[1]

		// Skip reserved properties used at container level
		if reservedProperties[configName] {
			continue
		}

		// Initialize config group if needed
		if configGroups[configName] == nil {
			configGroups[configName] = make(map[string]string)
		}
		configGroups[configName][property] = value
	}

	// Parse each config group
	var backups []BackupConfig
	for name, props := range configGroups {
		backup, err := parseConfigGroup(name, containerName, props)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}

	// Sort by name for deterministic ordering
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name < backups[j].Name
	})

	return backups, nil
}

// parseConfigGroup parses a single named config from its properties
func parseConfigGroup(name, containerName string, props map[string]string) (BackupConfig, error) {
	backup := BackupConfig{
		Name:      name,
		Retention: 7, // Default retention
	}

	// Parse backup type (required)
	if val, ok := props[LabelType]; ok {
		backup.BackupType = strings.TrimSpace(val)
	}
	if backup.BackupType == "" {
		return backup, fmt.Errorf("container %s config %q has no backup type specified", containerName, name)
	}

	// Parse schedule (required)
	if val, ok := props[LabelSchedule]; ok {
		backup.Schedule = strings.TrimSpace(val)
	}
	if backup.Schedule == "" {
		return backup, fmt.Errorf("container %s config %q has no schedule specified", containerName, name)
	}

	// Parse retention (optional)
	if val, ok := props[LabelRetention]; ok {
		retention, err := strconv.Atoi(val)
		if err != nil {
			return backup, fmt.Errorf("container %s config %q has invalid retention: %w", containerName, name, err)
		}
		if retention < 1 {
			return backup, fmt.Errorf("container %s config %q retention must be at least 1, got %d", containerName, name, retention)
		}
		backup.Retention = retention
	}

	// Parse storage pool (optional)
	if val, ok := props[LabelStorage]; ok {
		backup.Storage = strings.TrimSpace(val)
	}

	// Parse per-config notify override (optional)
	if val, ok := props[LabelNotify]; ok {
		backup.Notify = parseNotifyValue(val)
	}

	return backup, nil
}

// parseNotifyValue parses a comma-separated notification provider list
func parseNotifyValue(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}

	var providers []string
	for _, p := range strings.Split(val, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			providers = append(providers, p)
		}
	}
	return providers
}
