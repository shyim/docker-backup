# Docker Backup

A Go-based Docker backup daemon that monitors containers via labels and performs scheduled backups to configurable storage backends.

## Build & Run

```bash
# Install dependencies (first time only)
npm install

# Build everything (CSS + templ + Go)
make build

# Or build step by step:
npm run build:css           # Build production CSS
templ generate              # Generate templ templates
go build -o docker-backup ./cmd/docker-backup

# Run with local storage
./docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/backups

# Run with Docker
docker compose up -d

# Development: watch CSS changes
npm run watch:css
```

## Architecture

### Directory Structure

- `cmd/docker-backup/` - CLI entry point
  - `main.go` - Root command and global flags
  - `daemon.go` - Daemon command
  - `backup.go` - Backup subcommands (run, list, delete, restore)
  - `helpers.go` - Utility functions
- `internal/` - Core application logic (not importable)
  - `api/` - Unix socket API server for backup triggers
  - `backup/` - Backup type interface, registry, and orchestration manager
  - `backuptypes/` - Backup type implementations
    - `mysql/` - MySQL/MariaDB backup using mysqldump
    - `postgres/` - PostgreSQL backup using pg_dump
    - `volume/` - Volume backup for container mount points
  - `config/` - Configuration and label parsing
  - `docker/` - Docker client wrapper and event watcher
  - `notification/` - Notification interface, registry, and manager
  - `notifiers/` - Notification provider implementations
    - `discord/` - Discord webhook notifications
    - `telegram/` - Telegram Bot API notifications
  - `retention/` - Retention policy enforcement
  - `scheduler/` - Cron-based job scheduler
  - `storage/` - Storage interface, registry, and pool manager
  - `storages/` - Storage backend implementations
    - `local/` - Local filesystem storage
    - `s3/` - S3/MinIO storage

### Plugin System

Both storage backends and backup types use a self-registration pattern:

```go
func init() {
    storage.Register(&MyStorageType{})
}
```

### Key Interfaces

**BackupType** (`internal/backup/backup.go`):
```go
type BackupType interface {
    Name() string
    Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error
    Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error
    Validate(container *docker.ContainerInfo) error
}
```

**Storage** (`internal/storage/storage.go`):
```go
type Storage interface {
    Store(ctx context.Context, key string, reader io.Reader) error
    List(ctx context.Context, prefix string) ([]BackupFile, error)
    Delete(ctx context.Context, key string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
}
```

## Configuration

### CLI Flags

```bash
docker-backup daemon \
  --storage=<pool>.<option>=<value>   # Define storage pools
  --notify=<name>=<dsn>               # Define notification provider with DSN
  --default-storage=<pool>            # Default pool name
  --poll-interval=30s                 # Container scan interval
  --socket=/var/run/docker-backup.sock # Unix socket path
  --dashboard=:8080                   # Enable dashboard on address
  --dashboard.auth.basic=<path|creds> # Dashboard basic auth (htpasswd)
  --dashboard.auth.oidc.provider=<provider> # OIDC provider (google, github, oidc)
  --dashboard.auth.oidc.issuer-url=<url>    # OIDC issuer URL (for generic provider)
  --dashboard.auth.oidc.client-id=<id>      # OAuth client ID
  --dashboard.auth.oidc.client-secret=<secret> # OAuth client secret
  --dashboard.auth.oidc.redirect-url=<url>  # OAuth callback URL
  --dashboard.auth.oidc.allowed-users=<emails> # Allowed emails (comma-separated)
  --dashboard.auth.oidc.allowed-domains=<domains> # Allowed domains (comma-separated)
  --log-level=info                    # debug, info, warn, error
```

### Storage Pool Configuration

Storage pools can be configured via CLI flags or environment variables. CLI flags take precedence.

#### CLI Flags

```bash
# Local storage
--storage=local.type=local
--storage=local.path=/backups

# S3 storage
--storage=s3.type=s3
--storage=s3.bucket=my-bucket
--storage=s3.region=us-east-1

# MinIO
--storage=minio.type=s3
--storage=minio.bucket=backups
--storage=minio.endpoint=http://minio:9000
--storage=minio.access-key=minioadmin
--storage=minio.secret-key=minioadmin
--storage=minio.path-style=true
```

