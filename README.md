# Mist

Mist: UTMIST's Compute Platform

## Runtime Requirements

Note that you will need the following installed:

- docker
- docker-compose
- go

## Run the local NVIDIA demo

With Docker Desktop running in Linux/WSL2 mode and NVIDIA GPU support available:

```powershell
docker compose -p mist-local -f docker-compose.local.yml up --build -d
```

Open http://localhost:5173/jobs and click **Run GPU benchmark**. The page detects
your actual GPU, queues a fixed PyTorch matrix multiplication workload, and shows
CPU/GPU timings, verified results, failures, and execution output. Matrix sizes
are limited to 1024, 2048, and 4096. Jobs run one at a time with a 90-second limit.
The CPU comparison uses four threads; timings exclude data transfer and are not
a general hardware performance rating.

This local demo runs fixed Python workloads inside the GPU-enabled app container
as a non-root user. It does not mount the host Docker socket, accept arbitrary
scripts, or download models. Only localhost port 5173 is exposed. Redis stores the
job history in a dedicated volume; the page shows the latest 50 jobs. The first
build needs several GB for the PyTorch image.

```powershell
# Stop the local app, keeping job history:
docker compose -p mist-local -f docker-compose.local.yml down
```

The CLI and the original distributed-worker prototype remain incomplete. This
demo is a single-machine runner; recovery of interrupted jobs after a crash is
not implemented. A refresh preserves job history, but does not retry interrupted
work. The legacy CPU container path is still a lifecycle smoke test.
