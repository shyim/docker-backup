package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLabels_Disabled(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable": "false",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled)
	assert.Empty(t, cfg.Backups)
}

func TestParseLabels_NoEnableLabel(t *testing.T) {
	labels := map[string]string{
		"docker-backup.db.type":     "postgres",
		"docker-backup.db.schedule": "0 3 * * *",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled, "should be false when label not present")
}

func TestParseLabels_InvalidEnableValue(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable": "maybe",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
}

func TestParseLabels_SingleNamedConfig(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.db.type":     "postgres",
		"docker-backup.db.schedule": "0 3 * * *",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)

	assert.True(t, cfg.Enabled)
	require.Len(t, cfg.Backups, 1)

	backup := cfg.Backups[0]
	assert.Equal(t, "db", backup.Name)
	assert.Equal(t, "postgres", backup.BackupType)
	assert.Equal(t, "0 3 * * *", backup.Schedule)
	assert.Equal(t, 7, backup.Retention, "default retention should be 7")
}

func TestParseLabels_MultipleNamedConfigs(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":           "true",
		"docker-backup.hourly.type":      "postgres",
		"docker-backup.hourly.schedule":  "0 * * * *",
		"docker-backup.hourly.retention": "24",
		"docker-backup.daily.type":       "postgres",
		"docker-backup.daily.schedule":   "0 3 * * *",
		"docker-backup.daily.retention":  "30",
		"docker-backup.daily.storage":    "s3",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)
	require.Len(t, cfg.Backups, 2)

	// Backups should be sorted by name
	assert.Equal(t, "daily", cfg.Backups[0].Name)
	assert.Equal(t, "hourly", cfg.Backups[1].Name)

	// Check daily config
	daily := cfg.Backups[0]
	assert.Equal(t, 30, daily.Retention)
	assert.Equal(t, "s3", daily.Storage)

	// Check hourly config
	hourly := cfg.Backups[1]
	assert.Equal(t, 24, hourly.Retention)
}

func TestParseLabels_WithContainerLevelNotify(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.notify":      "telegram,discord",
		"docker-backup.db.type":     "postgres",
		"docker-backup.db.schedule": "0 3 * * *",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)
	assert.Equal(t, []string{"telegram", "discord"}, cfg.Notify)
}

func TestParseLabels_WithPerConfigNotify(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.notify":      "telegram",
		"docker-backup.db.type":     "postgres",
		"docker-backup.db.schedule": "0 3 * * *",
		"docker-backup.db.notify":   "discord",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)

	// Container-level notify
	assert.Equal(t, []string{"telegram"}, cfg.Notify)

	// Per-config notify override
	assert.Equal(t, []string{"discord"}, cfg.Backups[0].Notify)
}

func TestParseLabels_MissingType(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.db.schedule": "0 3 * * *",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no backup type")
}

func TestParseLabels_MissingSchedule(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":  "true",
		"docker-backup.db.type": "postgres",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no schedule")
}

func TestParseLabels_InvalidRetention(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":       "true",
		"docker-backup.db.type":      "postgres",
		"docker-backup.db.schedule":  "0 3 * * *",
		"docker-backup.db.retention": "invalid",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
}

func TestParseLabels_ZeroRetention(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":       "true",
		"docker-backup.db.type":      "postgres",
		"docker-backup.db.schedule":  "0 3 * * *",
		"docker-backup.db.retention": "0",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least 1")
}

func TestParseLabels_NegativeRetention(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":       "true",
		"docker-backup.db.type":      "postgres",
		"docker-backup.db.schedule":  "0 3 * * *",
		"docker-backup.db.retention": "-5",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
}

func TestParseLabels_EnabledButNoConfigs(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable": "true",
	}

	_, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no backup configurations")
}

func TestParseLabels_CustomPrefix(t *testing.T) {
	labels := map[string]string{
		"my-backup.enable":      "true",
		"my-backup.db.type":     "postgres",
		"my-backup.db.schedule": "0 3 * * *",
	}

	cfg, err := ParseLabels("my-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
	assert.Len(t, cfg.Backups, 1)
}

func TestParseLabels_IgnoresUnrelatedLabels(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.db.type":     "postgres",
		"docker-backup.db.schedule": "0 3 * * *",
		"other.label":               "value",
		"com.docker.compose":        "project",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)
	assert.Len(t, cfg.Backups, 1)
}

func TestParseLabels_WhitespaceHandling(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.db.type":     "  postgres  ",
		"docker-backup.db.schedule": "  0 3 * * *  ",
		"docker-backup.db.storage":  "  s3  ",
		"docker-backup.notify":      "  telegram , discord  ",
	}

	cfg, err := ParseLabels("docker-backup", "abc123", "mycontainer", labels)
	require.NoError(t, err)

	backup := cfg.Backups[0]
	assert.Equal(t, "postgres", backup.BackupType)
	assert.Equal(t, "0 3 * * *", backup.Schedule)
	assert.Equal(t, "s3", backup.Storage)
	assert.Equal(t, []string{"telegram", "discord"}, cfg.Notify)
}

func TestParseNotifyValue(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"  ", nil},
		{"telegram", []string{"telegram"}},
		{"telegram,discord", []string{"telegram", "discord"}},
		{"  telegram , discord  ", []string{"telegram", "discord"}},
		{"telegram,,discord", []string{"telegram", "discord"}},
		{"telegram,", []string{"telegram"}},
		{",telegram", []string{"telegram"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseNotifyValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseLabels_ContainerInfo(t *testing.T) {
	labels := map[string]string{
		"docker-backup.enable":      "true",
		"docker-backup.db.type":     "postgres",
		"docker-backup.db.schedule": "0 3 * * *",
	}

	cfg, err := ParseLabels("docker-backup", "abc123def456", "my-postgres-container", labels)
	require.NoError(t, err)

	assert.Equal(t, "abc123def456", cfg.ContainerID)
	assert.Equal(t, "my-postgres-container", cfg.ContainerName)
}
