---
icon: lucide/layers
---

# Multiple Backup Configurations

docker-backup supports multiple backup configurations per container, allowing you to:

- Run backups on different schedules
- Store backups in different locations
- Use different retention policies
- Send different notifications

## Named Configurations

Each backup configuration has a unique name that you choose:

```yaml
labels:
  - docker-backup.enable=true
  # Config named "hourly"
  - docker-backup.hourly.type=postgres
  - docker-backup.hourly.schedule=0 * * * *
  # Config named "daily"
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 3 * * *
```

The name (e.g., `hourly`, `daily`) is used in:

- Label names: `docker-backup.<name>.<option>`
- Backup paths: `container/<name>/YYYY-MM-DD/HHMMSS.ext`
- Dashboard display
- Scheduler job identification

## Use Cases

### Different Schedules

Run frequent backups for point-in-time recovery and daily backups for disaster recovery:

```yaml
labels:
  - docker-backup.enable=true

  # Every 15 minutes for point-in-time recovery
  - docker-backup.frequent.type=postgres
  - docker-backup.frequent.schedule=*/15 * * * *
  - docker-backup.frequent.retention=96  # 24 hours worth

  # Daily for disaster recovery
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 2 * * *
  - docker-backup.daily.retention=30

  # Weekly archive
  - docker-backup.weekly.type=postgres
  - docker-backup.weekly.schedule=0 3 * * 0
  - docker-backup.weekly.retention=52
```

### Different Storage Destinations

Store fast-access backups locally and archive backups in S3:

```yaml
labels:
  - docker-backup.enable=true

  # Fast local storage for recent backups
  - docker-backup.local.type=postgres
  - docker-backup.local.schedule=0 * * * *
  - docker-backup.local.retention=24
  - docker-backup.local.storage=local-ssd

  # S3 for offsite disaster recovery
  - docker-backup.offsite.type=postgres
  - docker-backup.offsite.schedule=0 4 * * *
  - docker-backup.offsite.retention=90
  - docker-backup.offsite.storage=s3-archive
```

### Different Retention Policies

Balance storage costs with recovery requirements:

```yaml
labels:
  - docker-backup.enable=true

  # Short retention for frequent backups (storage efficient)
  - docker-backup.short.type=postgres
  - docker-backup.short.schedule=0 * * * *
  - docker-backup.short.retention=6  # 6 hours

  # Long retention for daily backups
  - docker-backup.long.type=postgres
  - docker-backup.long.schedule=0 3 * * *
  - docker-backup.long.retention=365  # 1 year
```

### Per-Config Notifications

Get instant alerts for critical backups, daily summaries for routine ones:

```yaml
labels:
  - docker-backup.enable=true

  # Production database - immediate alerts
  - docker-backup.prod.type=postgres
  - docker-backup.prod.schedule=0 * * * *
  - docker-backup.prod.notify=telegram,pagerduty

  # Archive backup - daily summary only
  - docker-backup.archive.type=postgres
  - docker-backup.archive.schedule=0 3 * * *
  - docker-backup.archive.notify=discord
```

## Naming Conventions

Choose descriptive names that indicate the purpose:

| Name | Purpose |
|------|---------|
| `hourly`, `daily`, `weekly` | Schedule-based naming |
| `local`, `s3`, `offsite` | Storage-based naming |
| `fast`, `archive` | Retention-based naming |
| `prod`, `dr` | Purpose-based naming |

!!! tip "Keep Names Short"
    Names are included in backup paths. Keep them concise but descriptive.

## Storage Path Structure

With multiple configs, backups are organized by config name:

```
container-name/
├── hourly/
│   └── 2024-01-15/
│       ├── 100000.tar.zst
│       ├── 110000.tar.zst
│       └── 120000.tar.zst
├── daily/
│   └── 2024-01-15/
│       └── 030000.tar.zst
└── weekly/
    └── 2024-01-14/
        └── 030000.tar.zst
```

## Dashboard View

The dashboard shows all configurations for each container:

- Configuration name
- Backup type
- Schedule (cron expression)
- Next scheduled run
- Storage pool
- Retention policy

You can trigger individual configurations from the dashboard.

## CLI Operations

### Trigger All Configs

```bash
docker-backup backup run postgres
```

This triggers all backup configurations for the container.

### List Backups

```bash
docker-backup backup list postgres
```

Shows backups from all configurations:

```
KEY                                          SIZE      DATE
postgres/hourly/2024-01-15/120000.tar.zst   2.1 MB    2024-01-15 12:00:00
postgres/hourly/2024-01-15/110000.tar.zst   2.1 MB    2024-01-15 11:00:00
postgres/daily/2024-01-15/030000.tar.zst    2.0 MB    2024-01-15 03:00:00
postgres/weekly/2024-01-14/030000.tar.zst   1.9 MB    2024-01-14 03:00:00
```

## Complete Example

```yaml title="compose.yml"
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    command:
      - daemon
      # Fast local storage
      - --storage=local.type=local
      - --storage=local.path=/backups/local
      # S3 for offsite
      - --storage=s3.type=s3
      - --storage=s3.bucket=company-backups
      - --storage=s3.region=us-west-2
      - --storage=s3.access-key=${AWS_ACCESS_KEY_ID}
      - --storage=s3.secret-key=${AWS_SECRET_ACCESS_KEY}
      # Notifications
      - --notify=telegram.type=telegram
      - --notify=telegram.token=${TELEGRAM_TOKEN}
      - --notify=telegram.chat-id=${TELEGRAM_CHAT}
      - --default-storage=local
      - --dashboard=:8080
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - backup-local:/backups/local

  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: secret
    labels:
      - docker-backup.enable=true
      - docker-backup.notify=telegram

      # Hourly to local (for quick recovery)
      - docker-backup.hourly.type=postgres
      - docker-backup.hourly.schedule=0 * * * *
      - docker-backup.hourly.retention=24
      - docker-backup.hourly.storage=local

      # Daily to S3 (for disaster recovery)
      - docker-backup.daily.type=postgres
      - docker-backup.daily.schedule=0 2 * * *
      - docker-backup.daily.retention=30
      - docker-backup.daily.storage=s3

      # Weekly to S3 (for long-term archive)
      - docker-backup.weekly.type=postgres
      - docker-backup.weekly.schedule=0 3 * * 0
      - docker-backup.weekly.retention=52
      - docker-backup.weekly.storage=s3

volumes:
  backup-local:
```
