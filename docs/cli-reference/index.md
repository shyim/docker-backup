---
icon: lucide/terminal
---

# CLI Reference

docker-backup provides a command-line interface for managing backups and running the daemon.

## Global Flags

These flags apply to all commands:

| Flag | Default | Description |
|------|---------|-------------|
| `--docker-host` | `unix:///var/run/docker.sock` | Docker daemon socket |
| `--socket` | `/var/run/docker-backup.sock` | Unix socket for daemon communication |
| `--log-level` | `info` | Log level: debug, info, warn, error |
| `--log-format` | `text` | Log format: text, json |

## Commands

### daemon

Start the backup daemon. See [daemon](daemon.md) for full documentation.

```bash
docker-backup daemon [flags]
```

### backup

Backup management commands. See [backup](backup.md) for full documentation.

```bash
docker-backup backup <subcommand> [flags]
```

Subcommands:

- `run <container>` - Trigger immediate backup
- `list <container>` - List backups for a container
- `delete <container> <key>` - Delete a backup
- `restore <container> <key>` - Restore a backup

### htpasswd

Generate htpasswd-style password hashes. See [htpasswd](htpasswd.md) for full documentation.

```bash
docker-backup htpasswd <username> [flags]
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |

## Command Reference

<div class="grid cards" markdown>

-   :lucide-server: **daemon**

    ---

    Start the backup daemon

    [:octicons-arrow-right-24: daemon](daemon.md)

-   :lucide-archive: **backup**

    ---

    Manage backups (run, list, delete, restore)

    [:octicons-arrow-right-24: backup](backup.md)

-   :lucide-key: **htpasswd**

    ---

    Generate password hashes

    [:octicons-arrow-right-24: htpasswd](htpasswd.md)

</div>
