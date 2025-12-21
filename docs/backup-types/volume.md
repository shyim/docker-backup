---
icon: lucide/hard-drive
---

# Volume Backup

The `volume` backup type backs up all mounted volumes from a container. This is useful when you want to back up application data stored in Docker volumes without needing application-specific backup tools.

## Overview

- **Backup Method**: Creates a tar archive of all mounted volume contents
- **Compression**: zstd compression
- **Output Format**: `.tar.zst` containing all volume contents
- **Container Handling**: Stops the container before backup, restarts after
- **Configuration**: Labels on containers, like other backup types

## Configuration

Volume backups are configured using container labels, just like other backup types:

```yaml
services:
  app:
    image: myapp
    volumes:
      - app-data:/data
      - app-config:/config
    labels:
      - docker-backup.enable=true
      - docker-backup.data.type=volume
      - docker-backup.data.schedule=0 3 * * *
      - docker-backup.data.retention=7
      - docker-backup.data.storage=local

volumes:
  app-data:
  app-config:
```

## Requirements

### Container Must Have Mounted Volumes

The backup type validates that the container has at least one mounted volume. Bind mounts and tmpfs mounts are excluded - only named Docker volumes are backed up.

## How It Works

### Backup Process

1. **Validate**: Checks that the container has mounted volumes
2. **Stop Container**: Stops the container to ensure data consistency
3. **Create Archive**: Creates a tar.zst archive containing all volume mount points
4. **Restart Container**: Restarts the container
5. **Store**: Uploads the backup to the configured storage

### Archive Structure

The archive preserves the mount structure. For a container with volumes mounted at `/data` and `/config`:

```
data/
  file1.txt
  subdir/
    file2.txt
config/
  settings.json
```

### Restore Process

1. **Stop Container**: Stops the container
2. **Extract Archive**: Extracts the backup archive to the volume mount points
3. **Restart Container**: Restarts the container

## Example Configurations

### Basic Volume Backup

```yaml
services:
  app:
    image: myapp
    volumes:
      - app-data:/data
    labels:
      - docker-backup.enable=true
      - docker-backup.files.type=volume
      - docker-backup.files.schedule=0 3 * * *
```

### Combined with Database Backup

Back up both the database and application files:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - postgres-data:/var/lib/postgresql/data
    labels:
      - docker-backup.enable=true
      # Database backup using pg_dump
      - docker-backup.db.type=postgres
      - docker-backup.db.schedule=0 2 * * *
      - docker-backup.db.retention=14
      # Volume backup for config files or other data
      - docker-backup.files.type=volume
      - docker-backup.files.schedule=0 3 * * *
      - docker-backup.files.retention=7
```

### Application with Multiple Volumes

```yaml
services:
  wordpress:
    image: wordpress
    volumes:
      - wp-content:/var/www/html/wp-content
      - wp-uploads:/var/www/html/wp-content/uploads
    labels:
      - docker-backup.enable=true
      - docker-backup.data.type=volume
      - docker-backup.data.schedule=0 4 * * *
      - docker-backup.data.retention=7
```

## CLI Commands

Volume backups use the same backup commands as other backup types:

### Trigger Backup

```bash
docker-backup backup run my-app
```

### List Backups

```bash
docker-backup backup list my-app
```

### Restore Backup

```bash
docker-backup backup restore my-app "my-app/files/2024-01-15/030000.tar.zst"
```

## Storage Key Format

Volume backups are stored with the following key format:

```
{container-name}/{config-name}/{YYYY-MM-DD}/{HHMMSS}.tar.zst
```

Example: `my-app/files/2024-01-15/030000.tar.zst`

## Extracting Backups Manually

To manually extract and inspect a volume backup:

```bash
# Decompress and extract
zstd -d backup.tar.zst
tar -xf backup.tar -C /path/to/restore

# Or in one step
zstd -dc backup.tar.zst | tar -x -C /path/to/restore
```

## When to Use Volume Backup

Use the `volume` backup type when:

- Your application doesn't have a dedicated backup tool
- You want to back up configuration files alongside database backups
- You need a simple file-level backup of application data

For databases, prefer using database-specific backup types (`postgres`, `mysql`) which create consistent point-in-time backups.

## Troubleshooting

### "container has no mounted volumes" Error

This error means the container has no named Docker volumes mounted. Check that:

1. The container has volumes defined in the compose file
2. The volumes are named volumes, not bind mounts
3. The container is running and accessible

### Container Fails to Restart After Backup

If a container fails to restart after backup:

1. Check the container logs: `docker logs <container>`
2. Manually start the container: `docker start <container>`
3. Verify the backup completed successfully
