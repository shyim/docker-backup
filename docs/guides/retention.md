---
icon: lucide/trash-2
---

# Retention Policies

docker-backup automatically manages backup retention, keeping your storage clean while ensuring you have the backups you need.

## How Retention Works

After each successful backup, docker-backup:

1. Lists all backups for the container/config combination
2. Sorts by date (newest first)
3. Keeps the newest N backups (where N is the retention value)
4. Deletes older backups

## Configuring Retention

Set retention per backup configuration using the `retention` label:

```yaml
labels:
  - docker-backup.enable=true
  - docker-backup.db.type=postgres
  - docker-backup.db.schedule=0 3 * * *
  - docker-backup.db.retention=7  # Keep last 7 backups
```

### Default Value

If not specified, retention defaults to `7`.

## Retention Strategies

### Based on Schedule

Match retention to your backup schedule:

| Schedule | Retention | Coverage |
|----------|-----------|----------|
| Hourly | 24 | 1 day |
| Every 6 hours | 28 | 1 week |
| Daily | 7 | 1 week |
| Daily | 30 | 1 month |
| Weekly | 4 | 1 month |
| Weekly | 52 | 1 year |

### Example: 30-Day Rolling Window

```yaml
labels:
  - docker-backup.db.schedule=0 3 * * *  # Daily
  - docker-backup.db.retention=30         # Keep 30 days
```

### Example: 1-Week Rolling Window

```yaml
labels:
  - docker-backup.db.schedule=0 * * * *  # Hourly
  - docker-backup.db.retention=168        # 24 * 7 = 1 week
```

## Multi-Tier Retention

Use multiple backup configurations for different retention tiers:

```yaml
labels:
  - docker-backup.enable=true

  # Tier 1: Frequent, short retention
  - docker-backup.frequent.type=postgres
  - docker-backup.frequent.schedule=*/15 * * * *
  - docker-backup.frequent.retention=48  # 12 hours

  # Tier 2: Daily, medium retention
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 2 * * *
  - docker-backup.daily.retention=30  # 1 month

  # Tier 3: Weekly, long retention
  - docker-backup.weekly.type=postgres
  - docker-backup.weekly.schedule=0 3 * * 0
  - docker-backup.weekly.retention=52  # 1 year
```

This gives you:

- **12 hours** of 15-minute recovery points
- **30 days** of daily recovery points
- **1 year** of weekly recovery points

## Storage Considerations

### Calculating Storage Needs

```
Storage = Backup Size × Retention Count
```

Example:
- Daily PostgreSQL backup: 500 MB
- Retention: 30
- Total storage: 500 MB × 30 = 15 GB

### Per-Storage Retention

Different storage backends can have different retention policies:

```yaml
labels:
  - docker-backup.enable=true

  # Local storage: keep fewer (expensive fast storage)
  - docker-backup.local.type=postgres
  - docker-backup.local.schedule=0 * * * *
  - docker-backup.local.retention=12
  - docker-backup.local.storage=local-ssd

  # S3: keep more (cheap archive storage)
  - docker-backup.s3.type=postgres
  - docker-backup.s3.schedule=0 3 * * *
  - docker-backup.s3.retention=365
  - docker-backup.s3.storage=s3-archive
```

## Retention Scope

Retention is applied per:

- **Container** - Each container's backups are managed independently
- **Configuration** - Each named config has its own retention count

Example with container `postgres` and configs `hourly` and `daily`:

```
postgres/
├── hourly/  → Retention applied here (keeps 24)
│   └── ...
└── daily/   → Retention applied here (keeps 30)
    └── ...
```

## Manual Cleanup

### Delete Specific Backup

```bash
docker-backup backup delete postgres "postgres/daily/2024-01-01/030000.tar.zst"
```

### List Before Cleanup

```bash
docker-backup backup list postgres
```

## Retention Timing

Retention cleanup runs:

1. **After each successful backup** - Old backups are pruned immediately
2. **Synchronously** - The backup job waits for cleanup to complete

This ensures retention is always enforced after new backups are created.

## Best Practices

### Match Retention to Recovery Needs

Consider your recovery requirements:

- How far back might you need to restore?
- What's your Recovery Point Objective (RPO)?
- Do you need compliance/audit backups?

### Balance Cost and Coverage

```yaml
# Cost-effective: fewer frequent, more infrequent
- docker-backup.hourly.retention=12    # 12 hours
- docker-backup.daily.retention=14     # 2 weeks
- docker-backup.weekly.retention=12    # 3 months
- docker-backup.monthly.retention=12   # 1 year
```

### Test Restore Regularly

High retention is useless if backups can't be restored. Regularly test:

```bash
# Create test container
docker run -d --name postgres-test postgres:16

# Restore backup
docker-backup backup restore postgres-test "postgres/daily/2024-01-15/030000.tar.zst"

# Verify data
docker exec postgres-test psql -U myuser -c "SELECT count(*) FROM mytable;"

# Cleanup
docker rm -f postgres-test
```

## Troubleshooting

### Backups Not Being Deleted

Check:

1. Retention value is set correctly
2. Backups completed successfully (failed backups don't trigger cleanup)
3. Storage permissions allow deletion

### Running Out of Storage

If storage fills up:

1. Lower retention values
2. Trigger manual cleanup
3. Add more storage capacity
4. Move older backups to cheaper storage tier
