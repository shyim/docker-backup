---
icon: lucide/archive
---

# backup

Backup management commands for triggering, listing, deleting, and restoring backups.

## Synopsis

```bash
docker-backup backup <subcommand> <container> [key] [flags]
```

## Description

The `backup` command communicates with the running daemon via Unix socket to manage backups. The daemon must be running for these commands to work.

## Subcommands

### run

Trigger an immediate backup for a container.

```bash
docker-backup backup run <container>
```

#### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `container` | Yes | Container name |

#### Example

```bash
# Trigger backup for all configs on the container
docker-backup backup run postgres
```

---

### list

List all backups for a container.

```bash
docker-backup backup list <container>
```

#### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `container` | Yes | Container name |

#### Example

```bash
docker-backup backup list postgres
```

Output:
```
KEY                                        SIZE      DATE
postgres/db/2024-01-15/030000.tar.zst     2.1 MB    2024-01-15 03:00:00
postgres/db/2024-01-14/030000.tar.zst     2.0 MB    2024-01-14 03:00:00
postgres/db/2024-01-13/030000.tar.zst     1.9 MB    2024-01-13 03:00:00

Total: 3 backup(s)
```

---

### delete

Delete a specific backup.

```bash
docker-backup backup delete <container> <key>
```

#### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `container` | Yes | Container name |
| `key` | Yes | Backup key (from `list` output) |

#### Example

```bash
docker-backup backup delete postgres "postgres/db/2024-01-13/030000.tar.zst"
```

!!! warning
    Deleted backups cannot be recovered.

---

### restore

Restore a backup to a running container.

```bash
docker-backup backup restore <container> <key>
```

#### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `container` | Yes | Container name |
| `key` | Yes | Backup key (from `list` output) |

#### Example

```bash
docker-backup backup restore postgres "postgres/db/2024-01-15/030000.tar.zst"
```

!!! warning "Data Loss"
    Restoring will overwrite existing data. Make sure you have a current backup before restoring.

## Flags

### Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--socket` | `/var/run/docker-backup.sock` | Unix socket path |

## Examples

### Complete Workflow

```bash
# List available backups
docker-backup backup list my-postgres

# Trigger a new backup before making changes
docker-backup backup run my-postgres

# Verify the new backup
docker-backup backup list my-postgres

# If something goes wrong, restore
docker-backup backup restore my-postgres "my-postgres/db/2024-01-15/143022.tar.zst"

# Clean up old backups manually
docker-backup backup delete my-postgres "my-postgres/db/2024-01-10/030000.tar.zst"
```

### Docker Exec Usage

When running docker-backup in a container:

```bash
# Trigger backup
docker exec docker-backup docker-backup backup run postgres

# List backups
docker exec docker-backup docker-backup backup list postgres

# Restore backup
docker exec docker-backup docker-backup backup restore postgres "postgres/db/2024-01-15/030000.tar.zst"
```

### Scripting

```bash
#!/bin/bash
CONTAINER="postgres"

# Get latest backup key
LATEST=$(docker-backup backup list "$CONTAINER" | tail -2 | head -1 | awk '{print $1}')

if [ -n "$LATEST" ]; then
    echo "Latest backup: $LATEST"
    # Could trigger restore here if needed
fi
```

## Error Handling

### Common Errors

**"connection refused"**
: The daemon is not running. Start the daemon first.

**"container not found"**
: The container doesn't exist or doesn't have backup labels enabled.

**"backup not found"**
: The specified backup key doesn't exist. Use `list` to see available backups.

## See Also

- [Getting Started: Your First Backup](../getting-started/first-backup.md)
- [Container Labels](../configuration/container-labels.md)
- [Storage](../configuration/storage.md)
