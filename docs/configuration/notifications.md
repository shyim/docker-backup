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

Notification providers are configured using DSN (Data Source Name) strings. The notification system uses [go-notifier](https://github.com/shyim/go-notifier), which supports:

- **Telegram** - Chat-based notifications
- **Slack** - Team messaging
- **Discord** - Server notifications with rich embeds
- **Gotify** - Self-hosted push notifications
- **Microsoft Teams** - Enterprise messaging

### CLI Configuration

```bash
docker-backup daemon \
  --notify=<provider-name>=<dsn>
```

### Environment Variable Configuration

```bash
DOCKER_BACKUP_NOTIFY_<PROVIDER>=dsn
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
      --notify=telegram='telegram://BOT_TOKEN@default?channel=CHAT_ID'
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_TELEGRAM='telegram://123456:ABC-DEF@default?channel=-1001234567890'
    ```

### DSN Format

```
telegram://BOT_TOKEN@default?channel=CHAT_ID
```

- `BOT_TOKEN`: Bot token from @BotFather
- `CHAT_ID`: Chat/group/channel ID to send messages to

### Example Message

```
Backup Completed

Container: postgres
Type: postgres
Size: 1.2 MB
Duration: 3.2s
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
      --notify=discord='discord://WEBHOOK_TOKEN@default?webhook_id=WEBHOOK_ID'
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_DISCORD='discord://webhook_token@default?webhook_id=1234567890'
    ```

### DSN Format

```
discord://WEBHOOK_TOKEN@default?webhook_id=WEBHOOK_ID
```

Extract the `WEBHOOK_ID` and `WEBHOOK_TOKEN` from your webhook URL:
```
https://discord.com/api/webhooks/WEBHOOK_ID/WEBHOOK_TOKEN
```

## Slack

Send notifications via Slack.

### Configuration

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --notify=slack='slack://BOT_TOKEN@default?channel=CHANNEL_ID'
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_SLACK='slack://xoxb-token@default?channel=C1234567890'
    ```

### DSN Format

```
slack://BOT_TOKEN@default?channel=CHANNEL_ID
```

## Gotify

Send notifications to a self-hosted Gotify server.

### Configuration

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --notify=gotify='gotify://APP_TOKEN@SERVER_HOST'
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_GOTIFY='gotify://app_token@gotify.example.com'
    ```

### DSN Format

```
gotify://APP_TOKEN@SERVER_HOST
```

## Microsoft Teams

Send notifications via Microsoft Teams webhooks.

### Configuration

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --notify=teams='microsoftteams://default?webhook_url=WEBHOOK_URL'
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_NOTIFY_TEAMS='microsoftteams://default?webhook_url=https://...'
    ```

### DSN Format

```
microsoftteams://default?webhook_url=WEBHOOK_URL
```

## Multiple Providers

Configure multiple notification providers:

```bash
docker-backup daemon \
  --notify=telegram='telegram://123456:ABC@default?channel=-100123...' \
  --notify=discord='discord://webhook_token@default?webhook_id=1234567890'
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
      DOCKER_BACKUP_NOTIFY_TELEGRAM: 'telegram://${TELEGRAM_BOT_TOKEN}@default?channel=${TELEGRAM_CHAT_ID}'
      # Discord for team channel
      DOCKER_BACKUP_NOTIFY_DISCORD: 'discord://${DISCORD_WEBHOOK_TOKEN}@default?webhook_id=${DISCORD_WEBHOOK_ID}'
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
