# Executors

An executor turns an admitted job into processes on a node, streams their I/O,
and reports what happened. The scheduler decides; the executor carries the
decision out. Overview: [ARCHITECTURE.md](../../../ARCHITECTURE.md). Related
decisions: D1, D2, D13, D16, D17.

There are two executors:

- the **K3s executor**, used from M1
- the **node executor** (M5), which runs jobs as systemd units without
  containers

Both implement the same interface and pass the same conformance tests. Each node
is managed by exactly one executor.

## How it fits

Both executors take the same `LaunchSpec` and end at the same `mist-job-entry`.
Only the middle differs:

```mermaid
flowchart LR
    S[Scheduler] -->|LaunchSpec| I{{Executor interface}}
    I --> K["K3s executor (M1)"]
    I -.-> NE["Node executor (M5)"]

    K -->|client-go| KAPI[k3s API server]
    KAPI --> Pod["Pod job-ID<br/>uid, team gid, nodeName,<br/>requests = limits, no token"]
    KAPI --> CM["ConfigMap job-ID<br/>script + job.json"]
    Pod --> Vol["Volumes: team folder, software,<br/>scratch, /dev/shm, mist-nss"]
    Pod --> Dev["Device plugin / DRA<br/>allocated GPUs or boards"]
    Pod --> Entry[mist-job-entry]

    NE -.->|gRPC + mTLS| Agent[mist-noded]
    Agent -.->|D-Bus| Unit["systemd unit mist-job-ID<br/>User, DeviceAllow, MemoryMax, BindPaths"]
    Unit -.-> Entry

    Entry --> Work[Script, command or shell]
    K -->|"Started, Exited, Lost"| S
```

## Interface

```go
package executor

type Executor interface {
    Name() string

    // Nodes reports schedulable capacity and health for nodes this executor manages.
    Nodes(ctx context.Context) ([]Node, error)

    // Launch starts the job on spec.Node. Idempotent per spec.JobID.
    Launch(ctx context.Context, spec LaunchSpec) error

    // Terminate sends SIGTERM, then SIGKILL after grace. Idempotent.
    Terminate(ctx context.Context, jobID int64, grace time.Duration, reason string) error

    // Attach connects a terminal to a running interactive job; returns when the session ends.
    Attach(ctx context.Context, jobID int64, term Terminal) error

    // Logs streams the job's live output (stdout+stderr as written to the .out file).
    Logs(ctx context.Context, jobID int64, follow bool) (io.ReadCloser, error)

    // List returns every execution this executor knows about, for reconciliation.
    List(ctx context.Context) ([]Execution, error)

    // Events delivers state changes. Delivery is at-least-once; consumers dedupe by (JobID, Kind).
    Events() <-chan Event
}

type LaunchSpec struct {
    JobID        int64
    Kind         Kind               // Batch | Command | Interactive
    Node         string
    User         User               // Name, UID, PersonalGID
    TeamSlug     string
    TeamGID      int
    ExtraGIDs    []int              // mist-users
    Resources    Resources          // CPUs, MemoryBytes, GPUs, TTBoards, ScratchBytes
    DeviceIDs    []string           // optional; set when the scheduler assigns specific devices
    Mounts       []Mount            // resolved team folder + software (volume, source, target, readOnly)
    Workdir      string
    OutputPath   string
    Script       string             // Batch
    Argv         []string           // Command
    Env          map[string]string  // full job environment, see runtime-environment.md
    Runtime      string             // partition runtime image / host profile, pinned
    TimeLimit    time.Duration      // executor sets a backstop slightly beyond this
    SignalGrace  time.Duration
}

type Event struct {
    JobID    int64
    Kind     EventKind   // Started | Exited | LaunchFailed | Lost
    Node     string
    ExitCode *int
    Reason   ExitReason  // Normal | OutOfMemory | ScratchExceeded | Deadline | Signaled | ...
    Detail   string      // human-readable, shown to users
    Transient bool       // for LaunchFailed: safe to retry once
    At       time.Time
}

type Node struct {
    Name       string
    Partitions []string
    Capacity   Resources
    Devices    []Device   // optional per-device inventory
    Healthy    bool
    Detail     string
}
```

The scheduler and API depend only on this package. Kubernetes and systemd types
never leak outside their executor packages.

## Guarantees every executor must provide

These guarantees are tested by a shared **conformance suite**
(`internal/executor/conformance`) that runs against each implementation on real
nodes.

1. **Identity.** Processes run as `User.UID`. The primary group is `TeamGID`.
   The supplementary groups are exactly `ExtraGIDs` plus `PersonalGID`. There is
   no privilege escalation and there are no capabilities.
2. **Filesystem view.**
   - **Visible:** only the given mounts, at `/mist/teams/<team>` and
     `/mist/software` (read-only), plus `/scratch`. Other teams' folders are
     absent, not merely denied.
   - **Writable:** only the team folder (as permissions allow), `/scratch` and
     `/tmp`. `/tmp` lives inside scratch.
