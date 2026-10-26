# Implementation plan: jobs, scheduling and execution

**Owner:** Brandon. **Scope:** k3s cluster, scheduler, executors,
`mist-job-entry`, job API endpoints. **Architecture:**
[ARCHITECTURE.md](../../../ARCHITECTURE.md),
[scheduler.md](../architecture/scheduler.md),
[executors.md](../architecture/executors.md).

## Goals

1. **Real GPU jobs on the A4000s as soon as possible** (M1). Everything on the
   critical path is ordered for that.
2. **Own the scheduler in depth:** queueing, limits, priority, fairshare,
   backfill and accounting. Understand why each works the way it does, and
   compare against Slurm's designs.
3. **Later, own the node side:** build the native node executor (cgroups, systemd
   units, device control, PTYs) and swap it in behind the interface without
   touching the scheduler.

## What I depend on, and how I avoid waiting

| Need | Owner | Stand-in until ready |
|---|---|---|
| Accounts (username, UID, team GID) and `identity.Authorize` | Lucas | `identity` interface + fixture accounts; a dev-only principal header in non-production builds |
| `/mist` layout on the TT box | Ansh | A script creating the layout for 2–3 test users and teams |
| Login gateway, relay, CLI shell | TBD | Run `mist` directly against the API with the dev principal |
| Runtime images | Scott (to confirm) | Minimal `gpu`/`cpu` images: `nvidia/cuda:12.x-runtime-ubuntu22.04` + Python + `uv` + `mist-job-entry` |
| 10 GbE link moved | Ansh / Ethan | Works over the 1 GbE link; only throughput changes |

## What I provide to others

| Interface | Consumers |
|---|---|
| Job endpoints and wire types ([api.md](../architecture/api.md)) | CLI, portal |
| `executor.Executor` + conformance suite | Future node executor |
| `mist-job-entry` | Runtime image owner (bakes it in) |
| `mist-nss` ConfigMap contract (passwd and group from accounts) | Identity |

## M1: GPU batch (critical path)

The steps are ordered by dependency; each ends with a check that it works.

1. **Skeleton.**
   - Set up `cmd/mistd` with `api` and `scheduler` subcommands.
   - Add `internal/db` with migrations embedded via `embed`, one directory per
     owner, and `internal/executor` with the interface.
   - Agree the layout with the team in one PR.
   - *Check:* `mistd scheduler` starts, migrates an empty Postgres, and holds the
     advisory lock. A second instance waits.
2. **Fake executor and clock.**
   - Build an in-memory `Executor` that "runs" jobs for scripted durations,
     emits events, and can simulate OOM, failures and lost nodes.
   - Inject the clock everywhere.
   - *Check:* scheduler tests run without a cluster.
3. **Scheduler core.**
   - Tables: `partitions`, `qos`, `limits`, `jobs`, `job_specs`, `job_events`,
     `allocations`, `nodes`.
   - The state machine, with every transition written as an event.
   - The cycle (event-triggered + 10 s tick).
   - The ledger and placement, in one transaction per admission.
   - Limits with pending reasons, FIFO within QoS, time-limit enforcement,
     cancel, and reconciliation.
   - *Check:* tests for two 1-GPU jobs running together, a third waiting with
     `RESOURCES`, the per-user limit giving `USER_LIMIT`, a timeout giving
     `TIMEOUT`, cancel in each state, and a lost execution giving `NODE_FAIL`.
4. **`mist-job-entry`.**
   - umask, private dot-folders, output header and footer, copying output to
     the file and stdout, SIGTERM forwarding, OOM detection from
     `memory.events`, and a termination message.
   - *Check:* tests that run it locally inside a cgroup with a small
     `memory.max`.
5. **K3s executor.**
   - A pod builder written as a pure function (`LaunchSpec` → Pod and
     ConfigMap), tested against golden files.
   - Informers on pods and nodes, the status mapping, `Logs`, reconciliation
     and RBAC.
   - The `mist-jobs` namespace, the NetworkPolicy and the `mist-nss` generator.
   - *Check:* golden tests, plus a run on a local k3d cluster using the `cpu`
     path.
6. **Job API endpoints.**
   - Submit (normalise with defaults, validate), list, get, cancel, log (live
     and tail), partitions, and queue (with anonymisation).
   - *Check:* API tests through the dev principal.
7. **Cluster.**
   - Reinstall k3s on the direct link and install the GPU Operator with only the
     device plugin and toolkit.
   - Label nodes, deploy Postgres and `mistd`, and pre-pull the images.
   - *Check:* `Executor.Nodes()` reports 2 GPUs on `utmist-z1opa08` and the CPU
     capacity of `utmist-tt`.
