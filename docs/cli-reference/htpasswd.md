---
icon: lucide/key
---

# htpasswd

Generate htpasswd-style bcrypt password hashes for dashboard authentication.

## Synopsis

```bash
docker-backup htpasswd <username> [flags]
```

## Description

The `htpasswd` command generates bcrypt password hashes compatible with the `--dashboard.auth.basic` flag. Use this to create credentials for securing the web dashboard.

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `username` | Yes | Username for the credential |

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--cost` | `-c` | `10` | bcrypt cost factor (4-31) |

### Cost Factor

Higher cost values make the hash more secure but slower to compute:

| Cost | Security | Speed |
|------|----------|-------|
| 4 | Minimum | Very fast |
| 10 | Default | Balanced |
| 12 | Good | Slower |
| 14+ | High | Very slow |

## Input Methods

### Interactive Mode

When run in a terminal, prompts for password with confirmation:

```bash
docker-backup htpasswd admin
```

Output:
```
Password:
Confirm password:
admin:$2a$10$xyz...
```

### Non-Interactive Mode

Pipe password from stdin for scripting:

```bash
echo "mypassword" | docker-backup htpasswd admin
```

Output:
```
admin:$2a$10$xyz...
```

## Examples

### Generate Single User

```bash
# Interactive
docker-backup htpasswd admin

# Output to file
docker-backup htpasswd admin > htpasswd
```

### Generate Multiple Users

```bash
# Create file with first user
docker-backup htpasswd admin > htpasswd

# Append additional users
docker-backup htpasswd readonly >> htpasswd
docker-backup htpasswd operator >> htpasswd
```

### Non-Interactive for Scripting

```bash
# From environment variable
echo "$ADMIN_PASSWORD" | docker-backup htpasswd admin > htpasswd

# From password file
cat password.txt | docker-backup htpasswd admin > htpasswd
```

### Higher Security (Increased Cost)

```bash
docker-backup htpasswd admin --cost 14
```

### Docker Usage

```bash
# Generate inside container
docker run --rm -it ghcr.io/shyim/docker-backup:latest htpasswd admin

# Pipe password
echo "mypassword" | docker run --rm -i ghcr.io/shyim/docker-backup:latest htpasswd admin
```

## Output Format

The output follows htpasswd format:

```
username:$2a$10$...hash...
```

Where:
- `$2a$` indicates bcrypt algorithm
- `10` is the cost factor
- The rest is the base64-encoded hash

## Using Generated Credentials

### Direct Flag

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.basic='admin:$2a$10$xyz...'
```

### File-Based

```bash
# Generate credentials
docker-backup htpasswd admin > /etc/docker-backup/htpasswd
docker-backup htpasswd readonly >> /etc/docker-backup/htpasswd

# Use file
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.basic=/etc/docker-backup/htpasswd
```

### Docker Compose

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    command:
      - daemon
      - --dashboard=:8080
      - --dashboard.auth.basic=/config/htpasswd
    volumes:
      - ./htpasswd:/config/htpasswd:ro
```

## Validation

### Username Restrictions

- Cannot contain `:` (colon) character
- Can contain letters, numbers, and most special characters

### Password Recommendations

- Minimum 8 characters
- Mix of uppercase, lowercase, numbers, and symbols
- Avoid common passwords

## See Also

- [Dashboard Authentication](../configuration/dashboard.md#authentication)