#### Environment Variables

Format: `DOCKER_BACKUP_STORAGE_<POOL>_<OPTION>=value`

Pool names are uppercase, options use underscores (converted to hyphens internally).

```bash
# S3 storage pool named "s3prod"
DOCKER_BACKUP_STORAGE_S3PROD_TYPE=s3
DOCKER_BACKUP_STORAGE_S3PROD_BUCKET=my-bucket
DOCKER_BACKUP_STORAGE_S3PROD_REGION=us-east-1
DOCKER_BACKUP_STORAGE_S3PROD_ACCESS_KEY=AKIA...
DOCKER_BACKUP_STORAGE_S3PROD_SECRET_KEY=secret...

# Local storage pool named "local"
DOCKER_BACKUP_STORAGE_LOCAL_TYPE=local
DOCKER_BACKUP_STORAGE_LOCAL_PATH=/backups

# Default storage pool
DOCKER_BACKUP_DEFAULT_STORAGE=s3prod
```

This is useful for passing credentials securely in Docker/Kubernetes without exposing them in CLI arguments.

### Notification Provider Configuration

Notification providers are configured using DSN (Data Source Name) strings via CLI flags or environment variables. CLI flags take precedence.

The notification system uses [go-notifier](https://github.com/shyim/go-notifier), which supports:
- **Telegram** - Chat-based notifications
- **Slack** - Team messaging
- **Discord** - Server notifications with rich embeds
- **Gotify** - Self-hosted push notifications
- **Microsoft Teams** - Enterprise messaging

#### CLI Flags

```bash
# Telegram notifications
--notify=telegram='telegram://BOT_TOKEN@default?channel=CHAT_ID'

# Slack notifications
--notify=slack='slack://BOT_TOKEN@default?channel=CHANNEL_ID'

# Discord notifications
--notify=discord='discord://WEBHOOK_TOKEN@default?webhook_id=WEBHOOK_ID'

# Gotify notifications
--notify=gotify='gotify://APP_TOKEN@SERVER_HOST'

# Microsoft Teams notifications
--notify=teams='microsoftteams://default?webhook_url=WEBHOOK_URL'
```

#### Environment Variables

Format: `DOCKER_BACKUP_NOTIFY_<NAME>=dsn`

```bash
# Telegram provider named "telegram"
DOCKER_BACKUP_NOTIFY_TELEGRAM='telegram://123456:ABC-DEF@default?channel=-1001234567890'

# Discord provider named "discord"
DOCKER_BACKUP_NOTIFY_DISCORD='discord://webhook_token@default?webhook_id=1234567890'

# Slack provider named "slack"
DOCKER_BACKUP_NOTIFY_SLACK='slack://xoxb-token@default?channel=C1234567890'
```

#### Notification Events

When configured, notifications are sent for the following events:
- `backup_started` - When a backup operation begins
- `backup_completed` - When a backup completes successfully (includes size and duration)
- `backup_failed` - When a backup fails (includes error message)
- `restore_started` - When a restore operation begins
- `restore_completed` - When a restore completes successfully
- `restore_failed` - When a restore fails (includes error message)

### Dashboard Authentication

The dashboard supports HTTP Basic Authentication using htpasswd-style credentials. You can provide credentials in two ways:

#### Using an htpasswd file

```bash
# Generate htpasswd file with bcrypt (recommended)
htpasswd -Bc /etc/docker-backup/htpasswd admin

# Use the file
docker-backup daemon --dashboard=:8080 --dashboard.auth.basic=/etc/docker-backup/htpasswd
```

#### Using inline credentials

```bash
# Generate bcrypt hash
htpasswd -nbB admin yourpassword
# Output: admin:$2y$05$...

# Use inline (single user)
docker-backup daemon --dashboard=:8080 --dashboard.auth.basic='admin:$2y$05$...'
```

#### Environment Variable

```bash
DOCKER_BACKUP_DASHBOARD_AUTH_BASIC=/etc/docker-backup/htpasswd
```

Supported hash formats:
- **bcrypt** (`$2y$`, `$2a$`, `$2b$`) - Recommended
- **SHA1** (`{SHA}`) - Legacy support
- **Plain text** - Not recommended, for testing only

### Container Labels

Configure backups using named configurations in the format `docker-backup.<name>.<property>`:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram,discord  # Shared across all configs

  # Named config "db" - backs up PostgreSQL database
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7
  - docker-backup.db.storage=s3

  # Named config "files" - backs up volumes
  - docker-backup.files.type=volume
  - docker-backup.files.schedule=0 4 * * *
  - docker-backup.files.retention=14
  - docker-backup.files.storage=local
```

Each named config requires a unique name (e.g., "db", "files") and supports:
- `type` - Backup type (required)
- `schedule` - Cron expression (required)
- `retention` - Number of backups to keep (default: 7)
- `storage` - Storage pool name (optional)
- `notify` - Override container-level notifications (optional)

The `notify` label at container level applies to all backup configs unless overridden per-config. Notifications are opt-in - if not specified, no notifications will be sent.

## CLI Commands

The daemon exposes a Unix socket API at `/var/run/docker-backup.sock` (configurable via `--socket`).

### Trigger Immediate Backup

```bash
# Trigger backup via CLI (communicates with running daemon via Unix socket)
docker-backup backup run my-postgres

# With custom socket path
docker-backup backup run my-postgres --socket=/path/to/docker-backup.sock
```

### List Backups

```bash
# List all backups for a container
docker-backup backup list my-postgres
```

Output:
```
KEY                                         SIZE    DATE
my-postgres/db/2024-01-15/030000.sql.gz  1.2 MB  2024-01-15 03:00:00
my-postgres/db/2024-01-14/030000.sql.gz  1.1 MB  2024-01-14 03:00:00

Total: 2 backup(s)
```

### Delete Backup

```bash
# Delete a specific backup
docker-backup backup delete my-postgres "my-postgres/db/2024-01-14/030000.sql.gz"
```

### Restore Backup

```bash
# Restore a backup to a running container
docker-backup backup restore my-postgres "my-postgres/db/2024-01-15/030000.sql.gz"
```

### Docker Usage

To run commands inside the container:
```bash
docker exec docker-backup docker-backup backup run my-postgres
docker exec docker-backup docker-backup backup list my-postgres
```

## Extending

### Adding a New Backup Type

Create `internal/backuptypes/mysql/mysql.go`:

```go
package mysql

import (
    "context"
    "io"

    "github.com/shyim/docker-backup/internal/backup"
    "github.com/shyim/docker-backup/internal/docker"
)

func init() {
    backup.Register(&MySQLBackup{})
}

type MySQLBackup struct{}

func (m *MySQLBackup) Name() string { return "mysql" }

func (m *MySQLBackup) Validate(c *docker.ContainerInfo) error {
    // Check required env vars
}

func (m *MySQLBackup) Backup(ctx context.Context, c *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
    // Execute mysqldump in container, write gzipped data to w
}

func (m *MySQLBackup) Restore(ctx context.Context, c *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
    // Execute mysql in container with piped input
}
```

Then import in `internal/backuptypes/registry.go`.

### Adding a New Storage Backend

Create `internal/storages/azure/azure.go`:

```go
package azure

import "github.com/shyim/docker-backup/internal/storage"

func init() {
    storage.Register(&AzureStorageType{})
}

type AzureStorageType struct{}

func (t *AzureStorageType) Name() string { return "azure" }

func (t *AzureStorageType) Create(poolName string, options map[string]string) (storage.Storage, error) {
    // Parse options, create client
}
```

Then import in `internal/storages/registry.go`.

### Notification System

The notification system uses [go-notifier](https://github.com/shyim/go-notifier), which provides built-in support for:
- Telegram
- Slack
- Discord
- Gotify
- Microsoft Teams

No custom notification provider implementation is needed - all providers are configured via DSN strings. The notification manager automatically formats backup events into messages and sends them to the configured providers.
