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
	return NewContainerMgr(cli, 10, 100)
}

// Create a volume, check exists, delete, check not exists
func TestCreateDeleteVolume(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t1"
	_, err := mgr.createVolume(volName)
	if err != nil {
		t.Errorf("Failed to create volume %s: %v", volName, err)
	}
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
	err = mgr.removeVolume(volName, true)
	if err != nil {
		t.Errorf("Failed to remove volume %s: %v", volName, err)
	}
	vols, _ = mgr.cli.VolumeList(mgr.ctx, volume.ListOptions{})
	for _, v := range vols.Volumes {
		if v.Name == volName {
			t.Errorf("Volume %s still exists after deletion", volName)
		}
	}
}

// Create a volume with same name twice (should not fail)
func TestCreateVolumeTwice(t *testing.T) {
	mgr := setupMgr(t)
	volName := "test_volume_t3"
	_, err := mgr.createVolume(volName)
	if err != nil {
		t.Errorf("Failed to create volume %s: %v", volName, err)
	}
	defer mgr.removeVolume(volName, true)
	_, err = mgr.createVolume(volName)
	if err != nil {
		t.Errorf("Failed to create volume %s a second time: %v", volName, err)
	}
}

// Remove volume that doesn't exist (should fail or panic)
func TestRemoveNonexistentVolume(t *testing.T) {
	mgr := setupMgr(t)
	err := mgr.removeVolume("nonexistent_volume_t4", true)
	if err == nil {
		t.Errorf("Expected error when removing nonexistent volume, but no error")
	} else {
		t.Logf("Correctly got error when removing nonexistent volume: %v", err)
	}
}

// Remove volume in use (should fail or panic)
func TestRemoveVolumeInUse(t *testing.T) {
	mgr := setupMgr(t)
	imageName := "pytorch-cuda"
	runtimeName := "nvidia"
	volName := "test_volume_t5"
	_, err := mgr.createVolume(volName)
	if err != nil {
		t.Fatalf("Failed to create volume %s: %v", volName, err)
	}
	containerID, err := mgr.runContainer(imageName, runtimeName, volName)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}
	defer func() {
		// Cleanup: stop and remove container, then remove volume
		if err := mgr.stopContainer(containerID); err != nil {
			t.Logf("Cleanup: failed to stop container %s: %v", containerID, err)
		} else {
			t.Logf("Cleanup: stopped container %s successfully", containerID)
		}
		if err := mgr.removeContainer(containerID); err != nil {
			t.Logf("Cleanup: failed to remove container %s: %v", containerID, err)
		} else {
			t.Logf("Cleanup: removed container %s successfully", containerID)
		}
		if err := mgr.removeVolume(volName, true); err != nil {
			t.Logf("Cleanup: failed to remove volume %s: %v", volName, err)
		} else {
			t.Logf("Cleanup: removed volume %s successfully", volName)
		}
	}()
	err = mgr.removeVolume(volName, true) // Should error: volume is in use by a running container
	if err == nil {
		t.Errorf("Expected error when removing volume in use, but no error")
	} else {
		t.Logf("Correctly got error when removing volume in use: %v", err)
	}
}

// Attach a volume that does not exist (should fail or panic)
func TestAttachNonexistentVolume(t *testing.T) {
	mgr := setupMgr(t)
	imageName := "pytorch-cuda"
	runtimeName := "nvidia"
	volName := "nonexistent_volume_t6"
	id, err := mgr.runContainer(imageName, runtimeName, volName)
	// If Docker auto-creates the volume, this may not error; check your policy
	if id != "" && err != nil {
		t.Errorf("Expected error when attaching nonexistent volume, but got id=%v, err=%v", id, err)
	} else if err != nil {
		t.Logf("Correctly got error when attaching nonexistent volume: %v", err)
	}
}

// Two containers attach to the same volume (should succeed in Docker, but test for your policy)
func TestTwoContainersSameVolume(t *testing.T) {
	mgr := setupMgr(t)
	imageName := "pytorch-cuda"
	runtimeName := "nvidia"
	volName := "test_volume_t7"
	_, err := mgr.createVolume(volName)
	if err != nil {
		t.Fatalf("Failed to create volume %s: %v", volName, err)
	}
	id1, err := mgr.runContainer(imageName, runtimeName, volName)
	if err != nil {
		t.Fatalf("Failed to start first container: %v", err)
	}
	id2, err := mgr.runContainer(imageName, runtimeName, volName)
	if err != nil {
		t.Fatalf("Failed to start second container: %v", err)
	}
	// Both containers should be able to use the same volume
	if err := mgr.stopContainer(id1); err != nil {
		t.Logf("Failed to stop first container: %v", err)
	}
	if err := mgr.removeContainer(id1); err != nil {
		t.Logf("Failed to remove first container: %v", err)
	}
	if err := mgr.stopContainer(id2); err != nil {
		t.Logf("Failed to stop second container: %v", err)
	}
	if err := mgr.removeContainer(id2); err != nil {
		t.Logf("Failed to remove second container: %v", err)
	}
	if err := mgr.removeVolume(volName, true); err != nil {
		t.Logf("Failed to remove volume %s: %v", volName, err)
	}
}

