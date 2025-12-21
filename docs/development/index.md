---
icon: lucide/code
---

# Development

docker-backup uses a plugin architecture that makes it easy to extend with new backup types, storage backends, and notification providers.

## Architecture Overview

```
docker-backup/
├── cmd/docker-backup/     # CLI entry point
├── internal/
│   ├── backup/            # Backup orchestration and type registry
│   ├── backuptypes/       # Backup type implementations
│   │   └── postgres/      # PostgreSQL backup
│   ├── storage/           # Storage interface and registry
│   ├── storages/          # Storage implementations
│   │   ├── local/         # Local filesystem
│   │   └── s3/            # S3/MinIO
│   ├── notification/      # Notification interface and registry
│   ├── notifiers/         # Notification implementations
│   │   ├── telegram/      # Telegram
│   │   └── discord/       # Discord
│   ├── scheduler/         # Cron scheduler
│   ├── retention/         # Retention policy enforcement
│   ├── docker/            # Docker client wrapper
│   ├── config/            # Configuration and label parsing
│   ├── api/               # Unix socket API
│   └── dashboard/         # Web dashboard
```

## Plugin Pattern

All plugins use a self-registration pattern:

```go
func init() {
    registry.Register(&MyPlugin{})
}
```

This allows plugins to be added by simply importing their package.

## Development Setup

### Requirements

- Go 1.22+
- Node.js 20+ (for CSS build)
- Docker (for testing)
- templ CLI (`go install github.com/a-h/templ/cmd/templ@latest`)

### Build

```bash
# Clone repository
git clone https://github.com/shyim/docker-backup.git
cd docker-backup

# Install dependencies
go mod download
npm install

# Build everything
make build
```

### Development Workflow

```bash
# Watch CSS changes
npm run watch:css

# Regenerate templates
templ generate

# Run tests
go test ./...

# Build binary
go build -o docker-backup ./cmd/docker-backup
```

## Extension Guides

<div class="grid cards" markdown>

-   :lucide-database: **Backup Types**

    ---

    Add support for new database or application types

    [:octicons-arrow-right-24: Adding Backup Types](adding-backup-types.md)

-   :lucide-hard-drive: **Storage Backends**

    ---

    Add support for new storage destinations

    [:octicons-arrow-right-24: Adding Storage](adding-storage.md)

-   :lucide-bell: **Notification Providers**

    ---

    Add support for new notification services

    [:octicons-arrow-right-24: Adding Notifiers](adding-notifiers.md)

</div>
