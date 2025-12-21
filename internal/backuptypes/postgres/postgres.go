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

type PostgresBackup struct{}

func (p *PostgresBackup) Name() string {
	return "postgres"
}

func (p *PostgresBackup) FileExtension() string {
	return ".tar.zst"
}

func (p *PostgresBackup) Validate(container *docker.ContainerInfo) error {
	// Check for user
	if _, ok := container.Env[EnvPostgresUser]; !ok {
		if _, ok := container.Env[EnvPGUser]; !ok {
			return fmt.Errorf("container %s is missing PostgreSQL user (set %s or %s)", container.Name, EnvPostgresUser, EnvPGUser)
		}
	}

	return nil
}

func (p *PostgresBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	env := container.Env

	user := env[EnvPostgresUser]
	if user == "" {
		user = env[EnvPGUser]
	}

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

	databases, err := p.listDatabases(ctx, container, dockerClient, user)
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}

	for _, dbname := range databases {
		if err := p.backupDatabase(ctx, container, dockerClient, tarWriter, user, dbname); err != nil {
			return fmt.Errorf("failed to backup database %s: %w", dbname, err)
		}
	}

	return nil
}

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

func (p *PostgresBackup) backupDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, tarWriter *tar.Writer, user, dbname string) error {
	cmd := []string{
		"pg_dump",
		"-U", user,
		"-d", dbname,
		"--clean",
		"--if-exists",
		"--create",
	}

	tmpFile, err := os.CreateTemp("", "pgdump-*.sql")
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
		return fmt.Errorf("failed to execute pg_dump: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("pg_dump failed with exit code %d", exitCode)
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

func (p *PostgresBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)

	env := container.Env

	user := env[EnvPostgresUser]
	if user == "" {
		user = env[EnvPGUser]
	}

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

		if err := p.restoreDatabase(ctx, container, dockerClient, tarReader, user, header.Size); err != nil {
			return fmt.Errorf("failed to restore database %s: %w", dbname, err)
		}
	}

	return nil
}

func (p *PostgresBackup) restoreDatabase(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader, user string, size int64) error {
	cmd := []string{
		"psql",
		"-U", user,
		"-d", "postgres",
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