3. **Devices.** Only the allocated GPUs or TT boards can be opened, and they
   appear numbered from 0.
4. **Limits.** CPU and memory limits apply, swap is off, and scratch is capped.
   Hitting the memory limit gives `OutOfMemory`; going over scratch gives
   `ScratchExceeded`.
5. **Time limits.** `Terminate` delivers SIGTERM to the job's processes, then
   SIGKILL after the grace period. A backstop kills the job even if the
   scheduler is down.
6. **Network.**
   - **Allowed:** outbound internet and DNS.
   - **Blocked:** the LAN, the direct link, the Tailscale range, cluster
     services and the Kubernetes API.
   - **Inbound:** none.
7. **Output.** Output goes to `OutputPath` through `mist-job-entry` (below), and
   live output is also available from `Logs`.
8. **Cleanup.** When the job ends, all of its processes are gone, scratch is
   deleted and devices are free. Executors report `Exited` only once this is
   true.
9. **Repeat-safe and recoverable.** Repeating `Launch` or `Terminate` is safe.
   After a restart, `List` reports what is actually running.

## `mist-job-entry`

A small static Go binary that is the first process of every job, under any
executor. It is the single place that implements the job contract in
[runtime-environment.md](runtime-environment.md):

1. **Set up:**
   - set `umask 0007`
   - create, or reset to `0700`, `$HOME/{.cache,.config,.local,.ssh}`
   - create the output file and write a header (job ID, node, resources, start
     time)
2. **Run the work:**
   - **Batch:** `bash <script> <args>`
   - **Command:** run the argv directly
   - **Interactive:** wait (the holder) until a session starts or the job is
     terminated
3. **Handle output and signals:**
   - copy stdout and stderr to both the output file and its own stdout, so live
     logs work under any executor
   - forward SIGTERM to the job's process group
4. **On exit:**
   - work out the reason (for example `oom_kill` in the cgroup's
     `memory.events`)
   - write a footer: end time, exit code, reason, elapsed time, peak memory
   - report a structured result to the executor (on K3s, through the pod's
     termination message)
   - exit with the child's exit code

Interactive sessions run `mist-job-entry --session`, which starts `bash -l` in
the working directory with the job's environment.

## K3s executor (M1)

### Objects

All jobs run in one namespace, `mist-jobs`. Each job gets:

- **A Pod `job-<id>`.** Not a Kubernetes Job, because Mist owns retries (none)
  and state. It is labelled `mist/job-id`, `mist/partition` and `mist/team`.
- **A ConfigMap `job-<id>`** holding the script and `job.json`. The Pod owns it,
  so Kubernetes deletes it with the Pod.
- **A ResourceClaim** for TT boards (M4), using the existing Tenstorrent DRA
  driver.

### Pod spec essentials

| Field | Value | Why |
|---|---|---|
| `nodeName` | scheduler's choice | Bypasses the Kubernetes scheduler (D1) |
| `restartPolicy` | `Never` | No silent restarts |
| `activeDeadlineSeconds` | time limit + grace + 60 s | Backstop if `mistd` is down |
| `terminationGracePeriodSeconds` | signal grace | SIGTERM → SIGKILL timing |
| `automountServiceAccountToken`, `enableServiceLinks` | `false` | No cluster access from jobs |
| `securityContext` | `runAsUser: uid`, `runAsGroup: team gid`, `supplementalGroups: [19000, personal gid]`, `runAsNonRoot`, `seccompProfile: RuntimeDefault` | D4, D5 |
| container `securityContext` | `allowPrivilegeEscalation: false`, drop `ALL` capabilities, `readOnlyRootFilesystem: true` | Writes go only to `/mist`, `/scratch`, `/tmp` |
| **no `fsGroup`** | — | `fsGroup` would rewrite ownership of team files |
| resources | requests = limits for `cpu`, `memory`, `nvidia.com/gpu`, `ephemeral-storage` | Guaranteed QoS class; the device plugin isolates GPUs |
| image | partition runtime, pinned by digest | Same environment for batch and interactive |

### Volumes

| Mount | Source |
|---|---|
| `/mist/teams/<team>` | NFS `<volume addr>:<export>/teams/<team>`, or `hostPath` when the node hosts the volume |
| `/mist/software` (ro) | NFS or `hostPath`, likewise |
| `/scratch`, `/tmp` | one `emptyDir` with `sizeLimit` = scratch request (`/tmp` is a sub-path) |
| `/dev/shm` | `emptyDir`, `medium: Memory`, sized to half the memory request (PyTorch DataLoader workers need it) |
| `/etc/passwd`, `/etc/group` | ConfigMap `mist-nss`, generated by the API from accounts, so `whoami`, `getpass` and ssh work |
| `/etc/mist/job.json`, script | ConfigMap `job-<id>` |

### Network

A NetworkPolicy in `mist-jobs` denies all inbound traffic. It allows outbound
traffic to DNS and to the internet, except `10.0.0.0/8`, `172.16.0.0/12`,
`192.168.0.0/16` and `100.64.0.0/10`. k3s's built-in policy controller enforces
it.

