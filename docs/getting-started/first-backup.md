---
icon: lucide/play
---

# Your First Backup

This guide walks you through setting up your first automated backup.

## Prerequisites

- docker-backup daemon running (see [Installation](installation.md))
- A container you want to back up (we'll use PostgreSQL as an example)

## Step 1: Add Backup Labels

Add labels to your container to enable backups:

```yaml title="compose.yml"
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: myuser
      POSTGRES_PASSWORD: mypassword
      POSTGRES_DB: myapp
    labels:
      # Enable docker-backup for this container
      - docker-backup.enable=true
      # Define a backup configuration named "db"
      - docker-backup.db.type=postgres
      - docker-backup.db.schedule=0 3 * * *
      - docker-backup.db.retention=7
```

### Label Breakdown

| Label | Description |
|-------|-------------|
| `docker-backup.enable=true` | Enables backup discovery for this container |
| `docker-backup.db.type=postgres` | Use PostgreSQL backup type for config "db" |
| `docker-backup.db.schedule=0 3 * * *` | Run daily at 3:00 AM |
| `docker-backup.db.retention=7` | Keep last 7 backups |

## Step 2: Restart Your Container

Apply the labels by recreating the container:

```bash
docker compose up -d
```

## Step 3: Verify Discovery

Check the docker-backup logs to confirm your container was discovered:

```bash
docker logs docker-backup
```

You should see:

```
INFO starting docker-backup daemon
INFO container discovered container=postgres backups=1
INFO scheduled backup container=postgres config=db next_run="2024-01-16 03:00:00"
```

## Step 4: Trigger a Manual Backup

Don't want to wait for the schedule? Trigger a backup immediately:

```bash
# Using the CLI (if running as binary)
docker-backup backup run postgres

# Or via Docker exec
docker exec docker-backup docker-backup backup run postgres
```

## Step 5: Verify the Backup

List your backups:

```bash
docker-backup backup list postgres
```

Output:

```
KEY                                           SIZE      DATE
postgres/db/2024-01-15/143022.sql.gz         1.2 MB    2024-01-15 14:30:22

Total: 1 backup(s)
```

## Step 6: Access the Dashboard

If you enabled the dashboard (`--dashboard=:8080`), open your browser to:

```
http://localhost:8080
```

You'll see:

- Overview of all monitored containers
- Backup configurations and schedules
- Recent backup history
- Options to trigger, download, or restore backups

## What's Next?

- [Configure multiple backups per container](../guides/multiple-backups.md)
- [Set up S3 storage](../configuration/storage.md#s3)
- [Enable notifications](../configuration/notifications.md)
- [Secure the dashboard](../configuration/dashboard.md#authentication)