8. **Conformance on real hardware.**
   - Run the conformance suite against the K3s executor on both nodes.
   - *Check:* all the guarantees in [executors.md](../architecture/executors.md)
     pass. That includes another team's folder being absent, only one GPU being
     visible, an OOM giving `OUT_OF_MEMORY`, internal ranges being unreachable,
     and no service account token.
9. **First real job.**
   - Train a small model on one A4000 from a test user's folder, with a uv venv
     created in that folder. At the same time, run a second user's job on the
     other GPU, and a `cpu` download job.
   - *Check:* outputs and checkpoints are present, the `.out` files are
     complete, and the portal and API show correct states.

**M1 is done when** two real users, in two different teams, can each run a GPU
job at the same time from their own folders and can't see each other's files.
Every finished job must end in a correct terminal state with a readable
explanation.

## M2: Interactive

- `Attach` in the K3s executor (`pods/exec` over WebSocket, terminal resize,
  signals) and the interactive holder in `mist-job-entry`.
- `GET /v1/jobs/{id}/attach`, session tracking, and the reattach grace and
  idle cancel.
- The `interactive` QoS and its limits.
- *Check:* `mist shell --gpus 1` gives a shell that sees one GPU. Closing the
  laptop and running `mist attach` within 10 min resumes it; after 10 min the
  job is cancelled with a clear reason.

## M3: Fair scheduling

- **A simulator first:** replay synthetic workloads (job sizes, durations,
  arrival times by team) through the real scheduler, with the fake executor and
  a virtual clock. Report utilisation, wait times per team and starvation. Every
  policy change gets compared here before it ships.
- **Multifactor priority** and **fairshare.** Fairshare uses usage records with
  a 7-day half-life.
- **Backfill** with a reservation for the top blocked job.
- **Accounting records** (billing units), `mist usage`, and the admin endpoints
  for partitions, QoS, limits, nodes and holds.
- *Check:* in the simulator, a 2-GPU job is never starved by a stream of 1-GPU
  jobs. Two teams with equal shares converge to equal usage under contention.

## M4: Tenstorrent

- The `tt` partition, DRA board claims, hugepages, and the board-reset step
  before an allocation is released.
- *Check:* two 1-board TT jobs run side by side; a crashed job's board is reset
  before reuse.

## M5: Node executor

- **`mist-noded`:** a gRPC service over mTLS that starts systemd transient units
  through D-Bus (`github.com/coreos/go-systemd/v22/dbus`), with the properties
  table from [executors.md](../architecture/executors.md#node-executor-m5).
- **Device allocation by ID:** the scheduler's ledger records exact devices;
  `DeviceAllow` and `CUDA_VISIBLE_DEVICES` match them.
- **Filesystem view:** hide other teams' folders with `TemporaryFileSystem` and
  `BindPaths`.
- **Interactive sessions:** allocate a PTY in the agent and pass its file
  descriptors to the session unit.
- **Migrate one node:** drain it, exclude the device plugin, start the agent and
  run conformance.
- *Check:* the same conformance suite passes unchanged on a node run by the node
  executor.

## Learning notes

Read alongside each milestone:

| Milestone | Topic | Read |
|---|---|---|
| M1 | Kubernetes control loops | `client-go` informers, work queues; pod lifecycle and termination; device plugins |
| M1 | Scheduler state | Slurm job state codes and pending reasons (`squeue` docs); how Slurm handles node failure |
| M2 | Terminals | `pty(7)`; "The TTY demystified"; Kubernetes `remotecommand` |
| M3 | Policy | Slurm *Multifactor Priority*, *Classic Fairshare* and *Fair Tree*; `sched/backfill` and `bf_*` options; the Borg paper (scheduling sections) |
| M5 | Node isolation | Kernel `cgroup-v2.rst`; `systemd.resource-control(5)`, `systemd.exec(5)`; the systemd D-Bus API; Slurm `cgroup.conf` and `task/cgroup` (for comparison) |

## Implementation rules

- **One transaction per admission** (allocation + state change + node). Call the
  executor *after* commit. `Launch` must be safe to repeat, because a crash
  between commit and launch is fixed by retrying.
- **Never use Kubernetes to decide capacity.** The ledger decides; admission
  errors from Kubernetes are treated as a mismatch to report, not as feedback.
- **Inject the clock everywhere,** so tests and the simulator use virtual time.
- **Events arrive at least once:** handle each transition so a repeat has no
  effect.
- **Time limits run from `started_at`,** never from submit time.
- **Explain every wait:** a job left pending without a reason is a bug.

## Open questions

- Default limits once the first real usage is seen.
- Whether the CLI's job commands are written by me or the gateway owner. On the
  critical path I'll write them, then hand them over.
