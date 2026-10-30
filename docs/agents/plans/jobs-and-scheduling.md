# Implementation plan: jobs, scheduling and execution

**Owner:** Brandon. **Scope:** k3s cluster, scheduler, executors,
`mist-job-entry`, job API endpoints. **Architecture:**
[ARCHITECTURE.md](../../../ARCHITECTURE.md),
[scheduler.md](../architecture/scheduler.md),
[executors.md](../architecture/executors.md),
[implementation contracts and release gates](../architecture/implementation-contracts.md).

**Revision:** 27 October 2026. Read D18–D23 before implementation. The checklist
below must be completed with real integration evidence, not only fixture/golden
tests. This plan does not authorize removing old services or researcher files
without the deployment document's checks.

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
| Per-job `mist-nss` projection (required system entries, submitter/charged team only) | Identity |

## M1: GPU batch (critical path)

The steps are ordered by dependency; each ends with a check that it works.
Resolve the M1 gates in implementation-contracts.md before production rollout.
Development may proceed against fixtures, but M1-01 through M1-11 are release
requirements. Keep secrets and actual research data out of test fixtures.

1. **Skeleton.**
   - Set up `cmd/mistd` with `api` and `scheduler` subcommands.
   - Add `internal/db` with migrations embedded via `embed`, one directory per
     owner, and `internal/executor` with the interface.
   - Define shared Resources/Execution/Event types, immutable ExecutionID and
     SpecHash, execution receipts and per-job operation intent. Include batch
     Args and launch deadline; do not leave interface fields implicit.
   - Define transaction-aware Identity authorization/admission methods with a
     fixed lock order; owners keep their migrations/packages.
   - Agree the layout with the team in one PR.
   - *Check:* `mistd scheduler` starts, migrates an empty Postgres, and holds the
     advisory lock. A second instance waits.
   - *Status (27 Oct 2026):* implemented on `feat/jobs-scheduling-m1`; the check
     is `TestSchedulerMigratesAndHoldsLock` in `cmd/mistd`, plus a test that
     losing the lock connection stops the scheduler. Lock order is documented in
     `internal/db`. `internal/identity` holds only the contract Identity
     implements; `identitytest` is the fixture stand-in. Layout PR still needs
     team agreement.
2. **Fake executor and clock.**
   - Build an in-memory `Executor` that "runs" jobs for scripted durations,
     emits events, and can simulate OOM, failures and lost nodes.
   - Inject the clock everywhere. Model delayed/ambiguous create responses,
     duplicate/out-of-order events, observation outages and failed cleanup.
     Persist simulated receipts separately from notification delivery.
   - *Check:* scheduler tests run without a cluster.
   - *Status (28 Oct 2026):* `internal/clock` has `Fake` (timers and
     `AfterFunc` callbacks fire synchronously, in time order, inside `Advance`).
     `internal/executor/fake` runs scripted jobs on that clock and injects lost or
     late create responses, OOM, launch errors and timeouts, the backstop, SIGTERM
     versus SIGKILL, lost and recovered nodes, failed cleanup, and observation
     outages. Its durable receipts are kept separate from an event stream that
     tests can hold, drop, duplicate, reorder or lose on `Restart`. The scheduler
     tests that use it come with step 3.
3. **Scheduler core.**
   - Tables: `partitions`, `qos`, `limits`, `jobs`, `job_specs`, `job_events`,
     `allocations`, `nodes`, `partition_nodes`, operation intents, consumed
     event IDs and executor receipt/tombstone migrations through its package.
   - The state machine, including CLEANING and quarantined terminal allocations,
     with every transition written as an event. Persist stop cause before calls.
   - Recheck authorization/provisioning generations atomically with admission;
     integrate durable revocation of pending/launching/running jobs.
   - The cycle (event-triggered + 10 s tick).
   - The ledger and placement, in one transaction per admission.
   - Limits with pending reasons, FIFO within QoS, time-limit enforcement,
     cancel, and reconciliation.
   - *Check:* tests for two 1-GPU jobs running together, a third waiting with
     `RESOURCES`, the per-user limit giving `USER_LIMIT`, a timeout giving
     `TIMEOUT`, cancel in each state, and a lost execution giving `NODE_FAIL`.
     Cover M1-04/M1-05/M1-06: crash boundaries, split leadership, fast exit before
     Started, lost node with retained allocation and repeated cleanup receipts.
     Include shared-node and partition caps, not just accelerator counts.
   - *Status (30 Oct 2026):* implemented in `internal/scheduler`, with tables in
     `migrations/scheduler/0002`. `scheduler_test.go` covers every scenario
     listed in the check above, plus revocation, PROVISIONING/AUTH_UNAVAILABLE,
     stale generation before launch, ambiguous creates, and dropped or duplicated
     events. Writes are fenced by a leader epoch (scheduler.md). Still open:
     - **Unknown executions:** they are stopped, and their node is kept out of
       admission in memory, but not yet charged as quarantined occupancy.
     - **Executor receipt tables:** moved to step 5, since the fake keeps
       receipts in memory.
     - **Generation granularity:** the identity fixture's generation is global
       rather than per account, so any membership change cancels unsent
       launches. Identity's real implementation should scope it per account.
     - **Pending detail:** refreshed only when the pending reason changes.
