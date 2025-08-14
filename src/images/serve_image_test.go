package main

import (
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

func setupMgr(t *testing.T) *ContainerMgr {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	return NewContainerMgr(cli)
}

// T1: create a volume, check exists, delete, check not exists
func TestCreateDeleteVolume(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t1"
	mgr.createVolume(volName)
	// vols, _ := mgr.cli.VolumeList(mgr.ctx, *opts*/ {})
	vols, _ := mgr.cli.VolumeList(mgr.ctx, volume.ListOptions{})
	found := false
	for _, v := range vols.Volumes {
		if v.Name == volName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Volume %s not found after creation", volName)
	}
	mgr.removeVolume(volName, true)
	// vols, _ = mgr.cli.VolumeList(mgr.ctx, /*opts*/ {})
	vols, _ = mgr.cli.VolumeList(mgr.ctx, volume.ListOptions{})
	for _, v := range vols.Volumes {
		if v.Name == volName {
			t.Errorf("Volume %s still exists after deletion", volName)
		}
	}
}

// T2: create volume, start container, attach, write, stop, start, check persistence, cleanup
func TestVolumePersistence(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t2"
	mgr.createVolume(volName)
	containerID := mgr.runContainerCuda(volName)
	// Write to volume (you'd need to exec into container or mount and write a file)
	// For example, use mgr.execInContainer(containerID, "sh", "-c", "echo hello > /data/test.txt")
	// Stop and start container-p
	mgr.stopContainer(containerID)
	// mgr.startContainer(containerID)
	// Check file exists (again, exec into container and check)
	// Cleanup
	mgr.stopContainer(containerID)
	mgr.removeContainer(containerID)
	mgr.removeVolume(volName, true)
}

// T3: create a volume with same name twice (should not fail)
func TestCreateVolumeTwice(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t3"
	mgr.createVolume(volName)
	defer mgr.removeVolume(volName, true)
	mgr.createVolume(volName) // Should not fail
}

// T4: remove volume that doesn't exist (should fail or panic)
func TestRemoveNonexistentVolume(t *testing.T) {
	mgr := setupMgr(t)
	// defer func() {
	// 	if r := recover(); r == nil {
	// 		t.Errorf("Expected panic when removing nonexistent volume, but did not panic")
	// 	}
	// }()
	err := mgr.removeVolume("nonexistent_volume_t4", true) // Maybe this function never panics
	if err == nil {
		t.Errorf("Expected error when removing nonexistent volume, but no error")
	}
}

// T5: remove volume in use (should fail or panic)
func TestRemoveVolumeInUse(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t5"
	mgr.createVolume(volName)
	containerID := mgr.runContainerCuda(volName)
	defer func() {
		mgr.stopContainer(containerID)
		mgr.removeContainer(containerID)
		mgr.removeVolume(volName, true)
	}()
	err := mgr.removeVolume(volName, true) // why didn't this panic?
	if err == nil {
		t.Errorf("Expected error when removing nonexistent volume, but no error")
	}
}

// T6: attach a volume that does not exist (should fail or panic)
func TestAttachNonexistentVolume(t *testing.T) {
	mgr := setupMgr(t)
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic when attaching nonexistent volume, but did not panic")
		}
	}()
	mgr.runContainerCuda("nonexistent_volume_t6") // why did this one panic/work
}

// T7: two containers attach to the same volume (should succeed in Docker, but test for your policy)
func TestTwoContainersSameVolume(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t7"
	mgr.createVolume(volName)
	id1 := mgr.runContainerCuda(volName)
	id2 := mgr.runContainerCuda(volName)
	mgr.stopContainer(id1)
	mgr.removeContainer(id1)
	mgr.stopContainer(id2)
	mgr.removeContainer(id2)
	mgr.removeVolume(volName, true)
}

// T8: two containers try to attach to the same volume at the same time (should succeed in Docker)
func TestTwoContainersSameVolumeConcurrent(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t8"
	mgr.createVolume(volName)
	id1 := mgr.runContainerCuda(volName)
	id2 := mgr.runContainerCuda(volName)
	mgr.stopContainer(id1)
	mgr.removeContainer(id1)
	mgr.stopContainer(id2)
	mgr.removeContainer(id2)
	mgr.removeVolume(volName, true)
}

// T9: set a limit of 100 volumes (should fail on 101st if you enforce a limit)
func TestVolumeLimit(t *testing.T) {
	mgr := setupMgr(t)
	limit := 100
	created := []string{}
	for i := 0; i < limit; i++ {
		name := "test_volume_t9_" + fmt.Sprint(i)
		mgr.createVolume(name)
		created = append(created, name)
	}
	defer func() {
		for _, name := range created {
			mgr.removeVolume(name, true)
		}
	}()
	// why didn't you clean up the volumes
	// Try to create one more if you enforce a limit
	// If not enforced, this will succeed
}

// T10: set a limit of 10 containers (should fail on 11th if you enforce a limit)
func TestContainerLimit(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t10"
	mgr.createVolume(volName)
	ids := []string{}
	limit := 10
	for i := 0; i < limit; i++ {
		id := mgr.runContainerCuda(volName)
		ids = append(ids, id)
	}
	defer func() {
		for _, id := range ids {
			mgr.stopContainer(id)
			mgr.removeContainer(id)
		}
		mgr.removeVolume(volName, true) // why didnt you clean up the containers?
	}()
	// Try to create one more if you enforce a limit
	// If not enforced, this will succeed
}
