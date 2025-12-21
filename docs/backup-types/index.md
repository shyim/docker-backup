---
icon: lucide/database
---

# Backup Types

docker-backup supports multiple backup types, each designed for specific applications. The backup type is specified using the `docker-backup.<name>.type` label.

## Supported Types

| Type | Description | Output |
|------|-------------|--------|
| `postgres` | PostgreSQL database backup | `.tar.zst` |
| `mysql` | MySQL/MariaDB database backup | `.tar.zst` |
| `volume` | Docker volume backup | `.tar.zst` |

## How Backup Types Work

Each backup type:

1. **Validates** the container configuration (required environment variables, etc.)
2. **Executes** the appropriate backup command inside the container
3. **Streams** the output to storage (with optional compression)
4. **Supports** restoration from backup files

## Choosing a Backup Type

Select the backup type that matches your application:

| Application | Backup Type |
|-------------|-------------|
| PostgreSQL | `postgres` |
| MySQL | `mysql` |
| MariaDB | `mysql` |
| Generic file data | `volume` |

## Backup Type Reference

<div class="grid cards" markdown>

-   :simple-postgresql: **PostgreSQL**

    ---

    Backup PostgreSQL databases using `pg_dump`

    [:octicons-arrow-right-24: PostgreSQL](postgres.md)

-   :simple-mysql: **MySQL / MariaDB**

    ---

    Backup MySQL and MariaDB databases using `mysqldump`

    [:octicons-arrow-right-24: MySQL](mysql.md)

-   :lucide-hard-drive: **Volume**

    ---

    Backup all mounted volumes from a container

    [:octicons-arrow-right-24: Volume](volume.md)

</div>
