---
icon: lucide/hard-drive
---

# Storage Configuration

docker-backup supports multiple storage backends. You can configure multiple storage pools and reference them from container labels.

## Storage Pools

Storage is configured using named pools. Each pool has a type and type-specific options.

### CLI Configuration

```bash
docker-backup daemon \
  --storage=<pool-name>.<option>=<value> \
  --default-storage=<pool-name>
```

### Environment Variable Configuration

```bash
DOCKER_BACKUP_STORAGE_<POOL>_<OPTION>=value
DOCKER_BACKUP_DEFAULT_STORAGE=<pool-name>
```

Pool names are uppercase in environment variables. Options use underscores (converted to hyphens internally).

## Local Storage

Store backups on the local filesystem.

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --storage=local.type=local \
      --storage=local.path=/backups \
      --default-storage=local
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_STORAGE_LOCAL_TYPE=local
    DOCKER_BACKUP_STORAGE_LOCAL_PATH=/backups
    DOCKER_BACKUP_DEFAULT_STORAGE=local
    ```

=== "Docker Compose"

    ```yaml
    services:
      docker-backup:
        image: ghcr.io/shyim/docker-backup:latest
        command:
          - daemon
          - --storage=local.type=local
          - --storage=local.path=/backups
          - --default-storage=local
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock:ro
          - backup-data:/backups

    volumes:
      backup-data:
    ```

### Local Storage Options

| Option | Required | Description |
|--------|----------|-------------|
| `type` | Yes | Must be `local` |
| `path` | Yes | Directory path for backup storage |

## S3 Storage

Store backups in Amazon S3 or S3-compatible storage.

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --storage=s3.type=s3 \
      --storage=s3.bucket=my-backups \
      --storage=s3.region=us-east-1 \
      --storage=s3.access-key=AKIA... \
      --storage=s3.secret-key=secret... \
      --default-storage=s3
    ```

=== "Environment Variables"

    ```bash
    DOCKER_BACKUP_STORAGE_S3_TYPE=s3
    DOCKER_BACKUP_STORAGE_S3_BUCKET=my-backups
    DOCKER_BACKUP_STORAGE_S3_REGION=us-east-1
    DOCKER_BACKUP_STORAGE_S3_ACCESS_KEY=AKIA...
    DOCKER_BACKUP_STORAGE_S3_SECRET_KEY=secret...
    DOCKER_BACKUP_DEFAULT_STORAGE=s3
    ```

=== "Docker Compose"

    ```yaml
    services:
      docker-backup:
        image: ghcr.io/shyim/docker-backup:latest
        environment:
          DOCKER_BACKUP_STORAGE_S3_TYPE: s3
          DOCKER_BACKUP_STORAGE_S3_BUCKET: my-backups
          DOCKER_BACKUP_STORAGE_S3_REGION: us-east-1
          DOCKER_BACKUP_STORAGE_S3_ACCESS_KEY: ${AWS_ACCESS_KEY_ID}
          DOCKER_BACKUP_STORAGE_S3_SECRET_KEY: ${AWS_SECRET_ACCESS_KEY}
          DOCKER_BACKUP_DEFAULT_STORAGE: s3
    ```

### S3 Storage Options

| Option | Required | Default | Description |
|--------|----------|---------|-------------|
| `type` | Yes | - | Must be `s3` |
| `bucket` | Yes | - | S3 bucket name |
| `region` | Yes | - | AWS region |
| `access-key` | Yes | - | AWS access key ID |
| `secret-key` | Yes | - | AWS secret access key |
| `endpoint` | No | AWS default | Custom endpoint URL |
| `path-style` | No | `false` | Use path-style addressing |
| `prefix` | No | - | Key prefix for all backups |

## MinIO Storage

MinIO is an S3-compatible object storage. Use the S3 storage type with custom endpoint.

=== "CLI Flags"

    ```bash
    docker-backup daemon \
      --storage=minio.type=s3 \
      --storage=minio.bucket=backups \
      --storage=minio.endpoint=http://minio:9000 \
      --storage=minio.access-key=minioadmin \
      --storage=minio.secret-key=minioadmin \
      --storage=minio.path-style=true \
      --default-storage=minio
    ```

=== "Docker Compose"

    ```yaml
    services:
      docker-backup:
        image: ghcr.io/shyim/docker-backup:latest
        command:
          - daemon
          - --storage=minio.type=s3
          - --storage=minio.bucket=backups
          - --storage=minio.endpoint=http://minio:9000
          - --storage=minio.access-key=minioadmin
          - --storage=minio.secret-key=minioadmin
          - --storage=minio.path-style=true
          - --default-storage=minio

      minio:
        image: minio/minio
        command: server /data --console-address ":9001"
        environment:
          MINIO_ROOT_USER: minioadmin
          MINIO_ROOT_PASSWORD: minioadmin
        volumes:
          - minio-data:/data

    volumes:
      minio-data:
    ```

!!! note "Path-Style Addressing"
    Most S3-compatible storage (MinIO, DigitalOcean Spaces, etc.) requires `path-style=true`.

## Multiple Storage Pools

Configure multiple pools for different use cases:

```bash
docker-backup daemon \
  # Fast local storage for frequent backups
  --storage=local-fast.type=local \
  --storage=local-fast.path=/ssd/backups \
  # S3 for offsite backups
  --storage=s3-offsite.type=s3 \
  --storage=s3-offsite.bucket=company-backups \
  --storage=s3-offsite.region=us-west-2 \
  --storage=s3-offsite.access-key=AKIA... \
  --storage=s3-offsite.secret-key=secret... \
  # Default to local
  --default-storage=local-fast
```

Then reference pools in container labels:

```yaml
labels:
  - docker-backup.enable=true
  # Hourly to fast local
  - docker-backup.hourly.type=postgres
  - docker-backup.hourly.schedule=0 * * * *
  - docker-backup.hourly.storage=local-fast
  # Daily to S3
  - docker-backup.daily.type=postgres
  - docker-backup.daily.schedule=0 3 * * *
  - docker-backup.daily.storage=s3-offsite
```

## Backup Key Format

Backups are stored with the following key format:

```
<container-name>/<config-name>/<YYYY-MM-DD>/<HHMMSS>.<extension>
```

Example:

```
postgres/db/2024-01-15/030000.sql.gz
```
