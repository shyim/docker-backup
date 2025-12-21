package volume

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/shyim/docker-backup/internal/backup"
	"github.com/shyim/docker-backup/internal/docker"
)

func init() {
	backup.Register(&VolumeBackup{})
}

// VolumeBackup implements BackupType for Docker volume backups
type VolumeBackup struct{}

// Name returns the backup type identifier
func (v *VolumeBackup) Name() string {
	return "volume"
}

// FileExtension returns the file extension for this backup type
func (v *VolumeBackup) FileExtension() string {
	return ".tar.zst"
}

// Validate checks if the container has required volume configuration
func (v *VolumeBackup) Validate(container *docker.ContainerInfo) error {
	// Volume backups work with any container that has mounted volumes
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}
	return nil
}

// Backup performs the volume backup by creating a tar archive of all mounted volumes
func (v *VolumeBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}

	// Stop container before backup
	wasRunning := container.Running
	if wasRunning {
		slog.Debug("stopping container for volume backup", "container", container.Name)
		if err := dockerClient.StopContainer(ctx, container.ID, 30*time.Second); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		defer func() {
			if err := dockerClient.StartContainer(ctx, container.ID); err != nil {
				slog.Warn("failed to restart container after backup",
					"container", container.Name,
					"error", err,
				)
			}
		}()
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

	// Backup each volume mount
	for _, mount := range container.Mounts {
		if mount.Type != "volume" {
			continue // Only backup named volumes
		}

		volumePath := mount.Source
		if _, err := os.Stat(volumePath); os.IsNotExist(err) {
			slog.Warn("volume path not found, skipping",
				"container", container.Name,
				"volume", mount.Name,
				"path", volumePath,
			)
			continue
		}

		slog.Debug("backing up volume",
			"container", container.Name,
			"volume", mount.Name,
			"path", volumePath,
		)

		if err := v.addVolumeToTar(ctx, tarWriter, mount.Name, volumePath); err != nil {
			return fmt.Errorf("failed to backup volume %s: %w", mount.Name, err)
		}
	}

	return nil
}

// addVolumeToTar adds all files from a volume to the tar archive
func (v *VolumeBackup) addVolumeToTar(ctx context.Context, tarWriter *tar.Writer, volumeName, volumePath string) error {
	return filepath.WalkDir(volumePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get relative path within the volume
		relPath, err := filepath.Rel(volumePath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Prefix with volume name to support multiple volumes
		archivePath := filepath.Join(volumeName, relPath)

		// Get file info
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}
		header.Name = archivePath

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink: %w", err)
			}
			header.Linkname = linkTarget
		}

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// Write file content (only for regular files)
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("failed to write file to tar: %w", err)
			}
		}

		return nil
	})
}

// Restore restores volumes from a backup archive
func (v *VolumeBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}

	// Build a map of volume name to mount source path
	volumePaths := make(map[string]string)
	for _, mount := range container.Mounts {
		if mount.Type == "volume" {
			volumePaths[mount.Name] = mount.Source
		}
	}

	if len(volumePaths) == 0 {
		return fmt.Errorf("container %s has no named volumes to restore", container.Name)
	}

	// Stop container before restore
	wasRunning := container.Running
	if wasRunning {
		slog.Debug("stopping container for volume restore", "container", container.Name)
		if err := dockerClient.StopContainer(ctx, container.ID, 30*time.Second); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		defer func() {
			if err := dockerClient.StartContainer(ctx, container.ID); err != nil {
				slog.Warn("failed to restart container after restore",
					"container", container.Name,
					"error", err,
				)
			}
		}()
	}

	// Create zstd reader
	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(zstdReader)

	// Track which volumes have been cleared
	clearedVolumes := make(map[string]bool)

	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Parse volume name from archive path (first component)
		parts := filepath.SplitList(header.Name)
		if len(parts) == 0 {
			continue
		}

		// Get volume name (first path component)
		volumeName := ""
		relPath := header.Name
		for i, c := range header.Name {
			if c == '/' || c == filepath.Separator {
				volumeName = header.Name[:i]
				relPath = header.Name[i+1:]
				break
			}
		}
		if volumeName == "" {
			volumeName = header.Name
			relPath = "."
		}

		volumePath, ok := volumePaths[volumeName]
		if !ok {
			slog.Warn("backup contains unknown volume, skipping",
				"volume", volumeName,
				"container", container.Name,
			)
			continue
		}

		// Clear volume on first file for this volume
		if !clearedVolumes[volumeName] {
			if err := v.clearVolume(volumePath); err != nil {
				return fmt.Errorf("failed to clear volume %s: %w", volumeName, err)
			}
			clearedVolumes[volumeName] = true
		}

		// Construct target path
		targetPath := filepath.Join(volumePath, relPath)

		// Ensure the path doesn't escape the volume directory
		if !isSubPath(volumePath, targetPath) {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			file.Close()

		case tar.TypeSymlink:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}

	return nil
}

// clearVolume removes all contents from a volume directory
func (v *VolumeBackup) clearVolume(volumePath string) error {
	entries, err := os.ReadDir(volumePath)
	if err != nil {
		return fmt.Errorf("failed to read volume directory: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(volumePath, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// isSubPath checks if child is a subpath of parent
func isSubPath(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	return child == parent || len(child) > len(parent) && child[:len(parent)+1] == parent+string(filepath.Separator)
}
