package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// ContainerMgr manages Docker containers and volumes, enforces resource limits, and tracks active resources.
type ContainerMgr struct {
	ctx            context.Context
	cli            *client.Client
	containerLimit int
	volumeLimit    int
	containers     map[string]struct{}
	volumes        map[string]struct{}
	mu             sync.Mutex
}

// NewContainerMgr creates a new ContainerMgr with the specified Docker client and resource limits.
func NewContainerMgr(client *client.Client, containerLimit, volumeLimit int) *ContainerMgr {
	return &ContainerMgr{
		ctx:            context.Background(),
		cli:            client,
		containerLimit: containerLimit,
		volumeLimit:    volumeLimit,
		containers:     make(map[string]struct{}),
		volumes:        make(map[string]struct{}),
	}
}

// stopContainer stops a running container by its ID.
// Returns an error if the operation fails.
func (mgr *ContainerMgr) stopContainer(containerID string) error {
	ctx := mgr.ctx
	cli := mgr.cli

	err := cli.ContainerStop(ctx, containerID, container.StopOptions{})
	if err != nil {
		slog.Error("Failed to stop container", "containerID", containerID, "error", err)
		return err
	}
	return nil
}

// removeContainer removes a container by its ID and deletes it from the internal tracking map.
// Returns an error if the operation fails.
func (mgr *ContainerMgr) removeContainer(containerID string) error {
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

// createVolume creates a Docker volume with the given name, enforcing the volume limit.
// Returns the created volume or an error.
func (mgr *ContainerMgr) createVolume(volumeName string) (volume.Volume, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.volumes) >= mgr.volumeLimit {
		slog.Warn("Volume limit reached", "limit", mgr.volumeLimit)
		return volume.Volume{}, fmt.Errorf("volume limit reached")
	}
	ctx := mgr.ctx
	cli := mgr.cli

	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName, // You can leave this empty for a random name
	})
	if err != nil {
		slog.Error("Failed to create volume", "volumeName", volumeName, "error", err)
		return volume.Volume{}, err
	}
	mgr.volumes[vol.Name] = struct{}{}
	return vol, nil
}

// removeVolume removes a Docker volume by name.
// Returns an error if the volume does not exist or is in use (unless force is true).
func (mgr *ContainerMgr) removeVolume(volumeName string, force bool) error {
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

// runContainer creates and starts a container with the specified image, runtime, and volume attached at /data.
// Enforces the container limit and checks that the volume exists.
// Returns the container ID or an error.
func (mgr *ContainerMgr) runContainer(imageName string, runtimeName string, volumeName string) (string, error) {
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

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageName,
		Cmd:   []string{"sleep", "1000"},
	}, &container.HostConfig{
		Runtime: runtimeName,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/data",
			},
		},
	}, nil, nil, "")
	if err != nil {
		slog.Error("Failed to create container", "imageName", imageName, "runtimeName", runtimeName, "volumeName", volumeName, "error", err)
		return "", err
	}

	mgr.containers[resp.ID] = struct{}{}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		slog.Error("Failed to start container", "containerID", resp.ID, "error", err)
		return "", err
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true})
	if err != nil {
		slog.Error("Failed to get container logs", "containerID", resp.ID, "error", err)
		return resp.ID, err
	}

	io.Copy(os.Stdout, out)
	return resp.ID, nil
}
