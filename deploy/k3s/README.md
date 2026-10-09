# Accelerator jobs on k3s

For the original NVIDIA-node setup and recovery procedure, read the
[setup runbook](setup-runbook.md). Its final section records the QuietBox worker
join on 2026-10-03.

The NVIDIA server runs Ubuntu 22.04, k3s 1.36, and two RTX A4000 GPUs. The host
driver is pre-installed. NVIDIA GPU Operator v26.7.1 is configured with
[`nvidia-gpu-operator-values.yaml`](nvidia-gpu-operator-values.yaml). Pin the
chart version when installing or upgrading:

```bash
sudo env KUBECONFIG=/etc/rancher/k3s/k3s.yaml helm upgrade --install gpu-operator \
  oci://nvcr.io/nvidia/cloud-native-charts/gpu-operator \
  --version v26.7.1 --namespace gpu-operator --create-namespace \
  --values deploy/k3s/nvidia-gpu-operator-values.yaml --wait
```

Run from the repository root. Check that the operator pods are healthy and the
node advertises two GPUs before submitting a workload:

```bash
sudo k3s kubectl -n gpu-operator get pods
sudo k3s kubectl describe node utmist-z1opa08
```

The `Allocatable` section must include `nvidia.com/gpu: 2`. Then run the
single-GPU CUDA smoke test:

```bash
sudo k3s kubectl create namespace mist
sudo k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
sudo k3s kubectl -n mist wait --for=condition=complete job/gpu-smoke --timeout=180s
sudo k3s kubectl -n mist logs job/gpu-smoke
```

The expected log contains `Test PASSED`. To rerun the Job, delete only the
finished smoke Job and apply the manifest again:

```bash
sudo k3s kubectl -n mist delete job gpu-smoke
sudo k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
```