4. **`mist-job-entry`.**
   - umask 0027, symlink-safe private dot-folders, output header/footer, copying output to
     the file and stdout, SIGTERM forwarding, OOM detection from
     `memory.events`, and a termination message.
   - *Check:* tests that run it locally inside a cgroup with a small
     `memory.max`. Verify the fallback when the wrapper itself is killed;
     report incomplete output without fabricating a successful footer. Test
     batch arguments and safe output opening inside the restricted view.
   - *Status (30 Oct 2026):* `cmd/mist-job-entry` (static binary) with logic in
     `internal/jobentry`.
     - **How it works:** the output file opens via `openat2(RESOLVE_BENEATH)`
       under the team root, regular files only. Dot-folders are opened
       `O_NOFOLLOW` and checked by `fstat`; an unsafe replacement fails setup.
       Setup failures exit 125 with a `launch_error` result and never run the
       work. Work runs in its own process group with a parent-death signal;
       the local deadline timer sends SIGTERM, then SIGKILL after grace.
     - **Tests:** the process tests re-execute the test binary as the real
       wrapper. They cover batch args with shell metacharacters, command exit
       codes, SIGTERM forwarding, both deadline paths, killing the wrapper (no
       footer, no result, work dies) and an unsafe output path. OOM runs in a
       64 MiB systemd scope with swap off and detects `oom_kill` from
       `memory.events`.
     - **Still open:** grandchildren that call `setsid` escape the group and
       rely on the executor tearing down the container or cgroup.
       `/dev/termination-log` wiring belongs to the step-5 Pod builder.
5. **K3s executor.**
   - A pod builder written as a pure function (`LaunchSpec` → Pod and
     ConfigMap), tested against golden files.
   - Informers on pods and nodes, the status mapping, `Logs`, reconciliation
     and RBAC.
   - The `mist-jobs` namespace, NetworkPolicy plus host filtering, strict groups,
     per-job `mist-nss`, a durable observation journal and cleanup reporter.
   - Persist results/tails before deletion; use matching node receipts before
     Exited/release. Missing Pod/API timeout must not erase execution history.
   - Pin deadline behavior and implement a work-start timer plus independent
     executor backstop; launch timeout is separate from work walltime.
   - *Check:* golden tests, plus a run on a local k3d cluster using the `cpu`
     path.
   - *Status (30 Oct 2026):* `internal/executor/k3s`, plus `deploy/k3s/` (the
     namespace, NetworkPolicy and RBAC) and `mistd cleanup-report`.
     - **Unit tests:** golden Pod and ConfigMap files with separate security
       invariants, and executor behaviour on client-go's fake clientset.
     - **Integration:** `scripts/k3d-test.sh` runs on k3s v1.36.5 (containerd
       2.3.4), as mistd's service account. It covers strict groups (only the
       team, `mist-users` and the personal group), other teams absent, no
       service-account token, a read-only root, writable scratch, umask 0027,
       the network blocked except the internet, SIGTERM forwarding, kernel
       OOM, the local time limit, setup failure as LAUNCH_FAILED, executor
       restart, narrow RBAC, and scheduler → k3s → node cleanup receipt →
       COMPLETED.
     - **Found:** NetworkPolicy is applied about a second after the containers
       start. Fixed with the isolation gate init container (executors.md).
     - **Still open:**
       - Cleanup receipts reach the executor through `RecordCleanup`; the
         authenticated node → API transport is step 6.
       - Orphan ConfigMap collection.
       - Throttling `Lost` until the node has been NotReady for 2 min
         *and* the scheduler has confirmed it.
       - GPU, NFS, host firewall and real-node cleanup, which are steps 7–8.
