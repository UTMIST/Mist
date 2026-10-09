# Local job foundation

Implementation branch: `feat/local-job-foundation`. Recorded October 8, 2026
(America/New_York). The preserved starting point is commit `2f9b178`.
The [execution plan](foundation-execution-plan.md) records scope and progress.

## What runs now

```text
Website :3001 / CLI
        |
        v
Mist API :3000  -- validates image, resources, and workload
        |
        v
k3s Job + pod  -- Kubernetes schedules; containerd runs the image
       / \
      v   v
NVIDIA PC       QuietBox
CPU / 2 GPUs    4 n300 boards / 8 chips
      |             |
      v             v
Per-job output folder on the selected node's persistent volume
```

Open **http://127.0.0.1:3001/jobs**. The **Machines** page shows the same real
inventory. Jobs support Python scripts, approved container images, and small
accelerator training checks. Dashboard/login and other prototype pages remain
separate work. Job execution uses Kubernetes, without SSH or a Docker daemon
inside the job. SSH is useful for administration and reaching the local website.

The current API serves one configured owner, `utmist`. This is a trusted local
pilot. Membership, login, credits, quotas, public deployment, shared datasets,
and browser result downloads are subsequent tasks.

## Use the website

1. Check hardware availability at the top of **Jobs**.
2. Select **Compute**: CPU, NVIDIA, or Tenstorrent.
3. For accelerators, request whole GPUs or whole boards. A board has two chips.
4. Select **Workload** and an approved **Container image**.
5. For **Container image** workloads, optionally provide an executable and
   arguments, one argument per line. Leaving them blank uses the image defaults.
6. **Resources and environment** contains CPU/RAM, deadline, working directory,
   and `NAME=value` environment variables. Save files under `/outputs`.
7. Submit. The job card shows its ID, state, assigned machine, resources,
   output path, and exit code. **Logs** reads actual container output.
   **Cancel** stops a running or waiting job and preserves recent logs.

An image must contain its own code and dependencies, or provide a runtime for
script mode. Requesting a GPU does not convert CPU code into accelerator code.
The form accepts an image reference; it does not upload/build Docker archives.

### Included examples

The current deployment approves and locally caches these two example images:

| Image | Website settings | What it checks |
| --- | --- | --- |
| `mist-training:cpu-v1` | CPU; Container image; blank command/arguments | Packaged regression code and dataset, default entrypoint and `/app` working directory, saved/reloaded model |
| `mist-training:nvidia-v1` | NVIDIA; Container image; arguments below | CUDA training and checkpoint reload on each allocated GPU, without CPU fallback |

For two GPUs, select **GPUs = 2** and enter these arguments, one per line:

```text
--backend
nvidia
--devices
2
```

For Tenstorrent, choose **Small training check** and one to four boards. This
uses the already tested TT image and installed host runtime. It trains a small
regression model on the chips of every reserved board. TT-NN gradients are
computed explicitly; this does not establish arbitrary PyTorch model or TT
autograd compatibility. Cross-machine/distributed training is separate work.

## API contracts

The [job lifecycle reference](../src/jobs.md) describes state and retention.
The browser uses `/api`; Vite proxies it to the API. The API itself has these
unprefixed routes:

| Route | Response |
| --- | --- |
| `GET /hardware` | `{observed_at, machines: [...], pools: [...]}` |
| `GET /images` | `{images: [{reference, accelerators}], profiles: [...]}` |
| `POST /jobs` | HTTP 201, `{job_id, job}` |
| `GET /jobs` | `{count, jobs: [...]}` |
| `GET /jobs/<id>` | Actual job state, resources, placement and output paths |
| `GET /jobs/<id>/logs` | `{logs}` |
| `POST /jobs/<id>/cancel` | Retained job with state `Cancelled` |
| `GET /healthz` | Jobs API connectivity and executor |

### Submission

```json
{
  "name": "my-training-job",
  "type": "command",
  "accelerator": "nvidia",
  "device_count": 1,
  "image": "mist-training:nvidia-v1",
  "args": ["--backend", "nvidia", "--devices", "1"],
  "cpu": "2",
  "memory": "2Gi",
  "timeout_seconds": 600,
  "env": {"GREETING": "my first job"}
}
```

| Field | Behavior |
| --- | --- |
| `name` | Optional display name, at most 128 bytes |
| `type` | `command` (default) or `training-smoke` |
| `accelerator` | `cpu` (default), `nvidia`, `tenstorrent` |
| `device_count` | CPU: 0; NVIDIA: 1–2; TT: 1–4 boards. Accelerator default is 1 |
| `image` | Exact server-approved reference. Required when using only image defaults |
| `command` / `args` | Optional arrays, at most 64 combined entries, each at most 8192 bytes. No implicit shell parsing |
| `working_directory` | Optional absolute container path. Custom CPU/NVIDIA containers preserve image WORKDIR when omitted |
| `script` / `script_name` | Optional Python/shell script, at most 32 KiB; filename only, `.py` or `.sh`. Cannot accompany `command` |
| `env` | Up to 32 entries, values at most 4096 bytes. Managed Mist/TT variables cannot be overridden |
| `cpu` | Kubernetes quantity, greater than zero and at most 8; request equals limit |
| `memory` | 64Mi–32Gi; TT minimum 2Gi; request equals limit |
| `timeout_seconds` | 10–86400; default 600; includes queue/startup time |

