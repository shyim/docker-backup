---
icon: lucide/rocket
---

# Getting Started

This guide will help you install docker-backup and create your first backup.

## Installation

=== "Docker (Recommended)"

    The easiest way to run docker-backup is using Docker:

    ```yaml title="compose.yml"
    services:
      docker-backup:
        image: ghcr.io/shyim/docker-backup:latest
        restart: unless-stopped
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

    volumes:
      backup-data:
    ```

    ```bash
    docker compose up -d
    ```

=== "Binary"

    Download the latest release from GitHub:

    ```bash
    # Linux amd64
    curl -L https://github.com/shyim/docker-backup/releases/latest/download/docker-backup-linux-amd64 -o docker-backup
    chmod +x docker-backup
    sudo mv docker-backup /usr/local/bin/
    ```

    Run the daemon:

    ```bash
    docker-backup daemon \
      --storage=local.type=local \
      --storage=local.path=/var/backups/docker \
      --default-storage=local
    ```

=== "Build from Source"

    Requirements:

    - Go 1.22+
    - Node.js 20+ (for CSS build)
    - templ CLI

    ```bash
    git clone https://github.com/shyim/docker-backup.git
    cd docker-backup
    make build
    ```

## Verify Installation

Check that docker-backup is running:

```bash
# If running in Docker
docker logs docker-backup

# If running as binary
docker-backup --help
```

You should see the daemon start and begin monitoring containers.

## Next Steps

- [Create your first backup](first-backup.md)
- [Configure storage backends](../configuration/storage.md)
- [Set up notifications](../configuration/notifications.md)
