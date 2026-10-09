# Mist job lifecycle

## Kubernetes executor (default)

1. The API validates the image, script or command, accelerator count, CPU,
   memory, and deadline. It accepts Python/shell scripts up to 32 KiB or an
   explicit command/argument arrays, or an explicitly selected image's default
   entrypoint. Custom CPU/NVIDIA containers preserve image WORKDIR by default;
   `working_directory` provides an absolute override. An image allowlist is enforced.
2. Submission creates a unique `mist-...` Kubernetes Job in `mist`. The Job
   stores its submission and authenticated member owner in metadata. Scripts use
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
6. The API creates a job directory on shared NFS storage before submission. The
   workload mounts only its own directory at `/outputs` and
   `/checkpoints/<job-id>`. A selected owned ready dataset is mounted read-only
   at `/inputs`. Managed environment variables identify these locations.
   Legacy jobs retain original local PVCs; copied results remain downloadable.


There is no Job TTL. Retained Jobs preserve history across API restarts;
normal logs depend on retained pods and kubelet log retention. Removing Jobs,
pods, or PVCs can remove history, logs, or checkpoints. External metadata/log
archival remains future work; shared datasets/results are implemented.

## API

| Endpoint | Action |
| --- | --- |
| `POST /jobs` | Submit a validated job; returns `job_id` and initial job state |
| `GET /jobs` | List this member's managed jobs |
| `GET /jobs/<id>` | Actual status, placement, resources, exit code and active TT allocation |
| `GET /jobs/<id>/logs` | JSON containing recent stdout/stderr, up to 64 KiB |
| `POST /jobs/<id>/cancel` | Cancel a running or waiting job |
| `DELETE /jobs/<id>` | Alias for cancellation, preserving history |
| `GET /jobs/status?id=<id>` | Compatibility status endpoint |
| `GET /healthz` | Verify access to the Kubernetes Jobs API |
| `GET /hardware` | Live node readiness and whole-device allocations/availability |
| `GET /images` | Server-approved references and compute profiles |

When `MIST_AUTH_URL` is configured, Better Auth sessions identify each member.
Go authorizes every data request and rejects anonymous/other-owner access.
`MIST_PILOT_OWNER` remains for explicitly local development and legacy owner
migration. Namespaced workload RBAC and the read-only inventory ClusterRole
limit the API's cluster permissions. Credits, team quotas, priority admission
and distributed training are subsequent work. See
[the complete guide](../docs/complete-foundation.md) for login and storage routes.

Full request fields, response semantics, image ENTRYPOINT/CMD behavior,
resource limits, private registry setup, and output access are documented in
[the local foundation guide](../docs/local-job-foundation.md#api-contracts).
Active TT allocation details may disappear once the DRA claim is released;
saved training allocation files remain on the corresponding PVC.

## Legacy Docker/Redis executor

`MIST_EXECUTOR=docker` selects the older Scheduler/Supervisor implementation.
It stores jobs as Redis hashes and consumes Redis streams. Its execution
logic is incomplete and does not use the Kubernetes allocation described
above. Do not use its placeholder success results as proof of training.
