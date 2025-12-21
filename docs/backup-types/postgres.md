---
icon: simple/postgresql
---

# PostgreSQL Backup

The `postgres` backup type creates logical backups of PostgreSQL databases using `pg_dump`.

## Overview

- **Backup Method**: `pg_dump` per database, packaged into a tar archive
- **Compression**: zstd compression
- **Output Format**: `.tar.zst` containing individual `.sql` files per database
- **Restore Method**: `psql` to restore SQL dumps

## Configuration

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7
```

## Requirements

### Environment Variables

The container must have PostgreSQL credentials set via environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `POSTGRES_USER` | Yes* | PostgreSQL username |
| `PGUSER` | Yes* | Alternative to POSTGRES_USER |
| `POSTGRES_PASSWORD` | No | Password (used by pg_dump) |
| `POSTGRES_DB` | No | Default database |

*At least one of `POSTGRES_USER` or `PGUSER` must be set.

### Container Requirements

- PostgreSQL client tools (`pg_dump`, `psql`) must be available in the container
- The user must have permissions to dump all databases

## How It Works

### Backup Process

1. **List Databases**: Queries `pg_database` to get all non-template databases
2. **Dump Each Database**: Runs `pg_dump` for each database with options:
   - `--clean` - Include DROP statements
   - `--if-exists` - Use IF EXISTS with DROP
   - `--create` - Include CREATE DATABASE statement
3. **Package**: Creates a tar archive with each database as a separate `.sql` file
4. **Compress**: Applies zstd compression to the archive

### Backup Contents

The backup file (`.tar.zst`) contains:

```
backup.tar.zst
├── myapp.sql      # Database 'myapp' dump
├── users.sql      # Database 'users' dump
└── analytics.sql  # Database 'analytics' dump
```

### Restore Process

1. **Decompress**: Reads zstd-compressed tar archive
2. **Extract**: Processes each `.sql` file in the archive
3. **Restore**: Pipes each SQL dump to `psql` connected to the `postgres` database
4. **Recreate**: The `CREATE DATABASE` statements in the dump recreate the databases

## Example Configurations

### Basic Setup

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: myuser
      POSTGRES_PASSWORD: mypassword
      POSTGRES_DB: myapp
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=postgres
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
```

### Multiple Backup Schedules

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: admin
      POSTGRES_PASSWORD: secret
    labels:
      - docker-backup.enable=true
      - docker-backup.notify=telegram

      # Hourly backups (short retention)
      - docker-backup.hourly.type=postgres
      - docker-backup.hourly.schedule=0 * * * *
      - docker-backup.hourly.retention=24
      - docker-backup.hourly.storage=local-fast

      # Daily backups (long retention)
      - docker-backup.daily.type=postgres
      - docker-backup.daily.schedule=0 2 * * *
      - docker-backup.daily.retention=30
      - docker-backup.daily.storage=s3
```

### With Replication/Standby

For replicated setups, back up from the standby:

```yaml
services:
  postgres-primary:
    image: postgres:16
    # Primary configuration...

  postgres-standby:
    image: postgres:16
    environment:
      POSTGRES_USER: replicator
      POSTGRES_PASSWORD: secret
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=postgres
      - docker-backup.db.schedule=0 3 * * *
```

## Manual Operations

### Trigger Backup

```bash
docker-backup backup run postgres
```

### List Backups

```bash
docker-backup backup list postgres
```

Output:
```
KEY                                        SIZE      DATE
postgres/db/2024-01-15/030000.tar.zst     2.1 MB    2024-01-15 03:00:00
postgres/db/2024-01-14/030000.tar.zst     2.0 MB    2024-01-14 03:00:00
```

### Restore Backup

```bash
docker-backup backup restore postgres "postgres/db/2024-01-15/030000.tar.zst"
```

!!! warning "Restore Behavior"
    Restoring will:

    - Drop and recreate databases included in the backup
    - Overwrite any existing data in those databases
    - Not affect databases not included in the backup

### Download Backup

Via CLI:
```bash
docker-backup backup download postgres "postgres/db/2024-01-15/030000.tar.zst" > backup.tar.zst
```

Via Dashboard:
Navigate to Backups > postgres and click the download button.

## Extracting Backups Manually

To manually extract and inspect a backup:

```bash
# Decompress and extract
zstd -d backup.tar.zst
tar -xf backup.tar

# View contents
ls -la
# myapp.sql  users.sql  analytics.sql

# Restore a single database manually
psql -U postgres -d postgres < myapp.sql
```

## Troubleshooting

### "Missing PostgreSQL user" Error

Ensure the container has `POSTGRES_USER` or `PGUSER` environment variable set:

```yaml
environment:
  POSTGRES_USER: myuser
```

### Backup Fails with Permission Error

The configured user must have permissions to:

- Connect to all databases
- Run `pg_dump` on all databases

For full backups, use a superuser or grant necessary permissions:

```sql
GRANT pg_read_all_data TO backup_user;
```

### Large Databases Timing Out

For very large databases, consider:

1. Increasing the write timeout on the daemon
2. Using incremental backup strategies
3. Backing up during low-traffic periods