// Two containers try to attach to the same volume at the same time (should succeed in Docker)
func TestTwoContainersSameVolumeConcurrent(t *testing.T) {
	mgr := setupMgr(t)
	imageName := "pytorch-cuda"
	runtimeName := "nvidia"
	volName := "test_volume_t8"
	_, err := mgr.createVolume(volName)
	if err != nil {
		t.Fatalf("Failed to create volume %s: %v", volName, err)
	}
	id1, err := mgr.runContainer(imageName, runtimeName, volName)
	if err != nil {
		t.Fatalf("Failed to start first container: %v", err)
	}
	id2, err2 := mgr.runContainer(imageName, runtimeName, volName)
	if err2 != nil {
		t.Fatalf("Failed to start second container: %v", err2)
	}
	// This test does not actually run containers concurrently, but checks Docker's shared volume support
	if err := mgr.stopContainer(id1); err != nil {
		t.Logf("Failed to stop first container: %v", err)
	}
	if err := mgr.removeContainer(id1); err != nil {
		t.Logf("Failed to remove first container: %v", err)
	}
	if err := mgr.stopContainer(id2); err != nil {
		t.Logf("Failed to stop second container: %v", err)
	}
	if err := mgr.removeContainer(id2); err != nil {
		t.Logf("Failed to remove second container: %v", err)
	}
	if err := mgr.removeVolume(volName, true); err != nil {
		t.Logf("Failed to remove volume %s: %v", volName, err)
	}
}

// Set a limit of 100 volumes (should fail on 101st if you enforce a limit)
func TestVolumeLimit(t *testing.T) {
	mgr := setupMgr(t)
	limit := 100
	created := []string{}
	for i := 0; i < limit; i++ {
		name := "test_volume_t9_" + fmt.Sprint(i)
		_, err := mgr.createVolume(name)
		if err != nil {
			t.Fatalf("Failed to create volume %s: %v", name, err)
		}
		created = append(created, name)
	}
	name := "test_volume_fail"
	_, err := mgr.createVolume(name)
	if err == nil {
		t.Errorf("Volume limit not enforced")
	} else {
		t.Logf("Correctly got error when exceeding volume limit: %v", err)
	}

	defer func() {
		// Cleanup: remove all created volumes
		for _, name := range created {
			if err := mgr.removeVolume(name, true); err != nil {
				t.Logf("Cleanup: failed to remove volume %s: %v", name, err)
			}
		}
	}()
	// If your implementation doesn't enforce a limit, this test will fail
}

// Set a limit of 10 containers (should fail on 11th if you enforce a limit)
func TestContainerLimit(t *testing.T) {
	mgr := setupMgr(t)
	imageName := "pytorch-cuda"
	runtimeName := "nvidia"
	volName := "test_volume_t10"
	_, err := mgr.createVolume(volName)
	if err != nil {
		t.Fatalf("Failed to create volume %s: %v", volName, err)
	}
	ids := []string{}
	limit := 10
	for i := 0; i < limit; i++ {
		id, err := mgr.runContainer(imageName, runtimeName, volName)
		if err != nil {
			t.Fatalf("Failed to start container %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	_, err = mgr.runContainer(imageName, runtimeName, volName)
	if err == nil {
		t.Errorf("Container limit not enforced")
	} else {
		t.Logf("Correctly got error when exceeding container limit: %v", err)
	}
	defer func() {
		// Cleanup: stop and remove all containers, then remove the volume
		for _, id := range ids {
			if err := mgr.stopContainer(id); err != nil {
				t.Logf("Cleanup: failed to stop container %s: %v", id, err)
			}
			if err := mgr.removeContainer(id); err != nil {
				t.Logf("Cleanup: failed to remove container %s: %v", id, err)
			}
		}
		if err := mgr.removeVolume(volName, true); err != nil {
			t.Logf("Cleanup: failed to remove volume %s: %v", volName, err)
		}
	}()
}
