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
	ctx context.Context
	cli *client.Client
}

func NewContainerMgr(client *client.Client) *ContainerMgr {
	return &ContainerMgr{
		ctx: context.Background(),
		cli: client,
	}
}

func (containerMgr *ContainerMgr) stopContainer(containerID string) {
	ctx := containerMgr.ctx
	cli := containerMgr.cli

	err := cli.ContainerStop(ctx, containerID, container.StopOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Println("stopped container:", containerID)

}

func (containerMgr *ContainerMgr) removeContainer(containerID string) {
	ctx := containerMgr.ctx
	cli := containerMgr.cli

	err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{RemoveVolumes: true})
	if err != nil {
		panic(err)
	}
	fmt.Println("remove container:", containerID)

}

func (containerMgr *ContainerMgr) createVolume(volumeName string) volume.Volume {
	ctx := containerMgr.ctx
	cli := containerMgr.cli

	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName, // You can leave this empty for a random name
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("Created volume:", vol.Name)
	return vol
}

func (containerMgr *ContainerMgr) removeVolume(volumeName string, force bool) error {
	ctx := containerMgr.ctx
	cli := containerMgr.cli

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
	fmt.Printf("Remove error: %#v\n", err)
	if err != nil {
		return err
	}
	fmt.Println("Remove volume:", volumeName)
	return nil
}

func (containerMgr *ContainerMgr) runContainerCuda(volName string) string {
	ctx := containerMgr.ctx
	cli := containerMgr.cli

	hostConfig := &container.HostConfig{
		Runtime: "nvidia",
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volName,
				Target: "/data",
			},
		},
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "pytorch-cuda",
		Cmd:   []string{"sleep", "1000"},
	}, hostConfig, nil, nil, "")
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.ID)

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		panic(err)
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}

	io.Copy(os.Stdout, out)
	return resp.ID
}

func main() {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	// Create a Docker volume
	containerMgr := NewContainerMgr(cli)
	volumeName := "my_volume1"

	containerMgr.createVolume(volumeName)
	id := containerMgr.runContainerCuda(volumeName)
	containerMgr.stopContainer(id)
	containerMgr.removeContainer(id)
	containerMgr.removeVolume(volumeName, true)

}
