package mysql

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/shyim/docker-backup/internal/backup"
	"github.com/shyim/docker-backup/internal/docker"
)

func init() {
	backup.Register(&MySQLBackup{})
}

// Environment variable names for MySQL configuration
const (
	EnvMySQLUser         = "MYSQL_USER"
	EnvMySQLPassword     = "MYSQL_PASSWORD"
	EnvMySQLRootPassword = "MYSQL_ROOT_PASSWORD"
	EnvMySQLDatabase     = "MYSQL_DATABASE"
)

type MySQLBackup struct{}

func (m *MySQLBackup) Name() string {
	return "mysql"
}

func (m *MySQLBackup) FileExtension() string {
	return ".tar.zst"
}

func (m *MySQLBackup) Validate(container *docker.ContainerInfo) error {
	// Check for password - either root password or user password
	if _, ok := container.Env[EnvMySQLRootPassword]; !ok {
		if _, ok := container.Env[EnvMySQLPassword]; !ok {
			return fmt.Errorf("container %s is missing MySQL password (set %s or %s)", container.Name, EnvMySQLRootPassword, EnvMySQLPassword)
		}
		// If using user password, we need a user
		if _, ok := container.Env[EnvMySQLUser]; !ok {
			return fmt.Errorf("container %s has %s but is missing %s", container.Name, EnvMySQLPassword, EnvMySQLUser)
		}
	}

	return nil
}

func (m *MySQLBackup) getCredentials(env map[string]string) (user, password string) {
	// Prefer root user if root password is set
	if rootPass, ok := env[EnvMySQLRootPassword]; ok {
		return "root", rootPass
	}

	// Fall back to regular user
	return env[EnvMySQLUser], env[EnvMySQLPassword]
}

func (m *MySQLBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	user, password := m.getCredentials(container.Env)

	zstdWriter, err := zstd.NewWriter(w)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer func() {
		_ = zstdWriter.Close()
	}()

	tarWriter := tar.NewWriter(zstdWriter)
	defer func() {
		_ = tarWriter.Close()
	}()

	databases, err := m.listDatabases(ctx, container, dockerClient, user, password)
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}

	for _, dbname := range databases {
		if err := m.backupDatabase(ctx, container, dockerClient, tarWriter, user, password, dbname); err != nil {
			return fmt.Errorf("failed to backup database %s: %w", dbname, err)
		}
	}

	return nil
}

// getMySQLCommand returns the appropriate mysql command for the container
// MariaDB 11+ uses 'mariadb' instead of 'mysql'
func (m *MySQLBackup) getMySQLCommand(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client) string {
	// Try mariadb first (MariaDB 11+)
	result, err := dockerClient.Exec(ctx, container.ID, []string{"which", "mariadb"}, nil)
	if err == nil && result.ExitCode == 0 {
		return "mariadb"
	}
	return "mysql"
}

func (m *MySQLBackup) getMySQLDumpCommand(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client) string {
	// Try mariadb-dump first (MariaDB 11+)
	result, err := dockerClient.Exec(ctx, container.ID, []string{"which", "mariadb-dump"}, nil)
	if err == nil && result.ExitCode == 0 {
		return "mariadb-dump"
	}
	return "mysqldump"
}

var systemDatabases = map[string]bool{
	"information_schema": true,
	"mysql":              true,
	"performance_schema": true,
	"sys":                true,
}

func (m *MySQLBackup) listDatabases(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, user, password string) ([]string, error) {
	mysqlCmd := m.getMySQLCommand(ctx, container, dockerClient)
	cmd := []string{
		mysqlCmd,
		"-u", user,
		"-p" + password,
		"-N", "-e",
		"SELECT schema_name FROM information_schema.schemata",
	}

	result, err := dockerClient.Exec(ctx, container.ID, cmd, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("mysql failed with exit code %d: %s", result.ExitCode, result.Output)
	}

	var databases []string
	for _, line := range strings.Split(strings.TrimSpace(result.Output), "\n") {
		line = strings.TrimSpace(line)
		// Skip empty lines, system databases, and warning messages
		if line == "" || systemDatabases[line] || strings.HasPrefix(line, "[") || strings.Contains(line, "Warning") {
			continue
		}
		databases = append(databases, line)
	}

	return databases, nil
}

func (m *MySQLBackup) backupDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, tarWriter *tar.Writer, user, password, dbname string) error {
	mysqldumpCmd := m.getMySQLDumpCommand(ctx, container, dockerClient)
	cmd := []string{
		mysqldumpCmd,
		"-u", user,
		"-p" + password,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--events",
		"--add-drop-database",
		"--databases", dbname,
	}

	tmpFile, err := os.CreateTemp("", "mysqldump-*.sql")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()
	defer func() {
		_ = tmpFile.Close()
	}()

	exitCode, err := dockerClient.ExecWithOutput(ctx, container.ID, cmd, tmpFile)
	if err != nil {
		return fmt.Errorf("failed to execute mysqldump: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("mysqldump failed with exit code %d", exitCode)
	}

	fileInfo, err := tmpFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temp file: %w", err)
	}

	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek temp file: %w", err)
	}

	header := &tar.Header{
		Name: dbname + ".sql",
		Mode: 0644,
		Size: fileInfo.Size(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := io.Copy(tarWriter, tmpFile); err != nil {
		return fmt.Errorf("failed to write to tar: %w", err)
	}

	return nil
}

func (m *MySQLBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)

	user, password := m.getCredentials(container.Env)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		dbname := strings.TrimSuffix(header.Name, ".sql")

		if err := m.restoreDatabase(ctx, container, dockerClient, tarReader, user, password, header.Size); err != nil {
			return fmt.Errorf("failed to restore database %s: %w", dbname, err)
		}
	}

	return nil
}

func (m *MySQLBackup) restoreDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader, user, password string, size int64) error {
	mysqlCmd := m.getMySQLCommand(ctx, container, dockerClient)
	cmd := []string{
		mysqlCmd,
		"-u", user,
		"-p" + password,
	}

	result, err := dockerClient.Exec(ctx, container.ID, cmd, io.LimitReader(r, size))
	if err != nil {
		return fmt.Errorf("failed to execute restore command: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("restore failed with exit code %d: %s", result.ExitCode, result.Output)
	}

	return nil
}
