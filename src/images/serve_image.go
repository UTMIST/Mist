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

func (containerMgr *ContainerMgr) spinUpContainer() {
}

func (containerMgr *ContainerMgr) spinUpContainerRocm() {
}

func (containerMgr *ContainerMgr) spinUpContainerCpu() {
}

func (containerMgr *ContainerMgr) spinUpContainerCuda(volName string) {
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

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			panic(err)
		}
	case <-statusCh:
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}

	io.Copy(os.Stdout, out)
}

func main() {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	// Create a Docker volume
	vol, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: "my_volume", // You can leave this empty for a random name
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("Created volume:", vol.Name)
	containerMgr := NewContainerMgr(cli)
	containerMgr.spinUpContainerCuda("my_volume")

}
