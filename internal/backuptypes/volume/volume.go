package volume

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
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

		slog.Debug("backing up volume",
			"container", container.Name,
			"volume", mount.Name,
			"path", mount.Destination,
		)

		if err := v.addVolumeToTar(ctx, dockerClient, tarWriter, container.ID, mount.Name, mount.Destination); err != nil {
			return fmt.Errorf("failed to backup volume %s: %w", mount.Name, err)
		}
	}

	return nil
}

func (v *VolumeBackup) addVolumeToTar(ctx context.Context, dockerClient *docker.Client, tarWriter *tar.Writer, containerID, volumeName, mountPath string) error {
	reader, err := dockerClient.CopyFromContainer(ctx, containerID, mountPath)
	if err != nil {
		return fmt.Errorf("failed to copy volume from container: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	// Docker prefixes archive entries with the basename of the copied path; strip
	// it and re-root everything under the volume name for the restore to map back.
	srcPrefix := path.Base(mountPath)

	tarReader := tar.NewReader(reader)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read volume archive: %w", err)
		}

		relPath := strings.TrimPrefix(header.Name, srcPrefix)
		relPath = strings.TrimPrefix(relPath, "/")

		newName := volumeName
		if relPath != "" {
			newName = volumeName + "/" + relPath
		}
		if strings.HasSuffix(header.Name, "/") && !strings.HasSuffix(newName, "/") {
			newName += "/"
		}
		header.Name = newName

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		if header.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tarWriter, tarReader); err != nil {
				return fmt.Errorf("failed to write file to tar: %w", err)
			}
		}
	}

	return nil
}

func (v *VolumeBackup) Restore(ctx context.Context, container *docker.ContainerInfo, dockerClient *docker.Client, r io.Reader) error {
	if len(container.Mounts) == 0 {
		return fmt.Errorf("container %s has no mounted volumes", container.Name)
	}

	volumeDests := make(map[string]string)
	var volumeNames []string
	for _, mount := range container.Mounts {
		if mount.Type == "volume" {
			volumeDests[mount.Name] = mount.Destination
			volumeNames = append(volumeNames, mount.Name)
		}
	}

	if len(volumeDests) == 0 {
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

	// Entries are grouped per volume, so stream each volume into the container
	// through CopyToContainer, switching streams when the volume name changes.
	var current *volumeRestoreStream

	finishCurrent := func() error {
		if current == nil {
			return nil
		}
		err := current.close()
		current = nil
		return err
	}

	for {
		select {
		case <-ctx.Done():
			_ = finishCurrent()
			return ctx.Err()
		default:
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = finishCurrent()
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		volumeName, relPath := splitVolumePath(header.Name)

		dest, ok := volumeDests[volumeName]
		if !ok {
			slog.Warn("backup contains unknown volume, skipping",
				"volume", volumeName,
				"container", container.Name,
			)
			continue
		}

		if current == nil || current.volumeName != volumeName {
			if err := finishCurrent(); err != nil {
				return fmt.Errorf("failed to restore volume: %w", err)
			}
			current, err = newVolumeRestoreStream(ctx, dockerClient, container.ID, volumeName, dest)
			if err != nil {
				return fmt.Errorf("failed to start restore for volume %s: %w", volumeName, err)
			}
		}

		newName := path.Base(dest)
		if relPath != "" {
			newName += "/" + relPath
		}
		if strings.HasSuffix(header.Name, "/") && !strings.HasSuffix(newName, "/") {
			newName += "/"
		}
		header.Name = newName

		if err := current.writer.WriteHeader(header); err != nil {
			_ = finishCurrent()
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		if header.Typeflag == tar.TypeReg {
			if _, err := io.Copy(current.writer, tarReader); err != nil {
				_ = finishCurrent()
				return fmt.Errorf("failed to write file: %w", err)
			}
		}
	}

	if err := finishCurrent(); err != nil {
		return fmt.Errorf("failed to restore volume: %w", err)
	}

	return nil
}

// volumeRestoreStream pipes a tar archive of a single volume into the container
// via CopyToContainer.
type volumeRestoreStream struct {
	volumeName string
	pw         *io.PipeWriter
	writer     *tar.Writer
	done       chan error
}

func newVolumeRestoreStream(ctx context.Context, dockerClient *docker.Client, containerID, volumeName, dest string) (*volumeRestoreStream, error) {
	pr, pw := io.Pipe()
	s := &volumeRestoreStream{
		volumeName: volumeName,
		pw:         pw,
		writer:     tar.NewWriter(pw),
		done:       make(chan error, 1),
	}

	// CopyToContainer extracts into the parent of dest, so entries prefixed with
	// path.Base(dest) land inside the mount destination.
	target := path.Dir(dest)
	go func() {
		s.done <- dockerClient.CopyToContainer(ctx, containerID, target, pr)
	}()

	return s, nil
}

func (s *volumeRestoreStream) close() error {
	if err := s.writer.Close(); err != nil {
		_ = s.pw.CloseWithError(err)
		<-s.done
		return err
	}
	if err := s.pw.Close(); err != nil {
		<-s.done
		return err
	}
	return <-s.done
}

func splitVolumePath(name string) (volumeName, relPath string) {
	idx := strings.IndexByte(name, '/')
	if idx < 0 {
		return name, ""
	}
	return name[:idx], strings.TrimPrefix(name[idx+1:], "/")
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

