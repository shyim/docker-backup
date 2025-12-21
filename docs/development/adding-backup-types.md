---
icon: lucide/database
---

# Adding Backup Types

This guide walks you through creating a new backup type for docker-backup.

## Interface

Backup types implement the `BackupType` interface:

```go
type BackupType interface {
    // Name returns the backup type identifier used in labels
    Name() string

    // FileExtension returns the file extension for backup files
    FileExtension() string

    // Validate checks if the container has required configuration
    Validate(container *docker.ContainerInfo) error

    // Backup performs the backup, writing data to the writer
    Backup(ctx context.Context, container *docker.ContainerInfo,
           dockerClient *docker.Client, w io.Writer) error

    // Restore restores data from the reader to the container
    Restore(ctx context.Context, container *docker.ContainerInfo,
            dockerClient *docker.Client, r io.Reader) error
}
```

## Example: MySQL Backup Type

### Step 1: Create Package

Create `internal/backuptypes/mysql/mysql.go`:

```go
package mysql

import (
    "context"
    "fmt"
    "io"

    "github.com/shyim/docker-backup/internal/backup"
    "github.com/shyim/docker-backup/internal/docker"
)

func init() {
    backup.Register(&MySQLBackup{})
}

type MySQLBackup struct{}

func (m *MySQLBackup) Name() string {
    return "mysql"
}

func (m *MySQLBackup) FileExtension() string {
    return ".sql.gz"
}
```

### Step 2: Implement Validate

Check for required environment variables:

```go
const (
    EnvMySQLUser     = "MYSQL_USER"
    EnvMySQLPassword = "MYSQL_PASSWORD"
    EnvMySQLDatabase = "MYSQL_DATABASE"
    EnvMySQLRootPwd  = "MYSQL_ROOT_PASSWORD"
)

func (m *MySQLBackup) Validate(container *docker.ContainerInfo) error {
    env := container.Env

    // Check for root password or user credentials
    hasRoot := env[EnvMySQLRootPwd] != ""
    hasUser := env[EnvMySQLUser] != "" && env[EnvMySQLPassword] != ""

    if !hasRoot && !hasUser {
        return fmt.Errorf("container %s missing MySQL credentials", container.Name)
    }

    return nil
}
```

### Step 3: Implement Backup

Execute backup command inside container:

```go
func (m *MySQLBackup) Backup(ctx context.Context, container *docker.ContainerInfo,
                              dockerClient *docker.Client, w io.Writer) error {
    env := container.Env

    // Determine credentials
    user := "root"
    password := env[EnvMySQLRootPwd]
    if password == "" {
        user = env[EnvMySQLUser]
        password = env[EnvMySQLPassword]
    }

    // Build mysqldump command
    cmd := []string{
        "sh", "-c",
        fmt.Sprintf(
            "mysqldump -u%s -p%s --all-databases | gzip",
            user, password,
        ),
    }

    // Execute and stream output
    exitCode, err := dockerClient.ExecWithOutput(ctx, container.ID, cmd, w)
    if err != nil {
        return fmt.Errorf("mysqldump failed: %w", err)
    }

    if exitCode != 0 {
        return fmt.Errorf("mysqldump exited with code %d", exitCode)
    }

    return nil
}
```

### Step 4: Implement Restore

```go
func (m *MySQLBackup) Restore(ctx context.Context, container *docker.ContainerInfo,
                               dockerClient *docker.Client, r io.Reader) error {
    env := container.Env

    user := "root"
    password := env[EnvMySQLRootPwd]
    if password == "" {
        user = env[EnvMySQLUser]
        password = env[EnvMySQLPassword]
    }

    // Decompress and pipe to mysql
    cmd := []string{
        "sh", "-c",
        fmt.Sprintf("gunzip | mysql -u%s -p%s", user, password),
    }

    result, err := dockerClient.Exec(ctx, container.ID, cmd, r)
    if err != nil {
        return fmt.Errorf("restore failed: %w", err)
    }

    if result.ExitCode != 0 {
        return fmt.Errorf("mysql exited with code %d: %s", result.ExitCode, result.Output)
    }

    return nil
}
```

### Step 5: Register Plugin

Add import to `internal/backuptypes/registry.go`:

```go
package backuptypes

import (
    _ "github.com/shyim/docker-backup/internal/backuptypes/mysql"
    _ "github.com/shyim/docker-backup/internal/backuptypes/postgres"
)
```

## Docker Client Methods

The `docker.Client` provides these methods for executing commands:

### Exec

Execute a command with optional stdin, returns output:

```go
result, err := dockerClient.Exec(ctx, containerID, cmd, stdinReader)
// result.ExitCode, result.Output
```

### ExecWithOutput

Execute a command and stream stdout to a writer:

```go
exitCode, err := dockerClient.ExecWithOutput(ctx, containerID, cmd, writer)
```

## Container Information

The `docker.ContainerInfo` struct provides:

```go
type ContainerInfo struct {
    ID     string            // Container ID
    Name   string            // Container name
    Env    map[string]string // Environment variables
    Labels map[string]string // Container labels
}
```

## Best Practices

### Compression

Always compress backup data to reduce storage:

```go
// In Backup()
gzWriter := gzip.NewWriter(w)
defer gzWriter.Close()
// Write to gzWriter

// In Restore()
gzReader, _ := gzip.NewReader(r)
defer gzReader.Close()
// Read from gzReader
```

### Error Handling

Provide clear error messages:

```go
if exitCode != 0 {
    return fmt.Errorf("backup failed (exit %d): %s", exitCode, output)
}
```

### Validation

Check all requirements before backup:

```go
func (m *MySQLBackup) Validate(container *docker.ContainerInfo) error {
    if container.Env["MYSQL_ROOT_PASSWORD"] == "" {
        return fmt.Errorf("MYSQL_ROOT_PASSWORD required")
    }
    return nil
}
```

### Context Handling

Respect context cancellation:

```go
func (m *MySQLBackup) Backup(ctx context.Context, ...) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    // Continue with backup
}
```

## Testing

Create test file `internal/backuptypes/mysql/mysql_test.go`:

```go
package mysql

import (
    "testing"

    "github.com/shyim/docker-backup/internal/docker"
)

func TestValidate(t *testing.T) {
    backup := &MySQLBackup{}

    tests := []struct {
        name    string
        env     map[string]string
        wantErr bool
    }{
        {
            name:    "missing credentials",
            env:     map[string]string{},
            wantErr: true,
        },
        {
            name: "root password",
            env: map[string]string{
                "MYSQL_ROOT_PASSWORD": "secret",
            },
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            container := &docker.ContainerInfo{Env: tt.env}
            err := backup.Validate(container)
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Complete Example

See the PostgreSQL implementation at `internal/backuptypes/postgres/postgres.go` for a complete, production-ready example.
