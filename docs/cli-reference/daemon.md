---
icon: lucide/server
---

# daemon

Start the backup daemon that monitors containers and performs scheduled backups.

## Synopsis

```bash
docker-backup daemon [flags]
```

## Description

The daemon:

1. Connects to Docker and discovers containers with backup labels
2. Schedules backup jobs based on cron expressions
3. Executes backups at scheduled times
4. Enforces retention policies
5. Sends notifications on backup events
6. Exposes a Unix socket API for CLI commands
7. Optionally runs a web dashboard

## Flags

### Docker Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--docker-host` | `unix:///var/run/docker.sock` | Docker daemon socket |
| `--poll-interval` | `30s` | How often to scan for container changes |

### Storage Configuration

| Flag | Description |
|------|-------------|
| `--storage=<pool>.<option>=<value>` | Configure storage pools (repeatable) |
| `--default-storage=<pool>` | Default storage pool name |
| `--temp-dir` | Temporary directory for backup files |

### Notification Configuration

| Flag | Description |
|------|-------------|
| `--notify=<provider>.<option>=<value>` | Configure notification providers (repeatable) |

### API & Dashboard

| Flag | Default | Description |
|------|---------|-------------|
| `--socket` | `/var/run/docker-backup.sock` | Unix socket path for CLI |
| `--dashboard` | (disabled) | Dashboard listen address (e.g., `:8080`) |
| `--dashboard.auth.basic` | (disabled) | htpasswd file or inline credentials |
| `--dashboard.auth.oidc.provider` | (disabled) | OIDC provider: `google`, `github`, or `oidc` |
| `--dashboard.auth.oidc.issuer-url` | | OIDC issuer URL (for generic provider) |
| `--dashboard.auth.oidc.client-id` | | OAuth client ID |
| `--dashboard.auth.oidc.client-secret` | | OAuth client secret |
| `--dashboard.auth.oidc.redirect-url` | | OAuth callback URL |
| `--dashboard.auth.oidc.allowed-users` | | Allowed email addresses (comma-separated) |
| `--dashboard.auth.oidc.allowed-domains` | | Allowed email domains (comma-separated) |

### Volume Backups

| Flag | Default | Description |
|------|---------|-------------|
| `--volume-base-path` | `/var/lib/docker/volumes` | Path where Docker volumes are mounted |

### Logging

| Flag | Default | Description |
|------|---------|-------------|
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--log-format` | `text` | Log format: text, json |

## Examples

### Basic Local Storage

```bash
docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/backups \
  --default-storage=local
```

### S3 Storage with Dashboard

```bash
docker-backup daemon \
  --storage=s3.type=s3 \
  --storage=s3.bucket=my-backups \
  --storage=s3.region=us-east-1 \
  --storage=s3.access-key=AKIA... \
  --storage=s3.secret-key=secret... \
  --default-storage=s3 \
  --dashboard=:8080 \
  --dashboard.auth.basic=/etc/docker-backup/htpasswd
```

### Multiple Storage Pools

```bash
docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/backups \
  --storage=s3.type=s3 \
  --storage=s3.bucket=offsite-backups \
  --storage=s3.region=us-west-2 \
  --storage=s3.access-key=AKIA... \
  --storage=s3.secret-key=secret... \
  --default-storage=local
```

### With Notifications

```bash
docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/backups \
  --default-storage=local \
  --notify=telegram.type=telegram \
  --notify=telegram.token=123456:ABC... \
  --notify=telegram.chat-id=-100123...
```

### Debug Mode

```bash
docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/backups \
  --default-storage=local \
  --log-level=debug \
  --log-format=json
```

### Dashboard with OIDC Authentication

```bash
docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/backups \
  --default-storage=local \
  --dashboard=:8080 \
  --dashboard.auth.oidc.provider=google \
  --dashboard.auth.oidc.client-id=YOUR_CLIENT_ID.apps.googleusercontent.com \
  --dashboard.auth.oidc.client-secret=YOUR_CLIENT_SECRET \
  --dashboard.auth.oidc.redirect-url=http://localhost:8080/auth/callback \
  --dashboard.auth.oidc.allowed-domains=example.com
```

## Environment Variables

All flags can be set via environment variables. See [Configuration](../configuration/index.md#environment-variables) for details.

## Signals

| Signal | Behavior |
|--------|----------|
| `SIGINT` | Graceful shutdown |
| `SIGTERM` | Graceful shutdown |

## See Also

- [Configuration](../configuration/index.md)
- [Storage](../configuration/storage.md)
- [Notifications](../configuration/notifications.md)
- [Dashboard](../configuration/dashboard.md)
