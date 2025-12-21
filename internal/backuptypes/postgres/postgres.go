package postgres

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
	backup.Register(&PostgresBackup{})
}

// Environment variable names for PostgreSQL configuration
const (
	EnvPostgresUser     = "POSTGRES_USER"
	EnvPostgresPassword = "POSTGRES_PASSWORD"
	EnvPostgresDB       = "POSTGRES_DB"
	EnvPGHost           = "PGHOST"
	EnvPGPort           = "PGPORT"
	EnvPGDatabase       = "PGDATABASE"
	EnvPGUser           = "PGUSER"
	EnvPGPassword       = "PGPASSWORD"
)

// PostgresBackup implements BackupType for PostgreSQL databases
type PostgresBackup struct{}

// Name returns the backup type identifier
func (p *PostgresBackup) Name() string {
	return "postgres"
}

// FileExtension returns the file extension for this backup type
func (p *PostgresBackup) FileExtension() string {
	return ".tar.zst"
}

// Validate checks if the container has required PostgreSQL configuration
func (p *PostgresBackup) Validate(container *docker.ContainerInfo) error {
	// Check for user
	if _, ok := container.Env[EnvPostgresUser]; !ok {
		if _, ok := container.Env[EnvPGUser]; !ok {
			return fmt.Errorf("container %s is missing PostgreSQL user (set %s or %s)", container.Name, EnvPostgresUser, EnvPGUser)
		}
	}

	return nil
}

// Backup performs the PostgreSQL backup using pg_dumpall inside the container
func (p *PostgresBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	env := container.Env

	// Get user
	user := env[EnvPostgresUser]
	if user == "" {
		user = env[EnvPGUser]
	}

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
	databases, err := p.listDatabases(ctx, container, dockerClient, user)
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}

	// Backup each database
	for _, dbname := range databases {
		if err := p.backupDatabase(ctx, container, dockerClient, tarWriter, user, dbname); err != nil {
			return fmt.Errorf("failed to backup database %s: %w", dbname, err)
		}
	}

	return nil
}

// listDatabases returns a list of all databases in the PostgreSQL server
func (p *PostgresBackup) listDatabases(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, user string) ([]string, error) {
	cmd := []string{
		"psql",
		"-U", user,
		"-d", "postgres",
		"-t", "-A",
		"-c", "SELECT datname FROM pg_database WHERE datistemplate = false AND datname != 'postgres'",
	}

	result, err := dockerClient.Exec(ctx, container.ID, cmd, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("psql failed with exit code %d: %s", result.ExitCode, result.Output)
	}

	var databases []string
	for _, line := range strings.Split(strings.TrimSpace(result.Output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			databases = append(databases, line)
		}
	}

	return databases, nil
}

// backupDatabase backs up a single database to the tar archive
func (p *PostgresBackup) backupDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, tarWriter *tar.Writer, user, dbname string) error {
	// Build pg_dump command
	cmd := []string{
		"pg_dump",
		"-U", user,
		"-d", dbname,
		"--clean",
		"--if-exists",
		"--create",
	}

	// Create a temp file to stream the dump to (so we can get the size for tar header)
	tmpFile, err := os.CreateTemp("", "pgdump-*.sql")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Stream pg_dump output to temp file
	exitCode, err := dockerClient.ExecWithOutput(ctx, container.ID, cmd, tmpFile)
	if err != nil {
		return fmt.Errorf("failed to execute pg_dump: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("pg_dump failed with exit code %d", exitCode)
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

// Restore restores a PostgreSQL backup to the container
func (p *PostgresBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	// Create zstd reader
	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(zstdReader)

	// Get PostgreSQL credentials from container env
	env := container.Env

	user := env[EnvPostgresUser]
	if user == "" {
		user = env[EnvPGUser]
	}

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

		if err := p.restoreDatabase(ctx, container, dockerClient, tarReader, user, header.Size); err != nil {
			return fmt.Errorf("failed to restore database %s: %w", dbname, err)
		}
	}

	return nil
}

// restoreDatabase restores a single database from the tar archive
func (p *PostgresBackup) restoreDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader, user string, size int64) error {
	// Use psql to restore - connect to postgres database since the dump contains CREATE DATABASE
	cmd := []string{
		"psql",
		"-U", user,
		"-d", "postgres",
	}

	// Stream the SQL dump directly to psql using LimitReader to read exactly the tar entry size
	result, err := dockerClient.Exec(ctx, container.ID, cmd, io.LimitReader(r, size))
	if err != nil {
		return fmt.Errorf("failed to execute restore command: %w", err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("restore failed with exit code %d: %s", result.ExitCode, result.Output)
	}

	return nil
}
