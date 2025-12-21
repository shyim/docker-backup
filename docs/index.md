---
icon: lucide/database
---

# Docker Backup

A lightweight, label-driven backup daemon for Docker containers. Automatically discover and back up your containers based on simple labels, with support for multiple storage backends and notification providers.

## Features

- **Label-driven configuration** - Configure backups directly on your containers using Docker labels
- **Multiple backup configs** - Run different backup schedules and types per container
- **Pluggable storage backends** - Local filesystem, S3, MinIO, and more
- **Scheduled backups** - Cron-based scheduling with retention policies
- **Notifications** - Get notified via Telegram, Discord when backups complete or fail
- **Web dashboard** - Monitor and manage backups through a clean web interface
- **CLI tools** - Trigger backups, list, restore, and delete from the command line

## Quick Start

```yaml title="compose.yml"
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    command:
      - daemon
      - --storage=local.type=local
      - --storage=local.path=/backups
      - --default-storage=local
      - --dashboard=:8080
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - backup-data:/backups
    ports:
      - "8080:8080"

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

volumes:
  backup-data:
```

## How It Works

1. **Discovery** - The daemon monitors Docker for containers with `docker-backup.enable=true` label
2. **Scheduling** - Backups are scheduled based on cron expressions in container labels
3. **Execution** - At scheduled times, the appropriate backup type runs (e.g., `pg_dump` for PostgreSQL)
4. **Storage** - Backup files are stored in configured storage backends with automatic retention
5. **Notification** - Optional notifications are sent on backup success or failure

## Next Steps

<div class="grid cards" markdown>

-   :lucide-rocket: **Getting Started**

    ---

    Install docker-backup and create your first backup

    [:octicons-arrow-right-24: Installation](getting-started/index.md)

-   :lucide-settings: **Configuration**

    ---

    Learn about container labels, storage, and notifications

    [:octicons-arrow-right-24: Configuration](configuration/index.md)

-   :lucide-database: **Backup Types**

    ---

    Supported backup types and their options

    [:octicons-arrow-right-24: Backup Types](backup-types/index.md)

-   :lucide-terminal: **CLI Reference**

    ---

    Command-line interface documentation

    [:octicons-arrow-right-24: CLI Reference](cli-reference/index.md)

</div>