`command` replaces the image ENTRYPOINT; `args` replaces CMD. With neither,
the image's entrypoint and arguments run. Scripts and training checks use
their runtime profile and default `/tmp` working directory. TT uses `/tmp`
and its host Python/TT-NN runtime unless explicitly configured otherwise.
To use shell syntax, request it explicitly, for example
`"command": ["sh", "-c"], "args": ["python train.py && echo done"]`.

CPU defaults: 250m/256Mi. NVIDIA defaults: 2 CPU/2Gi. TT defaults: 2 CPU/4Gi
RAM plus 2Gi of 1Gi hugepages per board. Jobs have no retries. Images are
pulled with `IfNotPresent`; approval does not guarantee availability or
hardware compatibility. Invalid requests return HTTP 400 without creating
Jobs. Kubernetes failures include a message and actual exit code when available.

### Hardware inventory

Each machine reports `name`, internal `address`, `ready`, `schedulable`,
`cpu_allocatable`, and `memory_allocatable`. CPU/RAM are Kubernetes allocatable
capacity, not current utilization or remaining CPU/RAM.

Each pool reports `accelerator`, `node`, `unit`, `total`, `allocated`,
`available`, `ready`, and optional `message` / `chips_per_device`.

- NVIDIA: count scheduled nonterminal Pod reservations across all namespaces,
  including bound Pending and Terminating pods and effective init/sidecar
  requests. Unplaced queued pods do not consume a device.
- Tenstorrent: use current complete ResourceSlice generations and deduplicated
  ResourceClaim allocations across all namespaces. Claims reserve boards
  even before a pod starts. Administrative access claims do not consume devices.
- Unavailable/cordoned/tainted nodes or missing ready device drivers are not
  advertised as ready. Incomplete TT inventory has `available: null`.
- Failed discovery returns HTTP 503. The UI marks availability unknown;
  it does not invent zero allocations or free hardware.
- The known incorrect TT vendor memory capacity is ignored.

The snapshot is assembled from separate Kubernetes reads and can change before
submission. Kubernetes remains the allocation authority. Active TT `devices`
details can disappear from a completed job once its claim is released; the
request count and saved `allocation.json` remain useful records.

## Output persistence

An init container creates a job-specific directory before the workload starts.
The workload mounts only this directory at two aliases:

```text
Persistent volume / <job-id> /
                    ├── model/checkpoint files
                    └── results.json

Inside the workload:
  /outputs                      MIST_OUTPUT_DIR
  /checkpoints/<job-id>          MIST_CHECKPOINT_DIR
  <job-id>                      MIST_JOB_ID
```

Both mount paths refer to the same files. `output_directory` is `/outputs` for
new jobs; old retained jobs keep their original `checkpoint_directory` contract.
The per-job directory is writable by the supported root/nonroot images. Its
permissions and shared backing PVC are not an authenticated tenant boundary.
Writes elsewhere in the container are not saved results.

| Compute | PVC in `mist` | Physical node |
| --- | --- | --- |
| CPU | `mist-cpu-checkpoints` | `utmist-z1opa08` |
| NVIDIA | `training-nvidia-checkpoints` | `utmist-z1opa08` |
| Tenstorrent | `training-tenstorrent-checkpoints` | `utmist-tt` |

Find the host directory using the administrative kubeconfig:

```bash
k3s kubectl -n mist get pvc
k3s kubectl get pv
```

Read the selected PV's `spec.hostPath.path` and node affinity. On that node,
the files are under `<PV-host-directory>/<job-id>/`. A read-only helper Pod
can also mount the corresponding PVC on its node. The verification tool below
does this after training has exited, records SHA256 hashes/results, and deletes
its temporary helper Pods. There is no browser file download endpoint yet.

The local-path PVCs request 256Mi each; local-path does not enforce that as a
disk quota. Jobs/results are retained without TTL or automatic cleanup.
Normal logs are up to 64KiB and depend on pod/kubelet retention. Cancellation
stores up to 32KiB in Job annotations. API restarts preserve Kubernetes history.
Node/disk loss can lose outputs; shared storage and backups follow separately.

## Development and deployment