### Status mapping

| Observed | Event |
|---|---|
| Pod scheduled, container running | `Started` |
| Container terminated | `Exited`, with the code and reason from the termination message (fallback: Kubernetes `OOMKilled`, `DeadlineExceeded`, evicted for ephemeral storage → `ScratchExceeded`) |
| Admission errors (`UnexpectedAdmissionError`, out of `nvidia.com/gpu`), image errors | `LaunchFailed` (admission errors are transient) |
| Pod deleted outside Mist, or node `NotReady` > 2 min with the pod unknown | `Lost` |

The executor watches pods (filtered by label) and nodes through shared
informers. `Nodes()` reports node allocatable values for CPU, memory,
`nvidia.com/gpu` and ephemeral storage, plus TT board counts from DRA resource
slices.

### Logs and attach

- **`Logs`** uses `pods/log` (with `follow`), which carries the copy of the
  output that `mist-job-entry` writes to stdout.
- **`Attach`** uses `pods/exec` with a TTY over WebSocket, running
  `mist-job-entry --session`. Terminal resizes map to the exec resize queue.
  When the session's shell exits, the executor terminates the holder.

### Cleanup and reconciliation

<a id="reconciliation"></a>

- **Cleanup:** after `Exited` is recorded and the log tail saved, the pod is
  deleted 10 minutes later, which leaves a window for admin debugging.
- **Reconciliation** compares `List()` (pods with `mist/job-id`) with the
  database every 60 s and after each restart:
  - pods for terminal or unknown jobs are deleted
  - jobs marked live in the database with no pod are reported `Lost`

### RBAC

`mistd`'s service account gets:

- in `mist-jobs`: `pods`, `pods/log`, `pods/exec`, `configmaps` and
  `resourceclaims`
- cluster-wide: read-only `nodes` and `resourceslices`

### Tenstorrent (M4)

- **Allocation:** boards through DRA claims, plus `hugepages-1Gi` requests and
  `/dev/hugepages`, as `tt-metal` requires.
- **Reset after each TT job:** a short privileged pod, created by the executor
  on that node, runs `tt-smi -r` on the job's boards. The allocation is released
  only when the reset succeeds. A failed reset marks the board unhealthy.

## Node executor (M5)

It provides the same guarantees with no container runtime. One agent,
`mist-noded`, runs as root on each node it manages and talks to `mistd` over
gRPC with mTLS on the direct link.

### Launch

The agent starts a systemd **transient unit** `mist-job-<id>.service` (in
`mist-jobs.slice`) over D-Bus:

| Guarantee | systemd property |
|---|---|
| Identity | `User=`, `Group=` (team), `SupplementaryGroups=`, `NoNewPrivileges=yes`, `UMask=0007` |
| CPU and memory | `AllowedCPUs=` (a CPU set per allocation), `MemoryMax=`, `MemorySwapMax=0`, `TasksMax=` |
| Devices | `DevicePolicy=closed` + `DeviceAllow=` for the allocated `/dev/nvidiaN` (plus `nvidiactl`, `nvidia-uvm*`) or `/dev/tenstorrent/N`; `CUDA_VISIBLE_DEVICES` set to match |
| Filesystem view | `TemporaryFileSystem=/mist/teams:ro` + `BindPaths=/mist/teams/<team>`; `BindReadOnlyPaths=/mist/software`; scratch bound at `/scratch` and `/tmp`; `ProtectSystem=strict`, `ProtectHome=yes` |
| Network | `IPAddressDeny=` the internal ranges; `IPAddressAllow=` DNS |
| Time limit | `RuntimeMaxSec=` as backstop; `KillSignal=SIGTERM`, `TimeoutStopSec=` grace |
| Exit reason | Unit `Result=` (`oom-kill`, `timeout`, `exit-code`) and `ExecMainStatus` |

### Interactive sessions

The agent allocates a PTY and starts `mist-job-entry --session` as a second
transient unit inside the job's slice, passing the PTY as its standard input and
output. The agent keeps the PTY master and streams it over gRPC.

### Prerequisites

- **Accounts on the node:** Linux accounts must exist there, so the provisioner
  runs on every node it manages.
- **Matching software:** the host's software must match the partition's runtime
  profile. See [runtime-environment.md](runtime-environment.md).
- **Specific devices:** the scheduler records exact device IDs in allocations.

### Moving a node from K3s to the node executor

1. Drain the node in Mist.
2. Exclude it from the device plugin and DRA driver.
3. Start `mist-noded`.
4. Point the partition (or a new one) at the node executor.
5. Run the conformance suite.
6. Mark the node `up`.

## Work by milestone

- **M1:**
  - the interface and conformance suite
  - `mist-job-entry`
  - the K3s executor for batch and command jobs on `gpu` and `cpu`
  - logs and reconciliation
- **M2:** `Attach` and the interactive holder.
- **M4:** TT allocation, hugepages and board reset.
- **M5:** the node executor, which passes the same conformance suite.
