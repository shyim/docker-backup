---
icon: lucide/settings
---

# Configuration

docker-backup is configured through a combination of:

1. **CLI flags** - Daemon startup options
2. **Environment variables** - Alternative to CLI flags, useful for secrets
3. **Container labels** - Per-container backup configuration

## Daemon Configuration

### CLI Flags

```bash
docker-backup daemon \
  --docker-host=unix:///var/run/docker.sock \
  --poll-interval=30s \
  --socket=/var/run/docker-backup.sock \
  --storage=<pool>.<option>=<value> \
  --notify=<provider>.<option>=<value> \
  --default-storage=<pool> \
  --dashboard=:8080 \
  --dashboard.auth.basic=<htpasswd> \
  --log-level=info \
  --log-format=text
```

### Flag Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--docker-host` | `unix:///var/run/docker.sock` | Docker daemon socket |
| `--poll-interval` | `30s` | How often to scan for container changes |
| `--socket` | `/var/run/docker-backup.sock` | Unix socket for CLI communication |
| `--storage` | - | Storage pool configuration (repeatable) |
| `--notify` | - | Notification provider configuration (repeatable) |
| `--default-storage` | - | Default storage pool name |
| `--temp-dir` | System temp | Temporary directory for backup files |
| `--dashboard` | - | Dashboard listen address (e.g., `:8080`) |
| `--dashboard.auth.basic` | - | htpasswd file or inline credentials |
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--log-format` | `text` | Log format: text, json |

## Environment Variables

All configuration can be set via environment variables. This is useful for passing secrets without exposing them in CLI arguments.

### Storage Configuration

Format: `DOCKER_BACKUP_STORAGE_<POOL>_<OPTION>=value`

```bash
# S3 storage pool named "s3prod"
DOCKER_BACKUP_STORAGE_S3PROD_TYPE=s3
DOCKER_BACKUP_STORAGE_S3PROD_BUCKET=my-bucket
DOCKER_BACKUP_STORAGE_S3PROD_REGION=us-east-1
DOCKER_BACKUP_STORAGE_S3PROD_ACCESS_KEY=AKIA...
DOCKER_BACKUP_STORAGE_S3PROD_SECRET_KEY=secret...

# Default storage pool
DOCKER_BACKUP_DEFAULT_STORAGE=s3prod
```

### Notification Configuration

Format: `DOCKER_BACKUP_NOTIFY_<PROVIDER>_<OPTION>=value`

```bash
# Telegram provider
DOCKER_BACKUP_NOTIFY_TELEGRAM_TYPE=telegram
DOCKER_BACKUP_NOTIFY_TELEGRAM_TOKEN=123456:ABC-DEF...
DOCKER_BACKUP_NOTIFY_TELEGRAM_CHAT_ID=-1001234567890
```

## Container Labels

Backup configuration is defined on containers using labels. See [Container Labels](container-labels.md) for the complete reference.

### Quick Example

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7
  - docker-backup.db.storage=s3
  - docker-backup.notify=telegram
```

## Configuration Sections

<div class="grid cards" markdown>

-   :lucide-tag: **Container Labels**

    ---

    Complete reference for container label configuration

    [:octicons-arrow-right-24: Container Labels](container-labels.md)

-   :lucide-hard-drive: **Storage**

    ---

    Configure local, S3, and MinIO storage backends

    [:octicons-arrow-right-24: Storage](storage.md)

-   :lucide-bell: **Notifications**

    ---

    Set up Telegram and Discord notifications

    [:octicons-arrow-right-24: Notifications](notifications.md)

-   :lucide-layout-dashboard: **Dashboard**

    ---

    Enable and secure the web dashboard

    [:octicons-arrow-right-24: Dashboard](dashboard.md)

</div>
