# Mist job lifecycle

## Kubernetes executor (default)

1. The API validates the image, script or command, accelerator count, CPU,
   memory, and deadline. It accepts Python/shell scripts up to 32 KiB or an
   explicit command array. An image allowlist is enforced.
2. Submission creates a unique `mist-...` Kubernetes Job in `mist`. The Job
   stores its submission and configured pilot owner in metadata. Scripts use
   a ConfigMap owned by the Job. Retries are disabled; the deadline is enforced
   by Kubernetes. Workload pods receive no service account token.
3. Kubernetes places CPU/NVIDIA jobs on the NVIDIA server and Tenstorrent
   jobs on QuietBox. NVIDIA requests reserve whole GPUs. Tenstorrent requests
   reference a board-count ResourceClaimTemplate; each pod gets a separate
   claim and CDI injects only its allocated board devices. One board contains
   two chips. Insufficient resources leave the job `Scheduled`.
4. Mist reads actual Job/Pod state and logs. States are `Scheduled`,
   `InProgress`, `Success`, `Failure`, and `Cancelled`. Exit status comes from
   the workload container; a nonzero exit is never reported as success.
5. Cancellation suspends the Job, stops its pods, and releases reservations.
   Cancellation time and the last 32 KiB of logs are kept in Job annotations.
   Repeated cancellation is safe. Finished jobs cannot be cancelled.
6. Workloads receive `MIST_JOB_ID` and `MIST_CHECKPOINT_DIR`, which points to
   `/checkpoints/<job-id>`. The CPU, NVIDIA, and TT profiles use separate
   node-local PVCs; job directories share the corresponding PVC. This is
   persistence across pods, not isolation between untrusted users.

There is no Job TTL. Retained Jobs preserve history across API restarts;
normal logs depend on retained pods and kubelet log retention. Removing Jobs,
pods, or PVCs can remove history, logs, or checkpoints. External metadata/log
archival and shared dataset storage remain future work.

## API

| Endpoint | Action |
| --- | --- |
| `POST /jobs` | Submit a validated job; returns `job_id` and initial job state |
| `GET /jobs` | List this pilot owner's managed jobs |
| `GET /jobs/<id>` | Actual status, placement, resources, exit code and active TT allocation |
| `GET /jobs/<id>/logs` | JSON containing recent stdout/stderr, up to 64 KiB |
| `POST /jobs/<id>/cancel` | Cancel a running or waiting job |
| `DELETE /jobs/<id>` | Alias for cancellation, preserving history |
| `GET /jobs/status?id=<id>` | Compatibility status endpoint |
| `GET /healthz` | Verify access to the Kubernetes Jobs API |

The owner is configured by `MIST_PILOT_OWNER`, not authenticated per request.
The API uses namespaced RBAC in the deployed pilot. It does not implement
login, credits, team quotas, priority admission, or distributed training.

## Legacy Docker/Redis executor

`MIST_EXECUTOR=docker` selects the older Scheduler/Supervisor implementation.
It stores jobs as Redis hashes and consumes Redis streams. Its execution
logic is incomplete and does not use the Kubernetes allocation described
above. Do not use its placeholder success results as proof of training.
