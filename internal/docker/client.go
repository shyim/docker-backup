package docker

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// MountInfo holds information about a container mount
type MountInfo struct {
	Type        string // "volume", "bind", "tmpfs"
	Name        string // Volume name (for volume mounts)
	Source      string // Host path
	Destination string // Container path
}

// ContainerInfo holds relevant container information
type ContainerInfo struct {
	ID        string
	Name      string
	Labels    map[string]string
	Env       map[string]string
	NetworkIP string
	Running   bool
	Mounts    []MountInfo
}

// VolumeInfo holds relevant volume information
type VolumeInfo struct {
	Name       string
	Driver     string
	Mountpoint string // Host path, e.g., /var/lib/docker/volumes/myvolume/_data
	Labels     map[string]string
}

// Client wraps the Docker API client
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client
func NewClient(host string) (*Client, error) {
	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}

	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}

	// Verify connection
	_, err = cli.Ping(context.Background())
	if err != nil {
		return nil, err
	}

	return &Client{cli: cli}, nil
}

// Close closes the Docker client
func (c *Client) Close() error {
	return c.cli.Close()
}

// ListContainers returns all running containers
func (c *Client) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All: false, // Only running containers
	})
	if err != nil {
		return nil, err
	}

	var result []ContainerInfo
	for _, ctr := range containers {
		info, err := c.GetContainer(ctx, ctr.ID)
		if err != nil {
			continue // Skip containers we can't inspect
		}
		result = append(result, *info)
	}

	return result, nil
}

// GetContainer returns detailed information about a specific container
func (c *Client) GetContainer(ctx context.Context, containerID string) (*ContainerInfo, error) {
	inspect, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	// Parse environment variables into a map
	env := make(map[string]string)
	for _, e := range inspect.Config.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	// Get network IP (prefer first network's IP)
	var networkIP string
	if inspect.NetworkSettings != nil {
		for _, network := range inspect.NetworkSettings.Networks {
			if network.IPAddress != "" {
				networkIP = network.IPAddress
				break
			}
		}
	}

	// Clean container name (remove leading /)
	name := strings.TrimPrefix(inspect.Name, "/")

	// Parse mounts
	var mounts []MountInfo
	for _, m := range inspect.Mounts {
		mounts = append(mounts, MountInfo{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
		})
	}

	return &ContainerInfo{
		ID:        inspect.ID,
		Name:      name,
		Labels:    inspect.Config.Labels,
		Env:       env,
		NetworkIP: networkIP,
		Running:   inspect.State.Running,
		Mounts:    mounts,
	}, nil
}

// WatchEvents returns a channel of container events
func (c *Client) WatchEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	filterArgs.Add("event", "start")
	filterArgs.Add("event", "stop")
	filterArgs.Add("event", "die")

	return c.cli.Events(ctx, events.ListOptions{
		Filters: filterArgs,
	})
}

// ExecResult contains the result of a container exec
type ExecResult struct {
	ExitCode int
	Output   string
}

// Exec runs a command in a container and pipes stdin to it
func (c *Client) Exec(ctx context.Context, containerID string, cmd []string, stdin io.Reader) (*ExecResult, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := c.cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, err
	}

	resp, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	// If we have stdin data, write it
	if stdin != nil {
		go func() {
			_, _ = io.Copy(resp.Conn, stdin)
			_ = resp.CloseWrite()
		}()
	}

	// Read output - demultiplex Docker stream
	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	if err != nil {
		return nil, err
	}

	// Get exit code
	inspectResp, err := c.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, err
	}

	// Combine stdout and stderr for output
	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}

	return &ExecResult{
		ExitCode: inspectResp.ExitCode,
		Output:   output,
	}, nil
}

func (c *Client) ExecWithOutput(ctx context.Context, containerID string, cmd []string, stdout io.Writer) (int, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := c.cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return -1, err
	}

	resp, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return -1, err
	}
	defer resp.Close()

	// Demultiplex Docker stream - write stdout to writer, discard stderr
	_, err = stdcopy.StdCopy(stdout, io.Discard, resp.Reader)
	if err != nil {
		return -1, err
	}

	// Get exit code
	inspectResp, err := c.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return -1, err
	}

	return inspectResp.ExitCode, nil
}

// ListVolumes returns all Docker volumes
func (c *Client) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	resp, err := c.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}

	var result []VolumeInfo
	for _, vol := range resp.Volumes {
		result = append(result, VolumeInfo{
			Name:       vol.Name,
			Driver:     vol.Driver,
			Mountpoint: vol.Mountpoint,
			Labels:     vol.Labels,
		})
	}

	return result, nil
}

// GetVolume returns information about a specific volume
func (c *Client) GetVolume(ctx context.Context, name string) (*VolumeInfo, error) {
	vol, err := c.cli.VolumeInspect(ctx, name)
	if err != nil {
		return nil, err
	}

	return &VolumeInfo{
		Name:       vol.Name,
		Driver:     vol.Driver,
		Mountpoint: vol.Mountpoint,
		Labels:     vol.Labels,
	}, nil
}

// WatchVolumeEvents returns a channel of volume events
func (c *Client) WatchVolumeEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "volume")
	filterArgs.Add("event", "create")
	filterArgs.Add("event", "destroy")

	return c.cli.Events(ctx, events.ListOptions{
		Filters: filterArgs,
	})
}

// GetContainersUsingVolume returns all containers (running and stopped) using a specific volume
func (c *Client) GetContainersUsingVolume(ctx context.Context, volumeName string) ([]ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("volume", volumeName)

	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true, // Include stopped containers
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	var result []ContainerInfo
	for _, ctr := range containers {
		info, err := c.GetContainer(ctx, ctr.ID)
		if err != nil {
			continue
		}
		result = append(result, *info)
	}

	return result, nil
}

// StopContainer stops a container with the given timeout
func (c *Client) StopContainer(ctx context.Context, containerID string, timeout time.Duration) error {
	timeoutSeconds := int(timeout.Seconds())
	return c.cli.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeoutSeconds,
	})
}

// StartContainer starts a stopped container
func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	return c.cli.ContainerStart(ctx, containerID, container.StartOptions{})
}
