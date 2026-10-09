# Job lifecycle and API

Current authenticated team execution. See [the operating guide](../docs/department-rollout.md#developer-api-contract)
for the complete API and [architecture](../docs/architecture.md) for source layout.

## Lifecycle

1. Authenticate the Better Auth session and select an active team. Validate image
   registry, workload, device count, CPU/memory/runtime and writing authority.
2. Create a suspended `mist-...` Job in `mist-team-<team-id>` and persist its
   submission/creator metadata. Scripts use a Job-owned ConfigMap.
3. A single durable controller applies fair team admission and resource limits.
   Kubernetes then schedules the pod using whole GPU resources or TT DRA claims.
4. Read actual Job/pod status, placement, logs and exit code. API states remain
   `Scheduled`, `InProgress`, `Success`, `Failure` and `Cancelled`. Scheduled
   includes queued/admitted/pending work; inspect the message/queue metadata.
5. Cancellation/revocation suspends the Job and removes pods, freeing allocated
   devices. It preserves outputs/history; explicit cancellation saves recent logs.
6. Dataset mounts are read-only `/inputs`. Outputs are the job's own model-pool
   directory at `/outputs` and `/checkpoints/<job-id>`. Finished outputs remain.

A team's timeout starts after admission, not while waiting in the Mist queue.
The controller is single-replica. Jobs have no automatic retry or TTL. Retained
pods/kubelet retention determine normal log availability; cancellation keeps the
last 32 KiB. Workloads receive no service-account token or arbitrary host mounts.

Historical account jobs and original local PVCs remain available through the
explicit legacy workspace. New deployed submissions/uploads require a team.
The former Redis/Docker executor and fake supervisor endpoints are retired.

## Job endpoints

Browser/CLI calls use the portal prefix `/api`; Nginx strips it before Go.
Team work sends `X-Mist-Team` with a real team ID. Go derives identity and authority
from the session, not request ownership fields.

| Method/path after `/api` | Action |
|---|---|
| `POST /jobs` | Submit; **201** with `job_id` and job metadata |
| `GET /jobs` | List permitted team jobs, or selected legacy account history |
| `GET /jobs/{id}` | Actual state, placement, resources, exit and active TT allocation |
| `GET /jobs/{id}/logs` | Recent stdout/stderr JSON, up to 64 KiB |
| `POST /jobs/{id}/cancel` | Authorized cancellation |
| `DELETE /jobs/{id}` | Cancellation alias, preserves history |
| `GET /jobs/{id}/files` | Output listing; `/files/download?path=...` downloads |
| `GET /jobs/status?id=...` | Compatibility status route using the same scoped executor |
| `GET /hardware`, `/images` | Capacity and image/runtime policy profiles |
| `GET /healthz` | Kubernetes Jobs API availability |

Scripts are limited to 32 KiB; request JSON is bounded and rejects unknown fields
or multiple objects. An explicit image with no command/arguments preserves its
ENTRYPOINT/CMD. CPU/NVIDIA preserve image WORKDIR without an override. TT requires
compatible libraries and an explicit host/container runtime choice when applicable.
Custom tagged images are self-service within allowed registry rules.
