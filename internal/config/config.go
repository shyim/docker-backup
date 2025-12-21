package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// EnvPrefix is the prefix for all environment variables
	EnvPrefix = "DOCKER_BACKUP_"
	// EnvStoragePrefix is the prefix for storage pool environment variables
	EnvStoragePrefix = EnvPrefix + "STORAGE_"
	// EnvNotifyPrefix is the prefix for notification provider environment variables
	EnvNotifyPrefix = EnvPrefix + "NOTIFY_"
)

// Config holds the global application configuration
type Config struct {
	// Docker settings
	DockerHost   string
	PollInterval time.Duration

	// Storage settings
	DefaultStorage string
	StorageArgs    []string
	StoragePools   map[string]*StoragePool

	// Notification settings
	NotifyArgs    []string
	NotifyConfigs map[string]*NotifyConfig

	// Backup settings
	TempDir string

	// Dashboard settings
	DashboardAddr      string
	DashboardBasicAuth string // htpasswd-style credentials (user:hash or file path)

	// Dashboard OIDC settings
	DashboardOIDCProvider       string
	DashboardOIDCIssuerURL      string
	DashboardOIDCClientID       string
	DashboardOIDCClientSecret   string
	DashboardOIDCRedirectURL    string
	DashboardOIDCAllowedUsers   []string
	DashboardOIDCAllowedDomains []string

	// Logging
	LogLevel  string
	LogFormat string
}

// StoragePool represents a named storage pool configuration
type StoragePool struct {
	Name    string
	Type    string
	Options map[string]string
}

// NotifyConfig represents a named notification provider configuration
type NotifyConfig struct {
	Name    string
	Type    string
	Options map[string]string
}

// New creates a new Config with default values
func New() *Config {
	return &Config{
		DockerHost:    "unix:///var/run/docker.sock",
		PollInterval:  30 * time.Second,
		LogLevel:      "info",
		LogFormat:     "text",
		StoragePools:  make(map[string]*StoragePool),
		NotifyConfigs: make(map[string]*NotifyConfig),
	}
}

func (c *Config) ParseStoragePools() error {
	// First, parse environment variables
	c.parseStorageEnvVars()

	// Then parse CLI arguments (these override env vars)
	for _, arg := range c.StorageArgs {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid storage argument format: %s (expected pool.option=value)", arg)
		}

		key := parts[0]
		value := parts[1]

		// Split key into pool name and option
		keyParts := strings.SplitN(key, ".", 2)
		if len(keyParts) != 2 {
			return fmt.Errorf("invalid storage key format: %s (expected pool.option)", key)
		}

		poolName := keyParts[0]
		option := keyParts[1]

		c.setStoragePoolOption(poolName, option, value)
	}

	// Validate all pools have a type
	for name, pool := range c.StoragePools {
		if pool.Type == "" {
			return fmt.Errorf("storage pool %q is missing required 'type' option", name)
		}
	}

	// Set default storage if not specified and only one pool exists
	if c.DefaultStorage == "" && len(c.StoragePools) == 1 {
		for name := range c.StoragePools {
			c.DefaultStorage = name
		}
	}

	// Check for default storage from environment
	if c.DefaultStorage == "" {
		if envDefault := os.Getenv(EnvPrefix + "DEFAULT_STORAGE"); envDefault != "" {
			c.DefaultStorage = envDefault
		}
	}

	// Validate default storage exists
	if c.DefaultStorage != "" {
		if _, exists := c.StoragePools[c.DefaultStorage]; !exists {
			return fmt.Errorf("default storage pool %q does not exist", c.DefaultStorage)
		}
	}

	return nil
}

func (c *Config) parseStorageEnvVars() {
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, EnvStoragePrefix) {
			continue
		}

		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		// Remove prefix: DOCKER_BACKUP_STORAGE_S3PROD_BUCKET -> S3PROD_BUCKET
		remainder := strings.TrimPrefix(key, EnvStoragePrefix)

		// Split into pool name and option: S3PROD_BUCKET -> S3PROD, BUCKET
		underscoreIdx := strings.Index(remainder, "_")
		if underscoreIdx == -1 {
			continue // Invalid format
		}

		poolName := strings.ToLower(remainder[:underscoreIdx])
		option := strings.ToLower(remainder[underscoreIdx+1:])

		// Convert underscores to hyphens in option name (ACCESS_KEY -> access-key)
		option = strings.ReplaceAll(option, "_", "-")

		c.setStoragePoolOption(poolName, option, value)
	}
}

func (c *Config) setStoragePoolOption(poolName, option, value string) {
	pool, exists := c.StoragePools[poolName]
	if !exists {
		pool = &StoragePool{
			Name:    poolName,
			Options: make(map[string]string),
		}
		c.StoragePools[poolName] = pool
	}

	// Handle type specially
	if option == "type" {
		pool.Type = value
	} else {
		pool.Options[option] = value
	}
}

func (c *Config) ParseNotifyConfigs() error {
	// First, parse environment variables
	c.parseNotifyEnvVars()

	// Then parse CLI arguments (these override env vars)
	for _, arg := range c.NotifyArgs {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid notify argument format: %s (expected provider.option=value)", arg)
		}

		key := parts[0]
		value := parts[1]

		// Split key into provider name and option
		keyParts := strings.SplitN(key, ".", 2)
		if len(keyParts) != 2 {
			return fmt.Errorf("invalid notify key format: %s (expected provider.option)", key)
		}

		providerName := keyParts[0]
		option := keyParts[1]

		c.setNotifyConfigOption(providerName, option, value)
	}

	// Validate all configs have a type
	for name, cfg := range c.NotifyConfigs {
		if cfg.Type == "" {
			return fmt.Errorf("notification provider %q is missing required 'type' option", name)
		}
	}

	return nil
}

func (c *Config) parseNotifyEnvVars() {
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, EnvNotifyPrefix) {
			continue
		}

		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		// Remove prefix: DOCKER_BACKUP_NOTIFY_TELEGRAM_TOKEN -> TELEGRAM_TOKEN
		remainder := strings.TrimPrefix(key, EnvNotifyPrefix)

		// Split into provider name and option: TELEGRAM_TOKEN -> TELEGRAM, TOKEN
		underscoreIdx := strings.Index(remainder, "_")
		if underscoreIdx == -1 {
			continue // Invalid format
		}

		providerName := strings.ToLower(remainder[:underscoreIdx])
		option := strings.ToLower(remainder[underscoreIdx+1:])

		// Convert underscores to hyphens in option name (CHAT_ID -> chat-id)
		option = strings.ReplaceAll(option, "_", "-")

		c.setNotifyConfigOption(providerName, option, value)
	}
}

func (c *Config) setNotifyConfigOption(providerName, option, value string) {
	cfg, exists := c.NotifyConfigs[providerName]
	if !exists {
		cfg = &NotifyConfig{
			Name:    providerName,
			Options: make(map[string]string),
		}
		c.NotifyConfigs[providerName] = cfg
	}

	// Handle type specially
	if option == "type" {
		cfg.Type = value
	} else {
		cfg.Options[option] = value
	}
}