6. **Job API endpoints.**
   - Submit (normalise with defaults, validate), list, get, cancel, log (live
     and tail), partitions, and queue (with anonymisation).
   - Add submission idempotency, complete validation/authorization matrix,
     allowlisted queue summaries and cleanup/incomplete-output fields.
   - *Check:* M1-09 through dev fixtures and real session/relay auth. Production
     build must reject the development principal header.
   - *Status (30 Oct 2026):* `internal/api` and the `mistd api` wiring. The
     details are in [api.md](../architecture/api.md#implementation-notes-m1).
     - **Endpoints:**
       - `POST /v1/jobs`, which normalizes with database defaults and requires
         an `Idempotency-Key`;
       - `GET /v1/jobs` and `GET /v1/jobs/{id}`;
       - `POST /v1/jobs/{id}/cancel`;
       - `GET /v1/jobs/{id}/log`, including the `?follow=1` WebSocket;
       - `GET /v1/queue`, anonymised;
       - `GET /v1/partitions`;
       - `POST /internal/v1/executions/{id}/cleanup`, with host tokens.

       `mistd cleanup-report -post` delivers receipts.
     - **M1-09 tests on the fixture:**
       - A duplicate key gives one job, both through HTTP and inside the submit
         transaction. A conflicting body is rejected, and a replay survives a
         defaults change.
       - Non-team detail, log and cancel requests get `not_found`.
       - The lead, owner and admin rules follow the matrix, and former members
         lose access.
       - The queue and partition replies are free of other teams' identities,
         names, env and paths.
       - Hostile paths, env and argv are rejected and never stored.
       - Log streams close on revocation.
       - A production build refuses `-dev-fixture` and the dev header.

       Mutation checks confirm the tests catch removed authorization, queue
       redaction, the idempotency check, dev-header rejection, path
       confinement, protected env and node-scoped receipts.
     - **k3d:** `TestK3dAPIEndToEnd` submits over HTTP, admits, runs on k3s,
       reads the log through the API, and has the node post its own cleanup
       receipt to `/internal`. The job then reaches `COMPLETED` with the
       allocation released.
     - **Still open:**
       - Real session and relay auth (Identity) are not integrated, so the
         second half of the check waits for Lucas's authenticators.
       - Host-token issuing (Identity provisioning).
       - TLS on the direct link (step 7).
       - The scheduler role has no executors wired in `mistd` yet (step 7
         config).
7. **Cluster.**
   - Reinstall k3s on the direct link and install the GPU Operator with only the
     device plugin and toolkit.
   - Label nodes, deploy Postgres and `mistd`, and pre-pull actual pinned images.
   - Install/test cleanup reporters, gateway lease watchdog and host firewall;
     configure TLS, disk headroom and operational alerts. Save installed version
     manifest and capability checks. Integrate real Identity and provisioning.
   - *Check:* `Executor.Nodes()` reports 2 GPUs on `utmist-z1opa08` and the CPU
     capacity of `utmist-tt`.
8. **Conformance on real hardware.**
   - Run the conformance suite against the K3s executor on both nodes.
   - *Check:* all the guarantees in [executors.md](../architecture/executors.md)
     pass. That includes another team's folder being absent, only one GPU being
     visible, an OOM giving `OUT_OF_MEMORY`, internal ranges being unreachable,
     and no service account token. Execute M1-01 through M1-10 with real SSH,
     both local/remote mounts and both own-node/remote service probes. Fixture
     successes cannot substitute for gateway revocation and cleanup evidence.
9. **First real job.**
   - Train a small model on one A4000 from a test user's folder, with a uv venv
     created in that folder. At the same time, run a second user's job on the
     other GPU, and a `cpu` download job.
   - *Check:* outputs and checkpoints are present, the `.out` files are
     complete on normal exit, and the portal/API show correct states and mark
     exceptional incomplete output. Also verify a command job can create a venv
     without M2 interactive support.

**M1 is done when** two real users, in two different teams, can each run a GPU
job at the same time from their own folders and can't see each other's files.
Every finished job must converge to a correct terminal state with a readable
explanation; unresolved node cleanup remains explicitly quarantined. All M1
acceptance IDs must have evidence. Do not release on two successful GPU demos
alone. M1 FIFO may starve larger jobs; report this limitation until M3.

## M2: Interactive

- Persistent holder in `mist-job-entry`: one shell/PTY, job-private Unix socket,
  bounded replay and local detach timer. `pods/exec` runs a fixed attach helper
  without a second TTY; it never starts a new shell.
- `GET /v1/jobs/{id}/attach` proxies the holder's stream. One active attachment,
  explicit detach versus cancel, resize/signal controls and observed grace state.
  API/relay memory is not authoritative; walltime continues detached.
- The `interactive` QoS and its limits.
- *Check:* `mist shell --gpus 1` gives a shell that sees one GPU. Closing the
  laptop and running `mist attach` within 10 min resumes it; after 10 min the
  job is cancelled with a clear reason. Assert the same PID/cwd/variables and
  foreground process survive; repeat across API/relay restart, then holder crash.
  Check bounded output, concurrent attach conflict and nonzero shell exit.

## M3: Fair scheduling

- **A simulator first:** replay synthetic workloads (job sizes, durations,
  arrival times by team) through the real scheduler, with the fake executor and
  a virtual clock. Report utilisation, wait times per team and starvation. Every
  policy change gets compared here before it ships.
- **Multifactor priority** and **fairshare.** Fairshare uses usage records with
  a 7-day half-life.
- **Backfill** with a persistent reservation for the top eligible blocked job,
  respecting shared-node capacity, limits, signal grace and cleanup delays.
- **Accounting records** (billing units), `mist usage`, and the admin endpoints
  for partitions, QoS, limits, nodes and holds.
- *Check:* in the simulator, a 2-GPU job is never starved by a stream of 1-GPU
  jobs of lower priority while nodes/cleanup are healthy. Two teams with equal
  shares and equivalent eligible demand converge to equal usage under contention.
  Report strict interactive-QoS starvation separately. Crash/replayed events
  must not duplicate accounting intervals. Complete backup/restore gate too.

## M4: Tenstorrent

- The `tt` partition, DRA board claims, hugepages in the admission ledger and
  board-reset cleanup. After Mist admission use an exact node selector, never
  `nodeName`, so Kubernetes allocates claims on Mist's chosen node.
- Pin/test driver board/chip granularity and reset scope; record board IDs in M4,
  before M5. Stalled claims hit the launch deadline and release only after cleanup.
- *Check:* two 1-board TT jobs run side by side; a crashed job's board is reset
  before reuse; its neighbor is unaffected or concurrent TT jobs are disabled.
  Failed reset quarantines boards/node and never silently releases the count.

## M5: Node executor

- **`mist-noded`:** a gRPC service over mTLS that starts systemd transient units
  through D-Bus (`github.com/coreos/go-systemd/v22/dbus`), with the properties
  table from [executors.md](../architecture/executors.md#node-executor-m5).
- **Device allocation by ID:** the scheduler's ledger records exact devices;
  `DeviceAllow` and `CUDA_VISIBLE_DEVICES` match them.
- **Credentials:** trusted launcher explicitly clears/replaces supplementary
  groups, sets GID/UID and drops capabilities before user code; test a multi-team
  account. `SupplementaryGroups=` alone is insufficient.
- **Filesystem/network view:** isolate other teams, host processes/sockets and
  runtime paths; use a private network namespace with the same firewall contract.
- **Interactive sessions:** same persistent holder and PTY inside the main unit;
  reconnect from the agent rather than creating an independently limited unit.
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
  executor *after* commit. Persist the immutable spec/ExecutionID and retry
  only the same unstarted/ambiguous intent. Reconcile before replay; a missing
  execution with a recorded start is a loss, not permission to rerun.
- **Never use Kubernetes to decide capacity.** The ledger decides; admission
  errors from Kubernetes are a mismatch to report, not ordinary queue control.
  DRA is the explicit exception for device allocation after Mist selects the node.
- **Inject the clock everywhere,** so tests and the simulator use virtual time.
- **Events arrive at least once:** handle each transition so a repeat has no
  effect. Dedupe by stable event ID and validate ExecutionID/object UID; tolerate
  out-of-order delivery and recover from durable receipts.
- **Never free on process exit or Pod NotFound.** Require cleanup evidence and
  resolution of ambiguous in-flight launch operations; quarantine otherwise.
- **Time limits run from `started_at`,** never from submit time.
- **Explain every wait:** a job left pending without a reason is a bug.

## Open questions

- Default limits once the first real usage is seen.
- Whether the CLI's job commands are written by me or the gateway owner. On the
  critical path I'll write them, then hand them over.
