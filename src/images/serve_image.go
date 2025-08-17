package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

type ContainerMgr struct {
	ctx            context.Context
	cli            *client.Client
	containerLimit int
	volumeLimit    int
	containers     map[string]struct{}
	volumes        map[string]struct{}
}

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

func (mgr *ContainerMgr) stopContainer(containerID string) {
	ctx := mgr.ctx
	cli := mgr.cli

	err := cli.ContainerStop(ctx, containerID, container.StopOptions{})
	if err != nil {
		panic(err)
	}

}

func (mgr *ContainerMgr) removeContainer(containerID string) error {
	ctx := mgr.ctx
	cli := mgr.cli
	err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{RemoveVolumes: true})
	if err != nil {
		return err
	}
	delete(mgr.containers, containerID)
	return nil
}

func (mgr *ContainerMgr) createVolume(volumeName string) (volume.Volume, error) {
	if len(mgr.volumes) >= mgr.volumeLimit {
		return volume.Volume{}, fmt.Errorf("volume limit reached")
	}
	ctx := mgr.ctx
	cli := mgr.cli

	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName, // You can leave this empty for a random name
	})
	if err != nil {
		return volume.Volume{}, err
	}
	mgr.volumes[vol.Name] = struct{}{}
	return vol, nil
}

func (mgr *ContainerMgr) removeVolume(volumeName string, force bool) error {
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
		return fmt.Errorf("volume %s does not exist", volumeName)
	}

	err := cli.VolumeRemove(ctx, volumeName, force)
	if err != nil {
		return err
	}
	delete(mgr.volumes, volumeName)
	return nil
}

func (mgr *ContainerMgr) runContainerCuda(volumeName string) (string, error) {
	if len(mgr.containers) >= mgr.containerLimit {
		return "", fmt.Errorf("container limit reached")
	}
	ctx := mgr.ctx
	cli := mgr.cli

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "pytorch-cuda",
		Cmd:   []string{"sleep", "1000"},
	}, &container.HostConfig{
		Runtime: "nvidia",
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/data",
			},
		},
	}, nil, nil, "")
	vols, _ := cli.VolumeList(ctx, volume.ListOptions{})
	found := false
	for _, v := range vols.Volumes {
		if v.Name == volumeName {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("volume %s does not exist", volumeName)
	}
	if err != nil {
		return "", err
	}
	mgr.containers[resp.ID] = struct{}{}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", err
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}

	io.Copy(os.Stdout, out)
	return resp.ID, nil
}

func main() {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	// Create a Docker volume
	containerMgr := NewContainerMgr(cli, 10, 10)
	volumeName := "my_volume1"

	containerMgr.createVolume(volumeName)
	id, err := containerMgr.runContainerCuda(volumeName)
	if err != nil {
		fmt.Errorf("Failed to start container: %v", err.Error())
	}
	containerMgr.stopContainer(id)
	containerMgr.removeContainer(id)
	containerMgr.removeVolume(volumeName, true)

}
