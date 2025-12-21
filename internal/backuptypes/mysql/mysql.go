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

// MySQLBackup implements BackupType for MySQL/MariaDB databases
type MySQLBackup struct{}

// Name returns the backup type identifier
func (m *MySQLBackup) Name() string {
	return "mysql"
}

// FileExtension returns the file extension for this backup type
func (m *MySQLBackup) FileExtension() string {
	return ".tar.zst"
}

// Validate checks if the container has required MySQL configuration
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

// getCredentials returns the user and password to use for MySQL commands
func (m *MySQLBackup) getCredentials(env map[string]string) (user, password string) {
	// Prefer root user if root password is set
	if rootPass, ok := env[EnvMySQLRootPassword]; ok {
		return "root", rootPass
	}

	// Fall back to regular user
	return env[EnvMySQLUser], env[EnvMySQLPassword]
}

// Backup performs the MySQL backup using mysqldump inside the container
func (m *MySQLBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	user, password := m.getCredentials(container.Env)

	// Create zstd writer
	zstdWriter, err := zstd.NewWriter(w)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer zstdWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(zstdWriter)
	defer tarWriter.Close()

	// Get list of databases
	databases, err := m.listDatabases(ctx, container, dockerClient, user, password)
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}

	// Backup each database
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

// getMySQLDumpCommand returns the appropriate mysqldump command for the container
func (m *MySQLBackup) getMySQLDumpCommand(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client) string {
	// Try mariadb-dump first (MariaDB 11+)
	result, err := dockerClient.Exec(ctx, container.ID, []string{"which", "mariadb-dump"}, nil)
	if err == nil && result.ExitCode == 0 {
		return "mariadb-dump"
	}
	return "mysqldump"
}

// System databases that should be excluded from backup
var systemDatabases = map[string]bool{
	"information_schema": true,
	"mysql":              true,
	"performance_schema": true,
	"sys":                true,
}

// listDatabases returns a list of all user databases in the MySQL server
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

// backupDatabase backs up a single database to the tar archive
func (m *MySQLBackup) backupDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, tarWriter *tar.Writer, user, password, dbname string) error {
	mysqldumpCmd := m.getMySQLDumpCommand(ctx, container, dockerClient)
	// Build mysqldump command
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

	// Create a temp file to stream the dump to (so we can get the size for tar header)
	tmpFile, err := os.CreateTemp("", "mysqldump-*.sql")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Stream mysqldump output to temp file
	exitCode, err := dockerClient.ExecWithOutput(ctx, container.ID, cmd, tmpFile)
	if err != nil {
		return fmt.Errorf("failed to execute mysqldump: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("mysqldump failed with exit code %d", exitCode)
	}

	// Get file size
	fileInfo, err := tmpFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temp file: %w", err)
	}

	// Seek back to beginning for reading
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek temp file: %w", err)
	}

	// Write tar header
	header := &tar.Header{
		Name: dbname + ".sql",
		Mode: 0644,
		Size: fileInfo.Size(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	// Stream temp file to tar
	if _, err := io.Copy(tarWriter, tmpFile); err != nil {
		return fmt.Errorf("failed to write to tar: %w", err)
	}

	return nil
}

// Restore restores a MySQL backup to the container
func (m *MySQLBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	// Create zstd reader
	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(zstdReader)

	user, password := m.getCredentials(container.Env)

	// Restore each database from the tar archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Skip non-regular files
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Extract database name from filename (remove .sql extension)
		dbname := strings.TrimSuffix(header.Name, ".sql")

		if err := m.restoreDatabase(ctx, container, dockerClient, tarReader, user, password, header.Size); err != nil {
			return fmt.Errorf("failed to restore database %s: %w", dbname, err)
		}
	}

	return nil
}

// restoreDatabase restores a single database from the tar archive
func (m *MySQLBackup) restoreDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader, user, password string, size int64) error {
	mysqlCmd := m.getMySQLCommand(ctx, container, dockerClient)
	// Use mysql client to restore
	cmd := []string{
		mysqlCmd,
		"-u", user,
		"-p" + password,
	}

	// Stream the SQL dump directly to mysql using LimitReader to read exactly the tar entry size
	result, err := dockerClient.Exec(ctx, container.ID, cmd, io.LimitReader(r, size))
	if err != nil {
		return fmt.Errorf("failed to execute restore command: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("restore failed with exit code %d: %s", result.ExitCode, result.Output)
	}

	return nil
}
