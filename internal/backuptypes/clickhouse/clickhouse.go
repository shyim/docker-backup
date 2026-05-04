package clickhouse

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/shyim/docker-backup/internal/backup"
	"github.com/shyim/docker-backup/internal/docker"
)

func init() {
	backup.Register(&ClickHouseBackup{})
}

// Environment variable names for ClickHouse configuration
const (
	EnvClickHouseUser     = "CLICKHOUSE_USER"
	EnvClickHousePassword = "CLICKHOUSE_PASSWORD"
	EnvClickHouseDB       = "CLICKHOUSE_DB"

	// Minimum supported version for native BACKUP/RESTORE
	minMajorVersion = 22
	minMinorVersion = 8

	// Temp directory inside the container for backup staging
	backupTmpDir = "/tmp/docker-backup"
)

// System databases to exclude when auto-discovering databases
var systemDatabases = map[string]bool{
	"system":             true,
	"INFORMATION_SCHEMA": true,
	"information_schema": true,
	"default":            true,
}

type ClickHouseBackup struct{}

func (c *ClickHouseBackup) Name() string {
	return "clickhouse"
}

func (c *ClickHouseBackup) FileExtension() string {
	return ".tar.zst"
}

func (c *ClickHouseBackup) Validate(container *docker.ContainerInfo) error {
	// No env vars required — ClickHouse works with defaults (user=default, no password).
	// Version and clickhouse-client checks run at the start of Backup/Restore
	// where the docker client is available.
	return nil
}

func (c *ClickHouseBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	if err := c.checkVersion(ctx, container, dockerClient); err != nil {
		return err
	}

	user, password := c.getCredentials(container.Env)
	backupID := uuid.New().String()
	backupPath := backupTmpDir + "/" + backupID

	if err := c.ensureBackupPathAllowed(ctx, container, dockerClient); err != nil {
		return fmt.Errorf("failed to configure backup path: %w", err)
	}

	_, _ = dockerClient.Exec(ctx, container.ID, []string{"rm", "-rf", backupTmpDir}, nil)

	defer func() {
		_, _ = dockerClient.Exec(ctx, container.ID, []string{"rm", "-rf", backupPath}, nil)
	}()

	databases, err := c.getDatabases(ctx, container, dockerClient, user, password)
	if err != nil {
		return fmt.Errorf("failed to discover databases: %w", err)
	}

	if len(databases) == 0 {
		return fmt.Errorf("no databases found to backup in container %s", container.Name)
	}

	var dbClauses []string
	for _, db := range databases {
		dbClauses = append(dbClauses, "DATABASE "+db)
	}

	query := fmt.Sprintf("BACKUP %s TO File('%s/')", strings.Join(dbClauses, ", "), backupPath)
	if err := c.execQuery(ctx, container, dockerClient, user, password, query); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	zstdWriter, err := zstd.NewWriter(w)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer func() {
		_ = zstdWriter.Close()
	}()

	exitCode, err := dockerClient.ExecWithOutput(ctx, container.ID,
		[]string{"tar", "-c", "-C", backupTmpDir, backupID},
		zstdWriter,
	)
	if err != nil {
		return fmt.Errorf("failed to stream backup: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("tar failed with exit code %d", exitCode)
	}

	return nil
}

func (c *ClickHouseBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	if err := c.checkVersion(ctx, container, dockerClient); err != nil {
		return err
	}

	user, password := c.getCredentials(container.Env)
	restoreID := uuid.New().String()
	restorePath := backupTmpDir + "/" + restoreID

	defer func() {
		_, _ = dockerClient.Exec(ctx, container.ID, []string{"rm", "-rf", restorePath}, nil)
	}()

	result, err := dockerClient.Exec(ctx, container.ID, []string{"mkdir", "-p", backupTmpDir}, nil)
	if err != nil || result.ExitCode != 0 {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	result, err = dockerClient.Exec(ctx, container.ID,
		[]string{"tar", "-x", "-C", backupTmpDir},
		zstdReader,
	)
	if err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("tar extract failed with exit code %d: %s", result.ExitCode, result.Output)
	}

	result, err = dockerClient.Exec(ctx, container.ID, []string{"ls", backupTmpDir}, nil)
	if err != nil {
		return fmt.Errorf("failed to list backup directory: %w", err)
	}

	backupSubdir := strings.TrimSpace(result.Output)
	if backupSubdir == "" {
		return fmt.Errorf("backup archive is empty")
	}

	backupSubdir = strings.Split(backupSubdir, "\n")[0]
	backupSubdir = strings.TrimSpace(backupSubdir)

	fullBackupPath := backupTmpDir + "/" + backupSubdir

	query := fmt.Sprintf("RESTORE ALL FROM File('%s/') SETTINGS allow_non_empty_tables=true", fullBackupPath)
	if err := c.execQuery(ctx, container, dockerClient, user, password, query); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	return nil
}

func (c *ClickHouseBackup) getCredentials(env map[string]string) (user, password string) {
	user = env[EnvClickHouseUser]
	if user == "" {
		user = "default"
	}

	password = env[EnvClickHousePassword]

	return user, password
}

func (c *ClickHouseBackup) getDatabases(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, user, password string) ([]string, error) {
	if db, ok := container.Env[EnvClickHouseDB]; ok && db != "" {
		return []string{db}, nil
	}

	query := "SELECT name FROM system.databases FORMAT TabSeparated"
	result, err := c.execQueryWithOutput(ctx, container, dockerClient, user, password, query)
	if err != nil {
		return nil, err
	}

	var databases []string
	for _, line := range strings.Split(strings.TrimSpace(result), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || systemDatabases[line] {
			continue
		}
		databases = append(databases, line)
	}

	return databases, nil
}

func (c *ClickHouseBackup) checkVersion(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client) error {
	result, err := dockerClient.Exec(ctx, container.ID, []string{"clickhouse-client", "--version"}, nil)
	if err != nil {
		return fmt.Errorf("clickhouse-client not found in container %s: %w", container.Name, err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("clickhouse-client not available in container %s: %s", container.Name, result.Output)
	}

	major, minor, err := parseVersion(result.Output)
	if err != nil {
		return fmt.Errorf("failed to parse ClickHouse version: %w", err)
	}

	if major < minMajorVersion || (major == minMajorVersion && minor < minMinorVersion) {
		return fmt.Errorf("ClickHouse %d.%d is too old; native BACKUP requires version %d.%d or later", major, minor, minMajorVersion, minMinorVersion)
	}

	return nil
}

// parseVersion extracts major.minor from output like "ClickHouse client version 24.3.1.2672 (official build)."
func parseVersion(output string) (major, minor int, err error) {
	output = strings.TrimSpace(output)

	idx := strings.Index(strings.ToLower(output), "version")
	if idx == -1 {
		return 0, 0, fmt.Errorf("unexpected version output: %s", output)
	}

	versionPart := strings.TrimSpace(output[idx+len("version"):])
	if spaceIdx := strings.IndexByte(versionPart, ' '); spaceIdx != -1 {
		versionPart = versionPart[:spaceIdx]
	}

	parts := strings.SplitN(versionPart, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected version format: %s", versionPart)
	}

	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse major version %q: %w", parts[0], err)
	}

	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse minor version %q: %w", parts[1], err)
	}

	return major, minor, nil
}

