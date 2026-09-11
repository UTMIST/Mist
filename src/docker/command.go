package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// RunCommand runs a bounded, network-isolated command and returns its actual
// output and exit status. The caller supplies the deadline and trusted command.
func (mgr *DockerMgr) RunCommand(ctx context.Context, image string, command []string, gpu bool) (string, error) {
	host := &container.HostConfig{
		NetworkMode: "none",
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources:   container.Resources{Memory: 4 << 30, NanoCPUs: 4_000_000_000},
	}
	if gpu {
		host.DeviceRequests = []container.DeviceRequest{{Driver: "nvidia", Count: -1, Capabilities: [][]string{{"gpu"}}}}
	}
	mgr.mu.Lock()
	if len(mgr.containers) >= mgr.containerLimit {
		mgr.mu.Unlock()
		return "", fmt.Errorf("container limit reached")
	}
	created, err := mgr.cli.ContainerCreate(ctx, &container.Config{
		Image: image, Cmd: command, Labels: map[string]string{"mist.workload": "local-demo"},
	}, host, nil, nil, "")
	if err == nil {
		mgr.containers[created.ID] = struct{}{}
	}
	mgr.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("create workload container (is image %s installed?): %w", image, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := mgr.cli.ContainerRemove(cleanup, created.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
		if err != nil {
			// Keep failed cleanup in the accounting rather than hiding the resource.
			return
		}
		mgr.mu.Lock()
		delete(mgr.containers, created.ID)
		mgr.mu.Unlock()
	}()
	if err := mgr.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start workload: %w", err)
	}
	statusCh, errCh := mgr.cli.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	var status container.WaitResponse
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("workload interrupted: %w", ctx.Err())
	case err := <-errCh:
		return "", fmt.Errorf("wait for workload: %w", err)
	case status = <-statusCh:
	}
	logs, err := mgr.cli.ContainerLogs(ctx, created.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", fmt.Errorf("read workload logs: %w", err)
	}
	defer logs.Close()
	var output bytes.Buffer
	if _, err := stdcopy.StdCopy(&output, &output, io.LimitReader(logs, 64<<10)); err != nil {
		return output.String(), fmt.Errorf("decode workload logs: %w", err)
	}
	if status.Error != nil {
		return output.String(), fmt.Errorf("workload failed: %s", status.Error.Message)
	}
	if status.StatusCode != 0 {
		return output.String(), fmt.Errorf("workload exited with code %d", status.StatusCode)
	}
	return output.String(), nil
}
