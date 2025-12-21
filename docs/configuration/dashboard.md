---
icon: lucide/layout-dashboard
---

# Dashboard

docker-backup includes a web dashboard for monitoring and managing backups.

## Enabling the Dashboard

Enable the dashboard with the `--dashboard` flag:

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --storage=local.type=local \
  --storage=local.path=/backups
```

The dashboard will be available at `http://localhost:8080`.

### Docker Compose

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    command:
      - daemon
      - --dashboard=:8080
      - --storage=local.type=local
      - --storage=local.path=/backups
    ports:
      - "8080:8080"
```

## Dashboard Features

### Overview

The main dashboard shows:

- Total containers with backups enabled
- Number of scheduled backup jobs
- Configured storage pools
- Configured notification providers

### Containers

View all monitored containers with their backup configurations:

- Container name and ID
- Backup configurations (type, schedule, retention, storage)
- Next scheduled run time
- Quick actions (trigger backup)

### Backups

For each container, view and manage backups:

- List all backups with size and date
- Download backups
- Delete backups
- Restore backups

### Notifications

View configured notification providers.

## Authentication

!!! warning "Security"
    The dashboard has full access to trigger, download, and restore backups. Always enable authentication in production.

### HTTP Basic Authentication

Secure the dashboard with HTTP Basic Authentication using htpasswd-style credentials.

#### Generate Password Hash

Use the built-in `htpasswd` command to generate a bcrypt password hash:

```bash
# Interactive (prompts for password)
docker-backup htpasswd admin
# Output: admin:$2a$10$...

# Non-interactive (pipe password)
echo "mypassword" | docker-backup htpasswd admin
```

#### Using an htpasswd File

Create a file with one or more users:

```bash
# Generate first user
docker-backup htpasswd admin > /etc/docker-backup/htpasswd

# Add additional users
docker-backup htpasswd readonly >> /etc/docker-backup/htpasswd
```

Configure the daemon:

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.basic=/etc/docker-backup/htpasswd
```

#### Inline Credentials

For single-user setups, pass credentials directly:

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.basic='admin:$2a$10$...'
```

#### Docker Compose with Authentication

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    command:
      - daemon
      - --dashboard=:8080
      - --dashboard.auth.basic=/config/htpasswd
      - --storage=local.type=local
      - --storage=local.path=/backups
    volumes:
      - ./htpasswd:/config/htpasswd:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - backup-data:/backups
```

Or use environment variable:

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    environment:
      # Pre-generated hash for password "admin123"
      DASHBOARD_AUTH: 'admin:$$2a$$10$$xyz...'
    command:
      - daemon
      - --dashboard=:8080
      - --dashboard.auth.basic=${DASHBOARD_AUTH}
```

!!! note "Docker Compose Escaping"
    In Docker Compose files, escape `$` as `$$` in environment variables.

### Supported Password Formats

The dashboard authentication supports these htpasswd hash formats:

| Format | Prefix | Recommended |
|--------|--------|-------------|
| bcrypt | `$2y$`, `$2a$`, `$2b$` | Yes |
| SHA1 | `{SHA}` | No |
| Plain text | (none) | No |

Always use bcrypt for new passwords.

### OIDC Authentication

Secure the dashboard with OpenID Connect (OIDC) authentication using Google, GitHub, or any generic OIDC provider.

#### Supported Providers

| Provider | Value | Notes |
|----------|-------|-------|
| Google | `google` | Standard OIDC |
| GitHub | `github` | OAuth2 (fetches email from API) |
| Generic OIDC | `oidc` | Requires `--dashboard.auth.oidc.issuer-url` |

#### Google Authentication

1. Create OAuth credentials in the [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Add `http://localhost:8080/auth/callback` as an authorized redirect URI
3. Configure the daemon:

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.oidc.provider=google \
  --dashboard.auth.oidc.client-id=YOUR_CLIENT_ID.apps.googleusercontent.com \
  --dashboard.auth.oidc.client-secret=YOUR_CLIENT_SECRET \
  --dashboard.auth.oidc.redirect-url=http://localhost:8080/auth/callback \
  --dashboard.auth.oidc.allowed-domains=example.com
