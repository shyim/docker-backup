---
icon: lucide/bell
---

# Notifications

docker-backup can send notifications when backup events occur. Notifications are opt-in per container using the `docker-backup.notify` label.

## Notification Events

| Event | Description |
|-------|-------------|
| `backup_started` | Backup operation has begun |
| `backup_completed` | Backup completed successfully (includes size and duration) |
| `backup_failed` | Backup failed (includes error message) |
| `restore_started` | Restore operation has begun |
| `restore_completed` | Restore completed successfully |
| `restore_failed` | Restore failed (includes error message) |

## Provider Configuration

### CLI Configuration

```bash
docker-backup daemon \
  --notify=<provider-name>.<option>=<value>
```

### Environment Variable Configuration

```bash
DOCKER_BACKUP_NOTIFY_<PROVIDER>_<OPTION>=value
```

Provider names are uppercase in environment variables.

## Telegram

Send notifications via Telegram Bot API.

### Setup

1. Create a bot with [@BotFather](https://t.me/botfather)
2. Get your chat ID (send a message to your bot, then check `https://api.telegram.org/bot<TOKEN>/getUpdates`)

### Configuration

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --notify=telegram.type=telegram \
      --notify=telegram.token=123456:ABC-DEF... \
      --notify=telegram.chat-id=-1001234567890
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_TELEGRAM_TYPE=telegram
    DOCKER_BACKUP_NOTIFY_TELEGRAM_TOKEN=123456:ABC-DEF...
    DOCKER_BACKUP_NOTIFY_TELEGRAM_CHAT_ID=-1001234567890
    ```

### Options

| Option | Required | Description |
|--------|----------|-------------|
| `type` | Yes | Must be `telegram` |
| `token` | Yes | Bot token from @BotFather |
| `chat-id` | Yes | Chat/group/channel ID to send messages to |

### Example Message

```
✅ Backup Completed

Container: postgres
Config: db
Size: 1.2 MB
Duration: 3.2s
Storage: s3
```

## Discord

Send notifications via Discord webhooks.

### Setup

1. Go to Server Settings > Integrations > Webhooks
2. Create a new webhook and copy the URL

### Configuration

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --notify=discord.type=discord \
      --notify=discord.webhook-url=https://discord.com/api/webhooks/...
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_DISCORD_TYPE=discord
    DOCKER_BACKUP_NOTIFY_DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
    DOCKER_BACKUP_NOTIFY_DISCORD_USERNAME=Docker Backup
    ```

### Options

| Option | Required | Default | Description |
|--------|----------|---------|-------------|
| `type` | Yes | - | Must be `discord` |
| `webhook-url` | Yes | - | Discord webhook URL |
| `username` | No | `Docker Backup` | Bot username shown in messages |

## Multiple Providers

Configure multiple notification providers:

```bash
docker-backup daemon \
  --notify=telegram.type=telegram \
  --notify=telegram.token=123456:ABC... \
  --notify=telegram.chat-id=-100123... \
  --notify=discord.type=discord \
  --notify=discord.webhook-url=https://discord.com/api/webhooks/...
```

## Container Configuration

Notifications are opt-in. Enable them per container using labels.

### Enable for Container

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
```

### Multiple Providers

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram,discord
```

### Per-Config Override

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.notify=telegram

  # Uses global notify (telegram)
  - docker-backup.hourly.type=postgres
  - docker-backup.hourly.schedule=0 * * * *

  # Override: send to discord instead
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 3 * * *
  - docker-backup.daily.notify=discord
```

## Complete Example

```yaml title="compose.yml"
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    environment:
      # Telegram for critical alerts
      DOCKER_BACKUP_NOTIFY_TELEGRAM_TYPE: telegram
      DOCKER_BACKUP_NOTIFY_TELEGRAM_TOKEN: ${TELEGRAM_BOT_TOKEN}
      DOCKER_BACKUP_NOTIFY_TELEGRAM_CHAT_ID: ${TELEGRAM_CHAT_ID}
      # Discord for team channel
      DOCKER_BACKUP_NOTIFY_DISCORD_TYPE: discord
      DOCKER_BACKUP_NOTIFY_DISCORD_WEBHOOK_URL: ${DISCORD_WEBHOOK_URL}
    command:
      - daemon
      - --storage=local.type=local
      - --storage=local.path=/backups
      - --default-storage=local

  postgres:
    image: postgres:16
    labels:
      - docker-backup.enable=true
      - docker-backup.notify=telegram,discord
      - docker-backup.db.type=postgres
      - docker-backup.db.schedule=0 3 * * *
```
