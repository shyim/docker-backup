---
icon: simple/mysql
---

# MySQL / MariaDB Backup

The `mysql` backup type creates logical backups of MySQL and MariaDB databases using `mysqldump`.

## Overview

- **Backup Method**: `mysqldump` per database, packaged into a tar archive
- **Compression**: zstd compression
- **Output Format**: `.tar.zst` containing individual `.sql` files per database
- **Restore Method**: `mysql` client to restore SQL dumps
- **Compatibility**: MySQL 5.7+, MySQL 8.0, MariaDB 10.x, MariaDB 11.x

## Configuration

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=mysql
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7
```

## Requirements

### Environment Variables

The container must have MySQL credentials set via environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `MYSQL_ROOT_PASSWORD` | Yes* | Root password (preferred) |
| `MYSQL_USER` | Yes* | MySQL username |
| `MYSQL_PASSWORD` | Yes* | MySQL user password |
| `MYSQL_DATABASE` | No | Default database |

*Either `MYSQL_ROOT_PASSWORD` or both `MYSQL_USER` and `MYSQL_PASSWORD` must be set.

### Container Requirements

- MySQL client tools (`mysql`, `mysqldump`) must be available in the container
- For MariaDB 11+, `mariadb` and `mariadb-dump` commands are auto-detected
- The user must have permissions to dump all databases

## How It Works

### Backup Process

1. **Detect Client**: Checks for `mariadb`/`mariadb-dump` (MariaDB 11+) or falls back to `mysql`/`mysqldump`
2. **List Databases**: Queries `information_schema.schemata` to get all user databases
3. **Filter System DBs**: Excludes `mysql`, `information_schema`, `performance_schema`, `sys`
4. **Dump Each Database**: Runs `mysqldump` for each database with options:
   - `--single-transaction` - Consistent snapshot for InnoDB
   - `--routines` - Include stored procedures and functions
   - `--triggers` - Include triggers
   - `--events` - Include scheduled events
   - `--add-drop-database` - Include DROP DATABASE statement
5. **Package**: Creates a tar archive with each database as a separate `.sql` file
6. **Compress**: Applies zstd compression to the archive

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
3. **Restore**: Pipes each SQL dump to `mysql` client
4. **Recreate**: The `CREATE DATABASE` and `DROP DATABASE` statements recreate the databases

## Example Configurations

### Basic MySQL Setup

```yaml
services:
  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: secret
      MYSQL_DATABASE: myapp
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=mysql
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
```

### Basic MariaDB Setup

```yaml
services:
  mariadb:
    image: mariadb:11
    environment:
      MARIADB_ROOT_PASSWORD: secret
      MARIADB_DATABASE: myapp
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=mysql
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
```

### Multiple Backup Schedules

```yaml
services:
  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: secret
    labels:
      - docker-backup.enable=true
      - docker-backup.notify=telegram

      # Hourly backups (short retention)
      - docker-backup.hourly.type=mysql
      - docker-backup.hourly.schedule=0 * * * *
      - docker-backup.hourly.retention=24
      - docker-backup.hourly.storage=local-fast

      # Daily backups (long retention)
      - docker-backup.daily.type=mysql
      - docker-backup.daily.schedule=0 2 * * *
      - docker-backup.daily.retention=30
      - docker-backup.daily.storage=s3
```

### Non-Root User

```yaml
services:
  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: rootsecret
      MYSQL_USER: backup_user
      MYSQL_PASSWORD: backup_password
      MYSQL_DATABASE: myapp
    labels:
      - docker-backup.enable=true
      - docker-backup.db.type=mysql
      - docker-backup.db.schedule=0 3 * * *
```

!!! note "User Permissions"
    When using a non-root user, ensure it has sufficient privileges to backup all databases:
    ```sql
    GRANT SELECT, LOCK TABLES, SHOW VIEW, EVENT, TRIGGER ON *.* TO 'backup_user'@'%';
    ```

## Manual Operations

### Trigger Backup

```bash
docker-backup backup run mysql
```

### List Backups

```bash
docker-backup backup list mysql
```

Output:
```
KEY                                      SIZE      DATE
mysql/db/2024-01-15/030000.tar.zst      5.2 MB    2024-01-15 03:00:00
mysql/db/2024-01-14/030000.tar.zst      5.1 MB    2024-01-14 03:00:00
```

### Restore Backup

```bash
docker-backup backup restore mysql "mysql/db/2024-01-15/030000.tar.zst"
```

!!! warning "Restore Behavior"
    Restoring will:

    - Drop and recreate databases included in the backup
    - Overwrite any existing data in those databases
    - Not affect databases not included in the backup

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
mysql -u root -p < myapp.sql
```

## Troubleshooting

### "Missing MySQL password" Error

Ensure the container has either:

1. `MYSQL_ROOT_PASSWORD` set, OR
2. Both `MYSQL_USER` and `MYSQL_PASSWORD` set

```yaml
environment:
  MYSQL_ROOT_PASSWORD: secret
```

### Password Warning in Logs

The warning "Using a password on the command line interface can be insecure" is normal and doesn't affect the backup. The password is passed securely within the container.

### Backup Fails with Permission Error

The configured user must have permissions to:

- Connect to all databases
- Run `mysqldump` on all databases

For full backups, use root or grant necessary permissions:

```sql
GRANT SELECT, LOCK TABLES, SHOW VIEW, EVENT, TRIGGER ON *.* TO 'backup_user'@'%';
FLUSH PRIVILEGES;
```

### MariaDB 11+ Command Not Found

docker-backup automatically detects MariaDB 11+ and uses `mariadb`/`mariadb-dump` commands instead of `mysql`/`mysqldump`. Ensure these commands are available in your container.

### Large Databases Timing Out

For very large databases, consider:

1. Using `--single-transaction` (already enabled by default)
2. Backing up during low-traffic periods
3. Breaking up into multiple smaller backups
