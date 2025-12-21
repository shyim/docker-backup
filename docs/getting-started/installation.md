---
icon: lucide/download
---

# Installation

## Docker

The recommended way to run docker-backup is using Docker Compose:

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

### Required Volumes

| Volume | Purpose |
|--------|---------|
| `/var/run/docker.sock` | Docker socket for container discovery and exec |
| `/backups` | Local storage for backups (if using local storage) |

### Environment Variables

Storage and notification configuration can be passed via environment variables:

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    environment:
      # S3 storage
      DOCKER_BACKUP_STORAGE_S3_TYPE: s3
      DOCKER_BACKUP_STORAGE_S3_BUCKET: my-backups
      DOCKER_BACKUP_STORAGE_S3_REGION: us-east-1
      DOCKER_BACKUP_STORAGE_S3_ACCESS_KEY: ${AWS_ACCESS_KEY_ID}
      DOCKER_BACKUP_STORAGE_S3_SECRET_KEY: ${AWS_SECRET_ACCESS_KEY}
      DOCKER_BACKUP_DEFAULT_STORAGE: s3
      # Telegram notifications
      DOCKER_BACKUP_NOTIFY_TELEGRAM_TYPE: telegram
      DOCKER_BACKUP_NOTIFY_TELEGRAM_TOKEN: ${TELEGRAM_BOT_TOKEN}
      DOCKER_BACKUP_NOTIFY_TELEGRAM_CHAT_ID: ${TELEGRAM_CHAT_ID}
```

## Binary Installation

### Download Pre-built Binary

```bash
# Linux amd64
curl -L https://github.com/shyim/docker-backup/releases/latest/download/docker-backup-linux-amd64 -o docker-backup

# Linux arm64
curl -L https://github.com/shyim/docker-backup/releases/latest/download/docker-backup-linux-arm64 -o docker-backup

# Make executable
chmod +x docker-backup

# Move to PATH
sudo mv docker-backup /usr/local/bin/
```

### Systemd Service

Create a systemd service file:

```ini title="/etc/systemd/system/docker-backup.service"
[Unit]
Description=Docker Backup Daemon
After=docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/docker-backup daemon \
  --storage=local.type=local \
  --storage=local.path=/var/backups/docker \
  --default-storage=local
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable docker-backup
sudo systemctl start docker-backup
```

## Build from Source

### Requirements

- Go 1.22 or later
- Node.js 20+ (for Tailwind CSS build)
- templ CLI (`go install github.com/a-h/templ/cmd/templ@latest`)

### Build Steps

```bash
# Clone repository
git clone https://github.com/shyim/docker-backup.git
cd docker-backup

# Install dependencies and build
make build

# Binary is at ./docker-backup
./docker-backup --help
```

### Development Build

For development with live reload:

```bash
# Watch CSS changes
npm run watch:css

# In another terminal, regenerate templates on change
templ generate --watch

# Run the daemon
go run ./cmd/docker-backup daemon --storage=local.type=local --storage=local.path=./backups
```