```

#### GitHub Authentication

1. Create an OAuth App in [GitHub Developer Settings](https://github.com/settings/developers)
2. Set the callback URL to `http://localhost:8080/auth/callback`
3. Configure the daemon:

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.oidc.provider=github \
  --dashboard.auth.oidc.client-id=YOUR_CLIENT_ID \
  --dashboard.auth.oidc.client-secret=YOUR_CLIENT_SECRET \
  --dashboard.auth.oidc.redirect-url=http://localhost:8080/auth/callback \
  --dashboard.auth.oidc.allowed-users=admin@example.com,user@example.com
```

#### Generic OIDC Provider (Keycloak, Authentik, etc.)

```bash
docker-backup daemon \
  --dashboard=:8080 \
  --dashboard.auth.oidc.provider=oidc \
  --dashboard.auth.oidc.issuer-url=https://keycloak.example.com/realms/myrealm \
  --dashboard.auth.oidc.client-id=docker-backup \
  --dashboard.auth.oidc.client-secret=YOUR_CLIENT_SECRET \
  --dashboard.auth.oidc.redirect-url=http://localhost:8080/auth/callback
```

#### OIDC Configuration Options

| Flag | Description |
|------|-------------|
| `--dashboard.auth.oidc.provider` | Provider type: `google`, `github`, or `oidc` |
| `--dashboard.auth.oidc.issuer-url` | OIDC issuer URL (required for generic `oidc` provider) |
| `--dashboard.auth.oidc.client-id` | OAuth client ID |
| `--dashboard.auth.oidc.client-secret` | OAuth client secret |
| `--dashboard.auth.oidc.redirect-url` | OAuth callback URL |
| `--dashboard.auth.oidc.allowed-users` | Comma-separated list of allowed email addresses |
| `--dashboard.auth.oidc.allowed-domains` | Comma-separated list of allowed email domains |

#### Access Control

Control who can access the dashboard with `--dashboard.auth.oidc.allowed-users` and `--dashboard.auth.oidc.allowed-domains`:

```bash
# Allow specific users
--dashboard.auth.oidc.allowed-users=admin@example.com,ops@example.com

# Allow entire domains
--dashboard.auth.oidc.allowed-domains=example.com,acme.org

# Combine both (user must match either)
--dashboard.auth.oidc.allowed-users=external@gmail.com \
--dashboard.auth.oidc.allowed-domains=example.com
```

If neither option is specified, all authenticated users are allowed.

#### Docker Compose with OIDC

```yaml
services:
  docker-backup:
    image: ghcr.io/shyim/docker-backup:latest
    environment:
      DOCKER_BACKUP_DASHBOARD_OIDC_PROVIDER: google
      DOCKER_BACKUP_DASHBOARD_OIDC_CLIENT_ID: ${GOOGLE_CLIENT_ID}
      DOCKER_BACKUP_DASHBOARD_OIDC_CLIENT_SECRET: ${GOOGLE_CLIENT_SECRET}
      DOCKER_BACKUP_DASHBOARD_OIDC_REDIRECT_URL: https://backup.example.com/auth/callback
      DOCKER_BACKUP_DASHBOARD_OIDC_ALLOWED_DOMAINS: example.com
    command:
      - daemon
      - --dashboard=:8080
      - --storage=local.type=local
      - --storage=local.path=/backups
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - backup-data:/backups
```

!!! note "HTTPS Required"
    For production deployments, always use HTTPS for the redirect URL. Most OIDC providers require HTTPS for security.

## Dark Mode

The dashboard automatically uses dark mode based on your operating system preference (`prefers-color-scheme`). No configuration is needed.

## Reverse Proxy

When running behind a reverse proxy, ensure you forward the correct headers:

### Nginx

```nginx
location /backup/ {
    proxy_pass http://docker-backup:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

### Traefik

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.backup.rule=Host(`backup.example.com`)
  - traefik.http.services.backup.loadbalancer.server.port=8080
```
