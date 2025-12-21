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

type VolumeBackup struct{}

func (v *VolumeBackup) Name() string {
	return "volume"
}

func (v *VolumeBackup) FileExtension() string {
	return ".tar.zst"
}

func (v *VolumeBackup) Validate(container *docker.ContainerInfo) error {
	// Volume backups work with any container that has mounted volumes
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}
	return nil
}

func (v *VolumeBackup) Backup(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, w io.Writer) error {
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}

	var volumeNames []string
	for _, mount := range container.Mounts {
		if mount.Type == "volume" {
			volumeNames = append(volumeNames, mount.Name)
		}
	}

	stoppedContainers := make(map[string]bool)
	for _, volumeName := range volumeNames {
		containers, err := dockerClient.GetContainersUsingVolume(ctx, volumeName)
		if err != nil {
			slog.Warn("failed to get containers using volume",
				"volume", volumeName,
				"error", err,
			)
			continue
		}

		for _, c := range containers {
			if _, alreadyProcessed := stoppedContainers[c.ID]; alreadyProcessed {
				continue
			}

			if c.Running {
				slog.Debug("stopping container for volume backup",
					"container", c.Name,
					"volume", volumeName,
				)
				if err := dockerClient.StopContainer(ctx, c.ID, 30*time.Second); err != nil {
					v.restartContainers(ctx, dockerClient, stoppedContainers)
					return fmt.Errorf("failed to stop container %s: %w", c.Name, err)
				}
				stoppedContainers[c.ID] = true
			} else {
				stoppedContainers[c.ID] = false
			}
		}
	}

	defer v.restartContainers(ctx, dockerClient, stoppedContainers)

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

	for _, mount := range container.Mounts {
		if mount.Type != "volume" {
			continue
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

func (v *VolumeBackup) addVolumeToTar(ctx context.Context, tarWriter *tar.Writer, volumeName, volumePath string) error {
	return filepath.WalkDir(volumePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(volumePath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		archivePath := filepath.Join(volumeName, relPath)

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}
		header.Name = archivePath

		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink: %w", err)
			}
			header.Linkname = linkTarget
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer func() {
				_ = file.Close()
			}()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("failed to write file to tar: %w", err)
			}
		}

		return nil
	})
}

func (v *VolumeBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}

	volumePaths := make(map[string]string)
	var volumeNames []string
	for _, mount := range container.Mounts {
		if mount.Type == "volume" {
			volumePaths[mount.Name] = mount.Source
			volumeNames = append(volumeNames, mount.Name)
		}
	}

	if len(volumePaths) == 0 {
		return fmt.Errorf("container %s has no named volumes to restore", container.Name)
	}

	stoppedContainers := make(map[string]bool)
	for _, volumeName := range volumeNames {
		containers, err := dockerClient.GetContainersUsingVolume(ctx, volumeName)
		if err != nil {
			slog.Warn("failed to get containers using volume",
				"volume", volumeName,
				"error", err,
			)
			continue
		}

		for _, c := range containers {
			if _, alreadyProcessed := stoppedContainers[c.ID]; alreadyProcessed {
				continue
			}

			if c.Running {
				slog.Debug("stopping container for volume restore",
					"container", c.Name,
					"volume", volumeName,
				)
				if err := dockerClient.StopContainer(ctx, c.ID, 30*time.Second); err != nil {
					v.restartContainers(ctx, dockerClient, stoppedContainers)
					return fmt.Errorf("failed to stop container %s: %w", c.Name, err)
				}
				stoppedContainers[c.ID] = true
			} else {
				stoppedContainers[c.ID] = false
			}
		}
	}

	defer v.restartContainers(ctx, dockerClient, stoppedContainers)

	zstdReader, err := zstd.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)

	clearedVolumes := make(map[string]bool)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		parts := filepath.SplitList(header.Name)
		if len(parts) == 0 {
			continue
		}

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

		if !clearedVolumes[volumeName] {
			if err := v.clearVolume(volumePath); err != nil {
				return fmt.Errorf("failed to clear volume %s: %w", volumeName, err)
			}
			clearedVolumes[volumeName] = true
		}

		targetPath := filepath.Join(volumePath, relPath)

		if !isSubPath(volumePath, targetPath) {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			_ = file.Close()

		case tar.TypeSymlink:
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

func (v *VolumeBackup) restartContainers(ctx context.Context, dockerClient *docker.Client, stoppedContainers map[string]bool) {
	for containerID, wasRunning := range stoppedContainers {
		if wasRunning {
			if err := dockerClient.StartContainer(ctx, containerID); err != nil {
				slog.Warn("failed to restart container after backup/restore",
					"container", containerID,
					"error", err,
				)
			}
		}
	}
}

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

func isSubPath(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	return child == parent || len(child) > len(parent) && child[:len(parent)+1] == parent+string(filepath.Separator)
}
