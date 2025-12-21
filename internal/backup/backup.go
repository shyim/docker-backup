package backup

import (
	"context"
	"io"

	"github.com/shyim/docker-backup/internal/docker"
)

// BackupType defines the interface for different backup implementations.
type BackupType interface {
	Name() string
	FileExtension() string
	Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error
	Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error
	Validate(container *docker.ContainerInfo) error
}
