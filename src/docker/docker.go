package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerMgr manages Docker containers and volumes, enforces resource limits, and tracks active resources.
type DockerMgr struct {
	ctx            context.Context
	cli            *client.Client
	containerLimit int
	volumeLimit    int
	containers     map[string]struct{}
	volumes        map[string]struct{}
	mu             sync.Mutex
}

// NewDockerMgr creates a new DockerMgr with the specified Docker client and resource limits.
func NewDockerMgr(client *client.Client, containerLimit, volumeLimit int) *DockerMgr {
	return &DockerMgr{
		ctx:            context.Background(),
		cli:            client,
		containerLimit: containerLimit,
		volumeLimit:    volumeLimit,
		containers:     make(map[string]struct{}),
		volumes:        make(map[string]struct{}),
	}
}

// StopContainer stops a running container by its ID.
// Returns an error if the operation fails.
func (mgr *DockerMgr) StopContainer(containerID string) error {
	ctx := mgr.ctx
	cli := mgr.cli

	err := cli.ContainerStop(ctx, containerID, container.StopOptions{})
	if err != nil {
		slog.Error("Failed to stop container", "containerID", containerID, "error", err)
		return err
	}
	return nil
}

// RemoveContainer removes a container by its ID and deletes it from the internal tracking map.
// Returns an error if the operation fails.
func (mgr *DockerMgr) RemoveContainer(containerID string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	ctx := mgr.ctx
	cli := mgr.cli
	err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{RemoveVolumes: true})
	if err != nil {
		slog.Error("Failed to remove container", "containerID", containerID, "error", err)
		return err
	}
	delete(mgr.containers, containerID)
	return nil
}

// CreateVolume creates a Docker volume with the given name, enforcing the volume limit.
// Returns the created volume or an error.
func (mgr *DockerMgr) CreateVolume(volumeName string) (volume.Volume, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.volumes) >= mgr.volumeLimit {
		slog.Warn("Volume limit reached", "limit", mgr.volumeLimit)
		return volume.Volume{}, fmt.Errorf("volume limit reached")
	}
	ctx := mgr.ctx
	cli := mgr.cli

	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{Name: volumeName})
	if err != nil {
		slog.Error("Failed to create volume", "volumeName", volumeName, "error", err)
		return volume.Volume{}, err
	}
	mgr.volumes[vol.Name] = struct{}{}
	return vol, nil
}

// RemoveVolume removes a Docker volume by name.
// Returns an error if the volume does not exist or is in use (unless force is true).
func (mgr *DockerMgr) RemoveVolume(volumeName string, force bool) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	ctx := mgr.ctx
	cli := mgr.cli

	vols, _ := cli.VolumeList(ctx, volume.ListOptions{})
	found := false
	for _, v := range vols.Volumes {
		if v.Name == volumeName {
			found = true
			break
		}
	}
	if !found {
		slog.Warn("Volume does not exist", "volumeName", volumeName)
		return fmt.Errorf("volume %s does not exist", volumeName)
	}

	err := cli.VolumeRemove(ctx, volumeName, force)
	if err != nil {
		slog.Error("Failed to remove volume", "volumeName", volumeName, "error", err)
		return err
	}
	delete(mgr.volumes, volumeName)
	return nil
}

// RunContainer creates and starts a container with the specified image, runtime, volume, and name.
// containerName is used as the Docker container name (e.g. job ID for log lookup by name).
// Enforces the container limit and checks that the volume exists.
// Returns the container ID or an error.
func (mgr *DockerMgr) RunContainer(imageName string, runtimeName string, volumeName string, containerName string) (string, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.containers) >= mgr.containerLimit {
		slog.Warn("Container limit reached", "limit", mgr.containerLimit)
		return "", fmt.Errorf("container limit reached")
	}
	ctx := mgr.ctx
	cli := mgr.cli

	vols, _ := cli.VolumeList(ctx, volume.ListOptions{})
	found := false
	for _, v := range vols.Volumes {
		if v.Name == volumeName {
			found = true
			break
		}
	}
	if !found {
		slog.Error("Volume does not exist for container", "volumeName", volumeName)
		return "", fmt.Errorf("volume %s does not exist", volumeName)
	}

	resp, err := cli.ContainerCreate(
		ctx,
		&container.Config{
			Image: imageName,
			Cmd:   []string{"sh", "-c", "echo hello-from-container && sleep 1000"},
		},
		&container.HostConfig{
			Runtime: runtimeName,
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: volumeName,
					Target: "/data",
				},
			},
		},
		nil,
		nil,
		containerName,
	)

	if err != nil {
		slog.Error("Failed to create container", "imageName", imageName, "runtimeName", runtimeName, "volumeName", volumeName, "error", err)
		return "", err
	}

	mgr.containers[resp.ID] = struct{}{}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		slog.Error("Failed to start container", "containerID", resp.ID, "error", err)
		return "", err
	}

	return resp.ID, nil
}

// LogOptions configures how container logs are fetched.
type LogOptions struct {
	ShowStdout bool
	ShowStderr bool
	Tail       string // e.g. "100" for last 100 lines, or "all"
	Since      string // RFC3339 timestamp
	Until      string // RFC3339 timestamp
	Timestamps bool
}

// IsContainerRunning returns true if the container exists and is running.
func (mgr *DockerMgr) IsContainerRunning(ctx context.Context, containerID string) (bool, error) {
	inspect, err := mgr.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}
	return inspect.State.Running, nil
}

// GetContainerLogs fetches logs from a Docker container (running or stopped).
// Returns an error if the container does not exist or logs cannot be read.
func (mgr *DockerMgr) GetContainerLogs(ctx context.Context, containerID string, opts LogOptions) ([]byte, error) {
	// Docker allows fetching logs from stopped containers; no need to require running.
	_, err := mgr.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("container not found: %w", err)
	}
	if opts.ShowStdout == false && opts.ShowStderr == false {
		opts.ShowStdout = true
		opts.ShowStderr = true
	}

	options := container.LogsOptions{
		ShowStdout: opts.ShowStdout,
		ShowStderr: opts.ShowStderr,
		Timestamps: opts.Timestamps,
	}
	if opts.Tail != "" {
		options.Tail = opts.Tail
	}
	if opts.Since != "" {
		options.Since = opts.Since
	}
	if opts.Until != "" {
		options.Until = opts.Until
	}

	reader, err := mgr.cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		slog.Error("Failed to get container logs", "containerID", containerID, "error", err)
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}
	defer reader.Close()

	var buf bytes.Buffer
	_, err = stdcopy.StdCopy(&buf, &buf, reader)
	if err != nil && err != io.EOF {
		slog.Error("Failed to demultiplex container logs", "containerID", containerID, "error", err)
		return nil, fmt.Errorf("failed to read container logs: %w", err)
	}

	return buf.Bytes(), nil
}
