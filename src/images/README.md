# README

### Prerequisites
- Nvidia runtime installed
- Docker engine running
- [container-structure-test](https://github.com/GoogleContainerTools/container-structure-test) installed

### Instructions to build and interact with container
```
docker build -t <image name> .
docker run -it --rm --gpus all <image name> bash
```
## Test
```
sh run_tests.sh
```
## Troubleshooting

### Unknown or invalid runtime name: nvidia
- Add this do /etc/docker/daemon.json 
```
{
  "default-runtime": "runc",
  "runtimes": {
    "nvidia": {
      "path": "/usr/bin/nvidia-container-runtime",
      "runtimeArgs": []
    }
  }
}
```

- restart docker engine: sudo systemctl restart docker


### nvidia-smi: command not found
- download [nvidia container toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html#installing-the-nvidia-container-toolkit)

# mist/images Package Documentation

## Overview

The `mist/images` package provides a `ContainerMgr` struct and related methods for managing Docker containers and volumes programmatically using the Docker Go SDK. It enforces limits on the number of containers and volumes, and provides safe creation, deletion, and lifecycle management.

---

## Main APIs

### `type ContainerMgr struct`
Manages Docker containers and volumes, enforces resource limits, and tracks active resources.

**Fields:**
- `ctx context.Context` — Context for Docker operations.
- `cli *client.Client` — Docker client.
- `containerLimit int` — Maximum allowed containers.
- `volumeLimit int` — Maximum allowed volumes.
- `containers map[string]struct{}` — Tracks active container IDs.
- `volumes map[string]struct{}` — Tracks active volume names.

---

### `func NewContainerMgr(client *client.Client, containerLimit, volumeLimit int) *ContainerMgr`
Creates a new `ContainerMgr` with the specified Docker client and resource limits.

---

### `func (mgr *ContainerMgr) createVolume(volumeName string) (volume.Volume, error)`
Creates a Docker volume with the given name, enforcing the volume limit.  
Returns the created volume or an error.

---

### `func (mgr *ContainerMgr) removeVolume(volumeName string, force bool) error`
Removes a Docker volume by name.  
Returns an error if the volume does not exist or is in use (unless `force` is true).

---

### `func (mgr *ContainerMgr) runContainerCuda(volumeName string) (string, error)`
Creates and starts a container with the specified volume attached at `/data`.  
Enforces the container limit.  
Returns the container ID or an error.

---

### `func (mgr *ContainerMgr) stopContainer(containerID string) error`
Stops a running container by ID.  
Returns an error if the operation fails.

---

### `func (mgr *ContainerMgr) removeContainer(containerID string) error`
Removes a container by ID and deletes it from the internal tracking map.  
Returns an error if the operation fails.

---

## Test Plan

The test suite (`serve_image_test.go`) covers the following scenarios:

- **T1:** Create a volume, check it exists, delete it, check it no longer exists.
- **T3:** Create a volume with the same name twice (should not fail).
- **T4:** Remove a volume that doesn't exist (should fail or return error).
- **T5:** Remove a volume in use (should fail or return error).
- **T6:** Attach a volume that does not exist (should fail or return error).
- **T7:** Two containers attach to the same volume (should succeed in Docker, but test for your policy).
- **T8:** Two containers try to attach to the same volume at the same time (should succeed in Docker).
- **T9:** Set a limit of 100 volumes (should fail on 101st if you enforce a limit).
- **T10:** Set a limit of 10 containers (should fail on 11th if you enforce a limit).

---

## Example Usage

```go
cli, _ := client.NewClientWithOpts(client.FromEnv)
mgr := NewContainerMgr(cli, 10, 100)

vol, err := mgr.createVolume("myvol")
if err != nil { /* handle error */ }

cid, err := mgr.runContainerCuda("myvol")
if err != nil { /* handle error */ }

_ = mgr.stopContainer(cid)
_ = mgr.removeContainer(cid)
_ = mgr.removeVolume("myvol", true)
```

---

## Notes

- All resource-creating methods enforce limits and track resources in maps for accurate management.
- All destructive operations (`removeVolume`, `removeContainer`) return errors for non-existent or in-use resources.
- The package is designed for integration with Docker and expects a running Docker daemon.

---

**For more details, see the source code and comments in `serve_image.go