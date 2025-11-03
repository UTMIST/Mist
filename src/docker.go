package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/docker/docker/api/types/container"
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

// GetContainerLogs fetches container logs from Docker for the specified container.
// Returns a ReadCloser that can be used to read the logs, or an error if the operation fails.
// Options:
//   - tail: number of lines to return from the end of logs (0 = all)
//   - follow: whether to follow log output (default: false)
//   - since: return logs since this timestamp (RFC3339 format)
//   - until: return logs before this timestamp (RFC3339 format)
func (mgr *ContainerMgr) GetContainerLogs(containerID string, tail int, follow bool, since, until string) (io.ReadCloser, error) {
	ctx := mgr.ctx
	cli := mgr.cli

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: false, // Don't include timestamps in output
	}
	
	// Docker API: use "all" for all logs, or a number for tail
	if tail > 0 {
		opts.Tail = fmt.Sprintf("%d", tail)
	} else {
		opts.Tail = "all"
	}

	if since != "" {
		opts.Since = since
	}
	if until != "" {
		opts.Until = until
	}

	reader, err := cli.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}

	return reader, nil
}

// AssociateContainer associates a container ID with the ContainerMgr for tracking
func (mgr *ContainerMgr) AssociateContainer(containerID string) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.containers[containerID] = struct{}{}
	slog.Info("container associated", "container_id", containerID)
}

