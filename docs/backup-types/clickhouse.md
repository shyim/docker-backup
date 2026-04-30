---
icon: simple/clickhouse
---

# ClickHouse Backup

The `clickhouse` backup type creates backups of ClickHouse databases using the native `BACKUP`/`RESTORE` SQL commands available in ClickHouse 22.8+.

## Overview

- **Backup Method**: Native `BACKUP DATABASE` SQL via `clickhouse-client`
- **Compression**: zstd compression
- **Output Format**: `.tar.zst` containing the ClickHouse backup directory
- **Restore Method**: Native `RESTORE` SQL via `clickhouse-client`
- **Compatibility**: ClickHouse 22.8+

## Configuration

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=clickhouse
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7
```

## Requirements

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `CLICKHOUSE_USER` | No | `default` | ClickHouse username |
| `CLICKHOUSE_PASSWORD` | No | *(empty)* | ClickHouse password |
| `CLICKHOUSE_DB` | No | *(all user DBs)* | Specific database to back up |

ClickHouse works with default credentials out of the box, so no environment variables are strictly required.

### Container Requirements

- ClickHouse server version **22.8 or later** (for native `BACKUP`/`RESTORE` support)
- `clickhouse-client` must be available in the container (included in official `clickhouse/clickhouse-server` images)

## How It Works

### Backup Process

1. **Version Check**: Verifies `clickhouse-client` exists and ClickHouse version is >= 22.8
2. **Configure Path**: Writes a config snippet to allow `/tmp/docker-backup/` as a backup path
3. **Discover Databases**: If `CLICKHOUSE_DB` is set, backs up that database only. Otherwise, queries `system.databases` and backs up all user databases (excluding `system`, `INFORMATION_SCHEMA`, `information_schema`, `default`)
4. **Execute Backup**: Runs `BACKUP DATABASE ... TO File('/tmp/docker-backup/<uuid>/')` inside the container
5. **Stream Out**: Tars the backup directory and streams it through zstd compression to storage
6. **Cleanup**: Removes temporary backup files from the container

### Backup Contents

The backup file (`.tar.zst`) contains a ClickHouse native backup directory:

```
backup.tar.zst
└── <uuid>/
    ├── .backup          # Backup metadata
    └── data/            # Table data parts and schema
```

### Restore Process

1. **Decompress**: Reads the zstd-compressed tar archive
2. **Extract**: Pipes the tar stream into the container, recreating the backup directory
3. **Execute Restore**: Runs `RESTORE ALL FROM File('/tmp/docker-backup/<uuid>/') SETTINGS allow_non_empty_tables=true`
4. **Cleanup**: Removes temporary files from the container

## Example Configurations

### Basic Setup

```yaml
services:
  clickhouse:
    image: clickhouse/clickhouse-server:latest
    environment:
      CLICKHOUSE_USER: admin
      CLICKHOUSE_PASSWORD: secret
      CLICKHOUSE_DB: analytics
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=clickhouse
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
```

### Default Credentials

ClickHouse uses `default` user with no password by default. No env vars needed:

```yaml
services:
  clickhouse:
    image: clickhouse/clickhouse-server:latest
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=clickhouse
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
```

### Multiple Backup Schedules

```yaml
services:
  clickhouse:
    image: clickhouse/clickhouse-server:latest
    environment:
      CLICKHOUSE_USER: admin
      CLICKHOUSE_PASSWORD: secret
      CLICKHOUSE_DB: analytics
    labels:
      - docker-backup.enable=true
      - docker-backup.notify=telegram

      # Hourly backups (short retention)
      - docker-backup.hourly.type=clickhouse
      - docker-backup.hourly.schedule=0 * * * *
      - docker-backup.hourly.retention=24
      - docker-backup.hourly.storage=local-fast

      # Daily backups (long retention)
      - docker-backup.daily.type=clickhouse
      - docker-backup.daily.schedule=0 2 * * *
      - docker-backup.daily.retention=30
      - docker-backup.daily.storage=s3
```

### With S3 Storage

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    command:
      - daemon
      - --storage=s3.type=s3
      - --storage=s3.bucket=my-backups
      - --storage=s3.region=us-east-1
      - --storage=s3.access-key=AKIA...
      - --storage=s3.secret-key=secret
      - --default-storage=s3
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  clickhouse:
    image: clickhouse/clickhouse-server:latest
    environment:
      CLICKHOUSE_USER: admin
      CLICKHOUSE_PASSWORD: secret
      CLICKHOUSE_DB: analytics
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=clickhouse
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
      - docker-backup.db.storage=s3
```

## Manual Operations

### Trigger Backup

```bash
docker-backup backup run clickhouse
```

### List Backups

```bash
docker-backup backup list clickhouse
```

Output:
```
KEY                                             SIZE      DATE
clickhouse/db/2024-01-15/030000.tar.zst        2.1 MB    2024-01-15 03:00:00
clickhouse/db/2024-01-14/030000.tar.zst        2.0 MB    2024-01-14 03:00:00
```

### Restore Backup

```bash
docker-backup backup restore clickhouse "clickhouse/db/2024-01-15/030000.tar.zst"
```

!!! warning "Restore Behavior"
    Restoring will:

    - Recreate databases and tables included in the backup
    - Use `allow_non_empty_tables=true`, which may mix existing data with restored data
    - Not affect databases not included in the backup

## Troubleshooting

### "clickhouse-client not found" Error

Ensure you're using an official ClickHouse Docker image (`clickhouse/clickhouse-server`), which includes the client binary. Minimal or custom images may not have it.

### "version X.Y is too old" Error

Native `BACKUP`/`RESTORE` requires ClickHouse 22.8 or later. Upgrade your ClickHouse image:

```yaml
image: clickhouse/clickhouse-server:latest
```

### "Path is not allowed for backups" Error

docker-backup automatically configures the backup path on first run. If you see this error, it may indicate the config reload failed. Verify the ClickHouse server can write to `/tmp/docker-backup/`:

```bash
docker exec clickhouse ls -la /etc/clickhouse-server/config.d/
```

### Large Databases

For very large ClickHouse databases, consider:

1. Setting `CLICKHOUSE_DB` to back up a specific database instead of all databases
2. Backing up during low-traffic periods to minimize I/O impact
3. Ensuring sufficient temporary disk space inside the container (backup is staged at `/tmp/docker-backup/` before streaming)