See the [deployment guide](../deploy/k3s/README.md#mist-api-cli-and-jobs-page)
for image build/import and startup. Existing services on this machine are:

```bash
systemctl --user status mist-api-forward mist-web
systemctl --user restart mist-api-forward mist-web
```

From a laptop with SSH access to the NVIDIA server, tunnel the local website:

```bash
ssh -L 3001:127.0.0.1:3001 utmist@100.73.139.66
```

Open `http://127.0.0.1:3001/jobs` on that laptop while the SSH connection is
open. A different laptop port, such as `-L 3002:127.0.0.1:3001`, avoids a local
port conflict. This does not require public ingress.

The API runs in `mist-system` with namespaced workload permissions and a
read-only inventory ClusterRole: list nodes, pods, ResourceClaims and
ResourceSlices. It cannot read Secrets, exec into pods, update nodes, or create
workloads in other namespaces. The browser contains no Kubernetes credentials.

Custom registry images require an administrator to update `MIST_ALLOWED_IMAGES`
with exact references, rebuild/reconfigure the API deployment as needed, and
verify image/code compatibility. Prefer immutable tags or digests. For a private
registry, create its image-pull Secret in workload namespace `mist`; set the
API's `MIST_IMAGE_PULL_SECRETS` to its name (or comma-separated names).
The API references the Secrets without reading their contents. Keep credentials
out of the job form, environment variables, and Git. K3s registry configuration
is a separate option for mirrors/private endpoints.

### Code map

| Area | Files |
| --- | --- |
| Request model and routing | `src/api.go`, `src/kubernetes.go` |
| Validation, Job construction, cancellation | `src/kubernetes.go` |
| Live inventory | `src/hardware.go` |
| Approved image/profile catalog | `src/images.go` |
| Output directory initialization | `src/outputs.go` |
| Batched job history reads | `src/job_states.go` |
| API types and client | `web-interface/src/api.ts` |
| Polling/recovery | `web-interface/src/hooks/usePolling.ts` |
| Jobs orchestration and form/cards/logs | `web-interface/src/components/JobsPage.tsx`, `components/jobs/*` |
| Shared inventory and Machines page | `components/HardwarePanel.tsx`, `components/MachinesPage.tsx` |
| Cluster deployment/RBAC | `deploy/k3s/mist-api.yaml` |
| Packaged examples | `deploy/k3s/examples/*` |

### Verification

```bash
go -C src test -run 'Test(Kubernetes|Hardware|GPURequest|CustomImage|TenstorrentCustom)' ./...
go -C cli test ./...
cd web-interface
npm test
npx tsc --noEmit
npm run build
```

The full legacy backend suite additionally requires its Docker images and
Redis. It is separate from the Kubernetes path.

Fresh live checks require both accelerators initially free, the approved
example images imported, Playwright/Chromium installed, and admin kubectl access:

```bash
export KUBECONFIG=/home/utmist/.kube/config
export MIST_PLAYWRIGHT_MODULE=/tmp/mist-browser-check/node_modules/playwright/index.mjs
node deploy/k3s/verify_foundation_browser.mjs /home/utmist/mist-foundation-results-new-run
python3 deploy/k3s/verify_foundation.py --artifacts /home/utmist/mist-foundation-results-new-run
```

Use one artifact directory for both commands and run them sequentially.
The browser check covers packaged CPU training, both NVIDIA GPUs, two TT
boards/four chips, real card states/logs, failure, and cancellation. The API
check covers occupied/free counts, queuing and reuse after cancellation for
both device types, command/argument semantics, and persistent files read from
new containers on both nodes. These tools submit real jobs and retain results.
Their final markers are `FOUNDATION_BROWSER_PASSED` and
`FOUNDATION_ALLOCATION_OUTPUTS_PASSED`.

Today's detailed evidence is saved under
`/home/utmist/mist-foundation-results-2026-10-08`. The checked-in
[verification summary](../deploy/k3s/foundation-results.json) records actual
job IDs, the deployed image ID, and completed checks. Browser and allocation/
persistence checks passed on October 8 EDT (October 9 UTC). A separate live
check verified image-pull error reporting, deadline failure, and retained
history across API restarts. The normal image approval list was restored.

Automated checks passed: 12 focused backend tests, the full src suite with its
local dependencies, CLI tests, 5 frontend interaction tests, type checking,
production build, and lint of changed frontend files. These are small model
execution checks, not a claim about every team model or distributed training.
See the execution plan's progress log and the developer handoff below.

## Developer handoff: next work

1. **Shared datasets and downloads (task 4):** storage on QuietBox, private
   NFS/shared mount on both nodes, upload limits/progress, stable dataset IDs,
   read-only inputs, job output listing/downloads, retention and backup rules.
   Preserve the `/outputs` contract and migrate the checkpoint alias explicitly.
2. **Team deployment/login (task 5):** production web hosting with `/api`
   routing, authenticated membership, job/file ownership, then team limits.
3. **Model/platform work:** portable TT image, real team models, scheduling
   fairness/credits/priority, monitoring, Jupyter and chaining as separate work.

Current Wi-Fi/LAN can support initial shared-storage tests. A second Ethernet
cable can improve throughput later. Do not remove QuietBox's existing router
link or format the NVIDIA machine's unmounted disk as part of this work.
