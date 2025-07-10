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
