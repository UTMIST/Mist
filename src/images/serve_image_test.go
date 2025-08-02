package main

import (
	"testing"

	"github.com/docker/docker/client"
)

func TestImage(t *testing.T) {

	t.Log("Hello world")
	// t.Error("F")

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	// Create a Docker volume
	containerMgr := NewContainerMgr(cli)
	volumeName := "my_volume1"

	containerMgr.createVolume(volumeName)
	// assert volume meets certain requirements
	// 1. The total capacity of the volume
	// 2.

	// What if you create a volume with the same name

	// What if you delete a volume with a name that doesn't exist

	// T 1
	// create a volume
	// check volume exists
	// delete volume
	// check volume does not exist
	// cleanup

	// T 2
	// create a volume
	// start container and attach volume
	// check volume is accessible
	// write to the volume
	// stop the container
	// start the containerr
	// check that data is persisted
	// cleanup

	// T3
	// Create a volume name
	// createa a volume name -> should fail
	// cleanup

	// T4
	// No volume
	// remove volume that doesn't exist -> should fail

	// T5
	// Remove volume in the middle of running -> should fail

	// T6
	// Attach a volume that does not exist -> should fail

	// T7
	// two containers attach to the same volume -> should fail

	// T8
	// Two containers trying to attach to the same volume -> should fail

	// how to test things?

	// spawn 10,000 containers.

	// set a limit of 100 volumes.
	// set a limit of 1000 containers
	//

	// id := containerMgr.runContainerCuda(volumeName)
	// containerMgr.stopContainer(id)
	// containerMgr.removeContainer(id)
	// containerMgr.removeVolume(volumeName, true)
}

// func TestImage(t *testing.T) {

// 	t.Log("Hello world")
// 	t.Error("F")

// 	cli, err := client.NewClientWithOpts(client.FromEnv)
// 	if err != nil {
// 		panic(err)
// 	}

// 	// Create a Docker volume
// 	containerMgr := NewContainerMgr(cli)
// 	volumeName := "my_volume1"

// 	containerMgr.createVolume(volumeName)
// 	id := containerMgr.runContainerCuda(volumeName)
// 	containerMgr.stopContainer(id)
// 	containerMgr.removeContainer(id)
// 	containerMgr.removeVolume(volumeName, true)
// }