func (c *ClickHouseBackup) execQuery(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, user, password, query string) error {
	cmd := c.buildClientCmd(user, password, query)

	result, err := dockerClient.Exec(ctx, container.ID, cmd, nil)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("clickhouse-client failed (exit %d): %s", result.ExitCode, result.Output)
	}

	return nil
}

func (c *ClickHouseBackup) execQueryWithOutput(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, user, password, query string) (string, error) {
	cmd := c.buildClientCmd(user, password, query)

	result, err := dockerClient.Exec(ctx, container.ID, cmd, nil)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
	}

	if result.ExitCode != 0 {
		return "", fmt.Errorf("clickhouse-client failed (exit %d): %s", result.ExitCode, result.Output)
	}

	return result.Output, nil
}

// ensureBackupPathAllowed writes a ClickHouse config snippet that allows /tmp/docker-backup/ as a backup path.
// Without this, BACKUP TO File('/tmp/docker-backup/...') fails with BAD_ARGUMENTS.
func (c *ClickHouseBackup) ensureBackupPathAllowed(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client) error {
	configXML := `<clickhouse><backups><allowed_path>/tmp/docker-backup/</allowed_path></backups></clickhouse>`
	configPath := "/etc/clickhouse-server/config.d/docker_backup_allowed_path.xml"

	result, err := dockerClient.Exec(ctx, container.ID,
		[]string{"sh", "-c", fmt.Sprintf("echo '%s' > %s", configXML, configPath)},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to write backup config: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("failed to write backup config: %s", result.Output)
	}

	_, _ = dockerClient.Exec(ctx, container.ID,
		[]string{"clickhouse-client", "--query", "SYSTEM RELOAD CONFIG"},
		nil,
	)

	return nil
}

func (c *ClickHouseBackup) buildClientCmd(user, password, query string) []string {
	cmd := []string{
		"clickhouse-client",
		"--user", user,
		"--receive_timeout", "3600",
		"--send_timeout", "3600",
	}

	if password != "" {
		cmd = append(cmd, "--password", password)
	}

	cmd = append(cmd, "--query", query)

	return cmd
}