No Service or NodePort is needed for a batch Job. This test verifies that k3s
can allocate an NVIDIA GPU and execute CUDA. Mist now submits these resources
through its Kubernetes executor; see the [API setup](#mist-api-cli-and-jobs-page).

The cluster also has `utmist-tt` as an agent at `10.0.0.112`. Tenstorrent
DRA allocation is installed there: four n300 boards, each containing two
Wormhole chips. The NVIDIA smoke Job remains specific to `utmist-z1opa08`.

## Tenstorrent allocation

TT-Operator **0.3.0** installs Fabric Manager **0.2.30** and the DRA driver
**0.0.59**, configured by
[`tenstorrent-operator-values.yaml`](tenstorrent-operator-values.yaml).
The existing host KMD/firmware and the existing NFD installation are retained;
driver management, duplicate NFD, telemetry, JobSet, and PMIx are disabled.
The manually verified `tenstorrent.com/has-tt=true` node label enables discovery.

To reproduce the installation from the repo root:

```bash
export KUBECONFIG=/home/utmist/.kube/config
k3s kubectl label node utmist-tt tenstorrent.com/has-tt=true --overwrite
helm upgrade --install tt-operator oci://ghcr.io/tenstorrent/helm/tt-operator \
  --version 0.3.0 --namespace tt-operator-system --create-namespace \
  --values deploy/k3s/tenstorrent-operator-values.yaml
k3s kubectl apply -f deploy/k3s/tenstorrent-workload-policy.yaml
k3s kubectl -n tt-operator-system get pods
k3s kubectl get deviceclasses,resourceslices
k3s kubectl get resourceslices -o yaml
```

All three operator pods must be Ready. The `tenstorrent.com` DeviceClass
and a ResourceSlice on `utmist-tt` must exist, with **four** `n300` devices
whose `chipCount` is **2**. ResourceSlices are the inventory; claims show
reservations. TT does not appear as an `nvidia.com/gpu` node resource.

The supported reservation unit is a whole **n300 board**, including its
remote chip. Jobs can reserve one to four boards; separate jobs can use
the remaining boards. A request waits if insufficient boards or other
requested resources are free. Each pod gets its own ResourceClaim through
a ResourceClaimTemplate, and only its allocated device nodes are injected
through CDI. Boards are released when the pod finishes, even while its
completed Job and checkpoints are retained.

[`training-tenstorrent-job.yaml`](training-tenstorrent-job.yaml) now requests
one board, runs without privileged access, and reserves 2 GiB of 1 GiB
hugepages. The host still has 16 GiB of such hugepages. It mounts the existing
TT-Metal checkout and Python environment read-only, so it is specific to
this QuietBox rather than a portable training image.

For uniquely named training checks and separate checkpoint directories,
use [`submit_tenstorrent_smoke.py`](submit_tenstorrent_smoke.py). It needs
Python PyYAML, available on the server. For example:

```bash
export KUBECONFIG=/home/utmist/.kube/config
python3 deploy/k3s/submit_tenstorrent_smoke.py my-tt-test-a --boards 1
python3 deploy/k3s/submit_tenstorrent_smoke.py my-tt-test-b --boards 1
k3s kubectl -n mist get jobs,pods,resourceclaims
k3s kubectl -n mist logs -f job/my-tt-test-a
```

Use `--boards 2` for four chips, or `--boards 4` for all eight. The script
submits the small regression test described below. A different model needs
compatible Tenstorrent training code. Claim count, expected chip count,
hugepage reservation, and checkpoint directory must all match the job.
These tests train on each allocated chip independently; they do not perform
distributed training of one model. For multiple boards, the check launches
one runtime per board using its PCI address in `TT_VISIBLE_DEVICES`. This
avoids assuming that an arbitrary selected subset forms a connected mesh.
The scheduler reserves boards but does not promise their fabric topology;
a model spanning boards needs an appropriate topology and training runtime.

The admission policy applies to pods in `mist`, including init and ephemeral
containers. It rejects privileged access, added capabilities, host PID/IPC,
arbitrary host mounts, and writable mounts of the host runtime. This blocks
the previous privileged whole-machine test from being submitted again.
Individual character-device mounts remain allowed for the verification
probes, with access to unclaimed devices denied by device cgroups. The policy
is a guard for managed workloads, not a complete security boundary for
untrusted tenants or cluster administrators; hugepage mounts are host-shared.

### Allocation acceptance test

On 2026-10-03, four jobs reserved distinct boards and trained concurrently
on all eight chips. Their first-chip training intervals overlapped for
**3.924 seconds**. Each job could open its own device and received permission
errors opening the other three, even when the other character nodes were
mounted for the probe. A fifth job remained Pending with `cannot allocate
all claims`, then started automatically after a board was released. All five
jobs passed training, held-out-data, and checkpoint reload checks. Automatic
claim cleanup left all four boards free afterward.

To repeat that live test (it requires all four boards to be free):

```bash
export KUBECONFIG=/home/utmist/.kube/config
python3 deploy/k3s/verify_tenstorrent_allocation.py \
  --artifacts /home/utmist/k3s-allocation-results-new-run
```

The test holds initialized jobs at a shared-file barrier to inspect their
reservations, checks actual device access, observes the fifth job queuing,
and then releases the barrier. It verifies overlapping training timestamps,
loss thresholds, checkpoint files, and board reuse. Test jobs are retained;
the temporary PVC reader is deleted. The barrier times out after five
minutes if interrupted. The checkpoint PVC is retained.

Operator restart recovery and an NVIDIA CUDA regression check also passed.
See [`tenstorrent-allocation-results.json`](tenstorrent-allocation-results.json)
for pinned image IDs and results. Logs, claim/queue snapshots, admission
checks, and checkpoint archives are in
`/home/utmist/k3s-allocation-results-2026-10-03` on the NVIDIA server.
The vendor currently advertises an incorrect board-memory capacity in the
ResourceSlice; these claims select board type/count and do not use that
capacity for placement. Verify/correct it before adding memory-based selectors.

## Training smoke jobs

[`training_smoke.py`](training_smoke.py) learns a 32-input/32-output linear
mapping from 128 synthetic samples and evaluates 64 held-out samples. Each
device must complete 100 gradient updates, reduce training MSE by at least
98%, achieve held-out MSE below `0.0005`, change its weights, and reload its
checkpoint successfully. NVIDIA uses PyTorch CUDA autograd. Tenstorrent uses
TT-NN for forward passes, explicit analytic gradients, and weight updates;
CPU Torch creates the data, reads metrics, and saves checkpoints. This does
not validate automatic differentiation or arbitrary models on Tenstorrent.

The NVIDIA Job reserves both GPUs and tests each independently. The current
Tenstorrent manifest reserves one n300 board and tests its two chips
independently. The original eight-chip privileged Job, named
`training-tenstorrent`, is retained only as historical evidence; the new
Job is `training-tenstorrent-dra` and uses DRA/CDI reservations.

Run from the repository root:

```bash
sudo k3s kubectl -n mist create configmap training-smoke-script \
  --from-file=deploy/k3s/training_smoke.py --dry-run=client -o yaml \
  | sudo k3s kubectl apply -f -
sudo k3s kubectl apply -f deploy/k3s/training-nvidia-job.yaml
sudo k3s kubectl apply -f deploy/k3s/training-tenstorrent-job.yaml
sudo k3s kubectl -n mist logs -f job/training-nvidia
sudo k3s kubectl -n mist logs -f job/training-tenstorrent-dra
sudo k3s kubectl -n mist get jobs,pvc
```

Each successful log ends with `TRAINING_PASSED`. Checkpoints and
`results.json` are stored in the corresponding `training-*-checkpoints` PVC,
with the one-board TT check under its `dra-single` directory,
which uses storage local to the selected node. Preserve the PVCs when
removing a completed Job. Delete only the completed Job before applying its
manifest to rerun it; retained checkpoint filenames will be overwritten.
The TT manifest depends on QuietBox's existing host paths, hugepages, and
TT-Metal checkout and is not a portable training image.

The original whole-node Jobs passed on 2026-10-03, with exit code zero. All ten devices changed
weights, met the held-out-data threshold, and reloaded their saved
checkpoints. The archives were also checked after the Pods completed.

| Backend | Devices tested independently | Initial training MSE | Final training MSE | Held-out MSE |
| --- | --- | --- | --- | --- |
| NVIDIA RTX A4000, PyTorch FP32 | 2 GPUs | 0.319374 | 0.000000201866 | 0.000000932067 |
| Wormhole, TT-NN BF16 | 8 chips | 0.319374 | 0.0000623871 | 0.000175072 |

Precision and implementations differ; these figures establish convergence
and are not a performance comparison. Exact image IDs, the TT-Metal host
commit, Job timestamps, artifact locations, and acceptance thresholds are in
[`training-smoke-results.json`](training-smoke-results.json). Local logs and
checkpoint archives are retained under
`/home/utmist/k3s-training-results-2026-10-03` on the NVIDIA server. The
completed Kubernetes Jobs and their checkpoint PVCs are retained too.


## Mist API, CLI and Jobs page

The API now uses Kubernetes by default and runs in `mist-system`. Its service
account manages Jobs and script ConfigMaps in `mist`, reads Pod logs/events
and TT claims, and has no cluster administrator credentials. The configured
pilot owner is `utmist`. No authentication or public ingress is configured.
The foundation also lists node/pod/claim/slice state for live hardware inventory.
See [the foundation guide](../../docs/local-job-foundation.md) for contracts,
website instructions, persistent outputs, and the developer handoff.

### Build and install on the current server

Requires Go 1.25.1, Docker, k3s, and the private administrative kubeconfig.
Use the already installed GPU/TT operators described above. The API image is
built locally and imported into k3s containerd; `imagePullPolicy: Never`
means that it must be present on the selected server before deployment.

```bash
export KUBECONFIG=/home/utmist/.kube/config
CGO_ENABLED=0 go -C src build -o ../bin/mist-api .
docker build -f deploy/k3s/Dockerfile.api -t mist-api:foundation-20261008 .
docker build -f deploy/k3s/examples/Dockerfile.training \
  -t mist-training:cpu-v1 deploy/k3s/examples
docker build -f deploy/k3s/examples/Dockerfile.training \
  --build-arg BASE_IMAGE=pytorch/pytorch:2.5.1-cuda12.4-cudnn9-runtime \
  -t mist-training:nvidia-v1 deploy/k3s/examples
docker save -o /tmp/mist-foundation-images.tar \
  mist-api:foundation-20261008 mist-training:cpu-v1 mist-training:nvidia-v1
sudo k3s ctr -n k8s.io images import --platform linux/amd64 /tmp/mist-foundation-images.tar
k3s kubectl apply -f deploy/k3s/mist-api.yaml
k3s kubectl apply -f deploy/k3s/mist-tenstorrent-claims.yaml
k3s kubectl apply -f deploy/k3s/tenstorrent-workload-policy.yaml
k3s kubectl -n mist create configmap training-smoke-script \
  --from-file=deploy/k3s/training_smoke.py --dry-run=client -o yaml \
  | k3s kubectl apply -f -
k3s kubectl -n mist-system rollout status deployment/mist-api --timeout=120s
```

The API manifest creates namespaces, RBAC and all three checkpoint PVCs
without submitting a training Job. If rebuilding the same local image tag,
import the new image and run `rollout restart deployment/mist-api` in
`mist-system`. Preserve Jobs and PVCs when restarting the API.

For a manual local endpoint:

```bash
k3s kubectl -n mist-system port-forward service/mist-api 3000:3000 --address 127.0.0.1
```

Then in another terminal:

```bash
curl http://127.0.0.1:3000/healthz
go -C cli build -o ../bin/mist .
bin/mist job list --all
cd web-interface
npm install --package-lock=false
npm run dev -- --host 127.0.0.1 --port 3001 --strictPort
```

Open http://127.0.0.1:3001/jobs. Choose CPU, NVIDIA GPUs or Tenstorrent boards;
submit a Python script, select an approved **Container image** with its own
entrypoint/arguments, or choose **Small training check** for an accelerator.
The page displays actual placement, queue messages, exit status and logs.
Hardware availability is live; the Machines route uses the same inventory.
Cancel stops a waiting/running workload and retains cancellation history.
CLI commands are in [the CLI guide](../../cli/docs/setup.md).

The installed user units keep both local services running during the user's
systemd session. Their paths target this server and need adjustment on another
host; they do not enable user lingering or guarantee operation before login.

```bash
mkdir -p ~/.config/systemd/user
cp deploy/k3s/mist-api-forward.service deploy/k3s/mist-web.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now mist-api-forward mist-web
systemctl --user status mist-api-forward mist-web
```

Do not run manual listeners on the same ports while these units are active.
Stop them with `systemctl --user stop mist-web mist-api-forward` if needed.

### Submission examples

```bash
curl -sS http://127.0.0.1:3000/jobs -H 'Content-Type: application/json' \
  -d '{"name":"hello","accelerator":"cpu","command":["python","-c","print(42)"]}'
curl -sS http://127.0.0.1:3000/jobs -H 'Content-Type: application/json' \
  -d '{"name":"one-board","type":"training-smoke","accelerator":"tenstorrent","device_count":1}'
curl -sS http://127.0.0.1:3000/jobs -H 'Content-Type: application/json' \
  -d '{"name":"one-gpu","type":"training-smoke","accelerator":"nvidia","device_count":1}'
```

CPU jobs request no accelerator. NVIDIA requests accept one or two GPUs;
TT requests accept one to four boards, each containing two chips. Defaults
are 250m CPU/256Mi RAM for CPU jobs, 2 CPU/2Gi for NVIDIA, and 2 CPU/4Gi plus
2Gi hugepages per TT board. CPU and memory limits equal requests. The API
accepts CPU up to 8, memory 64Mi to 32Gi (TT minimum 2Gi), and deadlines from
10 seconds to 24 hours, default 600 seconds. Deadlines include queue time.
Images must be approved by the API's allowlist; submissions cannot specify
privileged access or host mounts. Environment variables `MIST_JOB_ID`,
`MIST_OUTPUT_DIR=/outputs`, and `MIST_CHECKPOINT_DIR=/checkpoints/<job-id>`
provide the job identity and two aliases for its persistent directory.

Storage remains node-local with a shared backing PVC per compute profile.
New workloads mount only their job-specific subdirectory. Retained Jobs
provide history across API restarts, without TTL. Normal logs remain in pod
logs; cancellation preserves their last 32KiB in the Job metadata. Keep the
Pods/PVCs or archive them before cleanup. This is a local single-owner pilot,
not an authenticated multi-user deployment.

### Custom images and private registries

The deployment's `MIST_ALLOWED_IMAGES` approves exact image references;
`GET /images` exposes them to the website. For real workloads, build/push an
image containing code/dependencies, approve its immutable tag/digest, and let
the selected node pull it. Local example tags must be imported on the NVIDIA
server. An approved image is not guaranteed to be pullable or CUDA compatible.
The TT profile still requires its tested image and read-only host runtime.

CPU/NVIDIA container jobs preserve image ENTRYPOINT/CMD and WORKDIR when no
override is supplied. Arguments are array entries, not a parsed shell command.
Scripts and training checks retain their tested runtime profiles. Write results
to `/outputs`; files elsewhere in the container are not persistent outputs.

For private images, an administrator creates a `kubernetes.io/dockerconfigjson`
Secret in workload namespace `mist`, then adds `MIST_IMAGE_PULL_SECRETS` to
the API Deployment environment with the Secret name(s), comma-separated.
Jobs receive those names as `imagePullSecrets`. Mist does not read Secret
contents or accept registry passwords in the submission form. Keep Secret
manifests/credentials outside Git. K3s node registry configuration is another
option: [private registries](https://docs.k3s.io/installation/private-registry).

### Foundation acceptance

With both accelerators free and the example images imported, run the browser
story, then the allocation/output checks using the same artifact directory:

```bash
export KUBECONFIG=/home/utmist/.kube/config
export MIST_PLAYWRIGHT_MODULE=/tmp/mist-browser-check/node_modules/playwright/index.mjs
node deploy/k3s/verify_foundation_browser.mjs /home/utmist/mist-foundation-results-new-run
python3 deploy/k3s/verify_foundation.py --artifacts /home/utmist/mist-foundation-results-new-run
```

Playwright and Chromium are test tooling, separate from the web application.
Set `MIST_PLAYWRIGHT_MODULE` to its installed `index.mjs`, or install Playwright
where Node can resolve it. `MIST_WEB_URL` changes the browser endpoint;
`--url` changes the API endpoint for the Python tool.
Checks cover packaged CPU/NVIDIA images, both GPUs, TT training, real UI
states/logs/cancellation, occupied/free counts, queues, reuse, and saved files
read by fresh containers on both nodes. Temporary read-only output probes are
removed; test Jobs and result directories remain. The final markers are
`FOUNDATION_BROWSER_PASSED` and `FOUNDATION_ALLOCATION_OUTPUTS_PASSED`.
October 8 evidence is under `/home/utmist/mist-foundation-results-2026-10-08`.
See [the checked-in summary](foundation-results.json) for actual job IDs,
deployed image ID, and completed checks.

### API acceptance test

This test needs both GPUs and all four TT boards available. It submits 17
uniquely named jobs, holds initialized devices at barriers to prove separate
reservations, verifies queued jobs, training/checkpoint reload, CPU failures,
deadlines, retained cancellation logs, and cancellation freeing all TT boards.

```bash
export KUBECONFIG=/home/utmist/.kube/config
python3 deploy/k3s/verify_mist_api.py \
  --artifacts /home/utmist/mist-api-results-new-run
```

Successful output ends with `MIST_API_PASSED`. Jobs and checkpoint directories
are retained. A barrier times out after five minutes if interrupted. The
verification script uses the operator's administrative context to inspect
claims/devices and release test barriers; the Mist API itself cannot exec.
Results from 2026-10-03 are in [mist-api-results.json](mist-api-results.json),
with raw evidence and checkpoint archives under
`/home/utmist/mist-api-results-2026-10-03`. These checks establish small model
training and allocation, not large/distributed-model support or performance.
