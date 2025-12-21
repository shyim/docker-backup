---
icon: lucide/tag
---

# Container Labels

docker-backup uses Docker labels to configure backups on individual containers. This allows backup configuration to live alongside your container definitions.

## Label Format

Labels follow the pattern:

```
docker-backup.<config-name>.<option>=<value>
```

Where:

- `docker-backup` is the fixed label prefix
- `<config-name>` is a unique name for this backup configuration
- `<option>` is the configuration option

## Basic Example

```yaml
labels:
  # Enable backup discovery
  - docker-backup.enable=true

  # Define a backup config named "db"
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7
```

## Label Reference

### Global Labels

These labels apply to the entire container:

| Label | Required | Description |
|-------|----------|-------------|
| `docker-backup.enable` | Yes | Set to `true` to enable backup discovery |
| `docker-backup.notify` | No | Comma-separated list of notification providers |

### Backup Config Labels

These labels define individual backup configurations. Replace `<name>` with your config name (e.g., `db`, `files`, `data`):

| Label | Required | Default | Description |
|-------|----------|---------|-------------|
| `docker-backup.<name>.type` | Yes | - | Backup type (e.g., `postgres`, `mysql`) |
| `docker-backup.<name>.schedule` | Yes | - | Cron expression for scheduling |
| `docker-backup.<name>.retention` | No | `7` | Number of backups to keep |
| `docker-backup.<name>.storage` | No | Default pool | Storage pool name |
| `docker-backup.<name>.notify` | No | Global notify | Override notification providers |

## Multiple Backup Configurations

A single container can have multiple backup configurations with different schedules, types, or storage destinations:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram

  # Hourly database backup to S3
  - docker-backup.hourly.type=postgres
  - docker-backup.hourly.schedule=0 * * * *
  - docker-backup.hourly.retention=24
  - docker-backup.hourly.storage=s3

  # Daily database backup to local (long-term)
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 3 * * *
  - docker-backup.daily.retention=30
  - docker-backup.daily.storage=local
```

## Cron Schedule Format

The schedule uses standard cron syntax:

```
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, Sunday=0)
│ │ │ │ │
* * * * *
```

### Common Schedules

| Schedule | Description |
|----------|-------------|
| `0 3 * * *` | Daily at 3:00 AM |
| `0 */6 * * *` | Every 6 hours |
| `0 * * * *` | Every hour |
| `*/15 * * * *` | Every 15 minutes |
| `0 3 * * 0` | Weekly on Sunday at 3:00 AM |
| `0 3 1 * *` | Monthly on the 1st at 3:00 AM |

## Storage Selection

### Using Default Storage

If no storage is specified, the default storage pool is used:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  # Uses --default-storage pool
```

### Specifying Storage Pool

Override the storage pool for specific backups:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.storage=s3-offsite
```

## Notifications

### Container-Level Notifications

Apply to all backup configs on the container:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram,discord
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
```

### Per-Config Notifications

Override notifications for specific backup configs:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram

  # This config uses global notify (telegram)
  - docker-backup.hourly.type=postgres
  - docker-backup.hourly.schedule=0 * * * *

  # This config overrides to discord only
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 3 * * *
  - docker-backup.daily.notify=discord
```

## Complete Example

```yaml title="compose.yml"
services:
  app-db:
    image: postgres:16
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: application
    labels:
      # Enable backups
      - docker-backup.enable=true

      # Send notifications to telegram for all backups
      - docker-backup.notify=telegram

      # Frequent backups to fast local storage
      - docker-backup.frequent.type=postgres
      - docker-backup.frequent.schedule=0 * * * *
      - docker-backup.frequent.retention=24
      - docker-backup.frequent.storage=local-fast

      # Daily backups to S3 for disaster recovery
      - docker-backup.daily.type=postgres
      - docker-backup.daily.schedule=0 2 * * *
      - docker-backup.daily.retention=30
      - docker-backup.daily.storage=s3-backup

      # Weekly full backup with longer retention
      - docker-backup.weekly.type=postgres
      - docker-backup.weekly.schedule=0 3 * * 0
      - docker-backup.weekly.retention=12
      - docker-backup.weekly.storage=s3-archive
```
