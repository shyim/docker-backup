package volume

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/shyim/docker-backup/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// skipIfVolumesNotAccessible checks if Docker volume paths are directly accessible
// from the host. This is typically only true on Linux where Docker runs natively.
// On macOS/Windows with Docker Desktop or OrbStack, volumes are inside a VM.
func skipIfVolumesNotAccessible(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("skipping: Docker volume paths are not directly accessible on " + runtime.GOOS)
	}
	// Also verify we can actually access /var/lib/docker/volumes
	if _, err := os.Stat("/var/lib/docker/volumes"); os.IsNotExist(err) {
		t.Skip("skipping: /var/lib/docker/volumes is not accessible (Docker may be running in a VM)")
	}
}

func TestVolumeBackup_Name(t *testing.T) {
	v := &VolumeBackup{}
	assert.Equal(t, "volume", v.Name())
}

func TestVolumeBackup_FileExtension(t *testing.T) {
	v := &VolumeBackup{}
	assert.Equal(t, ".tar.zst", v.FileExtension())
}

func TestVolumeBackup_Validate(t *testing.T) {
	v := &VolumeBackup{}

	tests := []struct {
		name        string
		container   *docker.ContainerInfo
		expectError bool
	}{
		{
			name: "valid with volume mount",
			container: &docker.ContainerInfo{
				Name: "test",
				Mounts: []docker.MountInfo{
					{
						Type:        "volume",
						Name:        "test-volume",
						Source:      "/var/lib/docker/volumes/test-volume/_data",
						Destination: "/data",
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid with multiple mounts",
			container: &docker.ContainerInfo{
				Name: "test",
				Mounts: []docker.MountInfo{
					{
						Type:        "volume",
						Name:        "vol1",
						Source:      "/var/lib/docker/volumes/vol1/_data",
						Destination: "/data1",
					},
					{
						Type:        "bind",
						Source:      "/host/path",
						Destination: "/data2",
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid no mounts",
			container: &docker.ContainerInfo{
				Name:   "test",
				Mounts: []docker.MountInfo{},
			},
			expectError: true,
		},
		{
			name: "invalid nil mounts",
			container: &docker.ContainerInfo{
				Name:   "test",
				Mounts: nil,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.container)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsSubPath(t *testing.T) {
	tests := []struct {
		parent   string
		child    string
		expected bool
	}{
		{"/data", "/data/file.txt", true},
		{"/data", "/data/subdir/file.txt", true},
		{"/data", "/data", true},
		{"/data", "/data2/file.txt", false},
		{"/data", "/other/file.txt", false},
		{"/data", "/dat", false},
		{"/data/vol", "/data/vol/../other", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.parent, tt.child), func(t *testing.T) {
			result := isSubPath(tt.parent, tt.child)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestVolumeBackup_Integration tests the full backup and restore cycle
// using a real container with a named volume via testcontainers.
func TestVolumeBackup_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	// Create a unique volume name for this test
	volumeName := fmt.Sprintf("test-volume-%d", time.Now().UnixNano())

	// Start a simple alpine container with a named volume
	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName, "/data"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	// Create Docker client
	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	// Get container info
	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Verify volume mount exists
	require.Len(t, containerInfo.Mounts, 1)
	require.Equal(t, "volume", containerInfo.Mounts[0].Type)

	// Get volume path for direct file manipulation
	volumePath := containerInfo.Mounts[0].Source
	require.NotEmpty(t, volumePath)

	// Create test files in the volume using exec in container
	_, _, err = container.Exec(ctx, []string{"sh", "-c", "echo 'Hello World' > /data/test.txt"})
	require.NoError(t, err)

	_, _, err = container.Exec(ctx, []string{"mkdir", "-p", "/data/subdir"})
	require.NoError(t, err)

	_, _, err = container.Exec(ctx, []string{"sh", "-c", "echo 'Nested file content' > /data/subdir/nested.txt"})
	require.NoError(t, err)

	_, _, err = container.Exec(ctx, []string{"sh", "-c", "echo 'Another file' > /data/another.txt"})
	require.NoError(t, err)

	// Verify files exist before backup
	exitCode, _, err := container.Exec(ctx, []string{"cat", "/data/test.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	// Perform backup
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)
	assert.Greater(t, backupBuffer.Len(), 0, "backup should not be empty")

	t.Logf("Backup size: %d bytes", backupBuffer.Len())

	// Container should be running again after backup
	containerInfo, err = dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)
	assert.True(t, containerInfo.Running, "container should be running after backup")

	// Delete files to simulate data loss
	_, _, err = container.Exec(ctx, []string{"rm", "-rf", "/data/test.txt", "/data/subdir", "/data/another.txt"})
	require.NoError(t, err)

	// Verify files are gone
	exitCode, _, err = container.Exec(ctx, []string{"cat", "/data/test.txt"})
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "file should not exist after deletion")

	// Perform restore
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)

	// Container should be running again after restore
	containerInfo, err = dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)
	assert.True(t, containerInfo.Running, "container should be running after restore")

	// Verify files are restored
	exitCode, reader, err := container.Exec(ctx, []string{"cat", "/data/test.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "file should exist after restore")

	output, err := readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "Hello World")

	// Verify nested file
	exitCode, reader, err = container.Exec(ctx, []string{"cat", "/data/subdir/nested.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "nested file should exist after restore")

	output, err = readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "Nested file content")

	// Verify another file
	exitCode, reader, err = container.Exec(ctx, []string{"cat", "/data/another.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "another file should exist after restore")

	output, err = readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "Another file")
}

// TestVolumeBackup_MultipleVolumes tests backup/restore with multiple volumes
func TestVolumeBackup_MultipleVolumes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	// Create unique volume names for this test
	volumeName1 := fmt.Sprintf("test-volume1-%d", time.Now().UnixNano())
	volumeName2 := fmt.Sprintf("test-volume2-%d", time.Now().UnixNano())

	// Start container with two named volumes
	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName1, "/data1"),
			testcontainers.VolumeMount(volumeName2, "/data2"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Verify both volume mounts exist
	volumeCount := 0
	for _, m := range containerInfo.Mounts {
		if m.Type == "volume" {
			volumeCount++
		}
	}
	require.Equal(t, 2, volumeCount)

	// Create test files in both volumes
	_, _, err = container.Exec(ctx, []string{"sh", "-c", "echo 'Volume 1 content' > /data1/file1.txt"})
	require.NoError(t, err)

	_, _, err = container.Exec(ctx, []string{"sh", "-c", "echo 'Volume 2 content' > /data2/file2.txt"})
	require.NoError(t, err)

	// Perform backup
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)
	assert.Greater(t, backupBuffer.Len(), 0)

	t.Logf("Multi-volume backup size: %d bytes", backupBuffer.Len())

	// Delete files
	_, _, err = container.Exec(ctx, []string{"rm", "-f", "/data1/file1.txt", "/data2/file2.txt"})
	require.NoError(t, err)

	// Verify files are gone
	exitCode, _, err := container.Exec(ctx, []string{"cat", "/data1/file1.txt"})
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode)

	// Perform restore
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)

	// Verify files are restored in both volumes
	exitCode, reader, err := container.Exec(ctx, []string{"cat", "/data1/file1.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err := readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "Volume 1 content")

	exitCode, reader, err = container.Exec(ctx, []string{"cat", "/data2/file2.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err = readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "Volume 2 content")
}

// TestVolumeBackup_LargeFiles tests backup/restore with larger files
func TestVolumeBackup_LargeFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	volumeName := fmt.Sprintf("test-volume-large-%d", time.Now().UnixNano())

	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName, "/data"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Create a 1MB file with random-ish data
	_, _, err = container.Exec(ctx, []string{"sh", "-c", "dd if=/dev/urandom of=/data/largefile.bin bs=1024 count=1024 2>/dev/null"})
	require.NoError(t, err)

	// Get checksum of original file
	exitCode, reader, err := container.Exec(ctx, []string{"md5sum", "/data/largefile.bin"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err := readExecOutput(reader)
	require.NoError(t, err)
	originalChecksum := output[:32] // md5sum output format: "checksum  filename"

	// Perform backup
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	t.Logf("Large file backup size: %d bytes (original: 1MB)", backupBuffer.Len())

	// Delete file
	_, _, err = container.Exec(ctx, []string{"rm", "-f", "/data/largefile.bin"})
	require.NoError(t, err)

	// Restore
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)

	// Verify checksum matches
	exitCode, reader, err = container.Exec(ctx, []string{"md5sum", "/data/largefile.bin"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err = readExecOutput(reader)
	require.NoError(t, err)
	restoredChecksum := output[:32]

	assert.Equal(t, originalChecksum, restoredChecksum, "file checksum should match after restore")
}

// TestVolumeBackup_SpecialFilenames tests backup/restore with special characters in filenames
func TestVolumeBackup_SpecialFilenames(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	volumeName := fmt.Sprintf("test-volume-special-%d", time.Now().UnixNano())

	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName, "/data"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Create files with special characters in names
	specialFiles := []struct {
		name    string
		content string
	}{
		{"file with spaces.txt", "spaces content"},
		{"file-with-dashes.txt", "dashes content"},
		{"file_with_underscores.txt", "underscores content"},
		{"日本語ファイル.txt", "unicode content"},
	}

	for _, f := range specialFiles {
		_, _, err = container.Exec(ctx, []string{"sh", "-c", fmt.Sprintf("echo '%s' > '/data/%s'", f.content, f.name)})
		require.NoError(t, err, "failed to create file: %s", f.name)
	}

	// Perform backup
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Delete all files
	_, _, err = container.Exec(ctx, []string{"sh", "-c", "rm -rf /data/*"})
	require.NoError(t, err)

	// Restore
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)

	// Verify all files are restored
	for _, f := range specialFiles {
		exitCode, reader, err := container.Exec(ctx, []string{"cat", fmt.Sprintf("/data/%s", f.name)})
		require.NoError(t, err)
		require.Equal(t, 0, exitCode, "file %s should exist after restore", f.name)

		output, err := readExecOutput(reader)
		require.NoError(t, err)
		assert.Contains(t, output, f.content, "file %s should have correct content", f.name)
	}
}

// TestVolumeBackup_Symlinks tests backup/restore with symbolic links
func TestVolumeBackup_Symlinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	volumeName := fmt.Sprintf("test-volume-symlink-%d", time.Now().UnixNano())

	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName, "/data"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Create a file and a symlink to it
	_, _, err = container.Exec(ctx, []string{"sh", "-c", "echo 'original content' > /data/original.txt"})
	require.NoError(t, err)

	_, _, err = container.Exec(ctx, []string{"ln", "-s", "original.txt", "/data/link.txt"})
	require.NoError(t, err)

	// Verify symlink works before backup
	exitCode, reader, err := container.Exec(ctx, []string{"cat", "/data/link.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err := readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "original content")

	// Perform backup
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Delete files
	_, _, err = container.Exec(ctx, []string{"rm", "-rf", "/data/original.txt", "/data/link.txt"})
	require.NoError(t, err)

	// Restore
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)

	// Verify symlink is restored and works
	exitCode, reader, err = container.Exec(ctx, []string{"cat", "/data/link.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err = readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "original content")

	// Verify it's actually a symlink
	exitCode, reader, err = container.Exec(ctx, []string{"readlink", "/data/link.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err = readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "original.txt")
}

// TestVolumeBackup_EmptyVolume tests backup/restore with an empty volume
func TestVolumeBackup_EmptyVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	volumeName := fmt.Sprintf("test-volume-empty-%d", time.Now().UnixNano())

	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName, "/data"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Backup empty volume (should still work)
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	t.Logf("Empty volume backup size: %d bytes", backupBuffer.Len())

	// Restore should also work without errors
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)
}

// TestVolumeBackup_DeepDirectoryStructure tests backup/restore with deeply nested directories
func TestVolumeBackup_DeepDirectoryStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	skipIfVolumesNotAccessible(t)

	ctx := context.Background()

	volumeName := fmt.Sprintf("test-volume-deep-%d", time.Now().UnixNano())

	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.VolumeMount(volumeName, "/data"),
		},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	containerID := container.GetContainerID()

	dockerClient, err := docker.NewClient("")
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	containerInfo, err := dockerClient.GetContainer(ctx, containerID)
	require.NoError(t, err)

	// Create deeply nested directory structure
	deepPath := "/data/a/b/c/d/e/f/g/h/i/j"
	_, _, err = container.Exec(ctx, []string{"mkdir", "-p", deepPath})
	require.NoError(t, err)

	_, _, err = container.Exec(ctx, []string{"sh", "-c", fmt.Sprintf("echo 'deep content' > %s/deep.txt", deepPath)})
	require.NoError(t, err)

	// Perform backup
	v := &VolumeBackup{}
	var backupBuffer bytes.Buffer
	err = v.Backup(ctx, containerInfo, dockerClient, &backupBuffer)
	require.NoError(t, err)

	// Delete all
	_, _, err = container.Exec(ctx, []string{"rm", "-rf", "/data/a"})
	require.NoError(t, err)

	// Restore
	err = v.Restore(ctx, containerInfo, dockerClient, bytes.NewReader(backupBuffer.Bytes()))
	require.NoError(t, err)

	// Verify deep file is restored
	exitCode, reader, err := container.Exec(ctx, []string{"cat", filepath.Join(deepPath, "deep.txt")})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	output, err := readExecOutput(reader)
	require.NoError(t, err)
	assert.Contains(t, output, "deep content")
}

// Helper function to read exec output from testcontainers
func readExecOutput(reader io.Reader) (string, error) {
	if reader == nil {
		return "", nil
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
