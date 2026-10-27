# Architecture decisions

Each entry records the context, the decision, and what follows from it. To
change a decision, add a new entry that supersedes the old one rather than
editing it. The system overview is [ARCHITECTURE.md](../../../ARCHITECTURE.md).

## D1. Mist has its own scheduler; Kubernetes only runs jobs

**Context.** The previous platform made every job a container image with
declared mounts, and that friction is the reason for the redesign. An existing
HPC scheduler (Slurm) was considered. The team chose to build the scheduler as
part of Mist, and the cluster is expected to grow beyond two nodes.

**Decision.** Mist's scheduler owns the queue, policy and placement. A pluggable
**executor** turns an admitted job into running processes. The first executor
uses k3s; the Kubernetes scheduler never decides when or where a job runs.

**Consequences.** Mist implements queueing, limits, priority and accounting.
Kubernetes provides process supervision, device allocation (NVIDIA device plugin,
Tenstorrent DRA), cgroup limits and node membership. If both systems made
placement decisions, jobs could sit pending with no visible reason; pinning pods
to the scheduler's chosen node prevents that.

## D2. Executors sit behind one interface, and each node has exactly one executor

**Decision.** Executors implement the interface in [executors.md](executors.md).
The K3s executor ships first. A native node executor (systemd units and cgroups,
no containers) follows behind the same interface. Each partition maps to one
executor, and each node belongs to one executor, because two systems cannot both
own a node's devices.

**Consequences.** The scheduler and API never import Kubernetes types. A shared
conformance test suite defines executor behaviour. Moving a node between
executors means draining it first.

## D3. Postgres is the source of truth

**Context.** The previous platform kept queue and policy state in ConfigMaps and
suspended Jobs.

**Decision.** All Mist state lives in Postgres. Pods, Linux accounts and folders
are derived and reconciled from it.

**Consequences.** Restarting `mistd` or wiping execution objects loses nothing;
the executor rebuilds its view by comparing the database with what is actually
running. Usage history for fairshare and accounting has a natural home.

## D4. One Linux UID per person

**Context.** Creating a Linux user per job was considered, so that only jobs
would get privileges.

**Decision.** Each person has one stable UID, the same on every host. Jobs run
as that UID. Privileges (devices, write access to the right folders) come from
the job's cgroup, mounts and supplementary groups, not from a different user.

**Consequences.** Files keep one owner across login, jobs and teammates. There
is no ownership mess between jobs. UID and GID allocation is centralised in the
Mist DB, in fixed ranges that don't collide with local system accounts.

## D5. Each job is scoped to one charged team

**Decision.** Every job names a charged team; by default it is the team whose
folder the job was submitted from. The job is given only that team's group and
that team's folder, plus shared read-only software.

**Consequences.** Someone in two teams cannot accidentally mix IP-bound data
between them inside a job. They can still copy files by hand, since they can
read both teams; no technical control prevents a person from moving data they
can read. A person's home is per-team for the same reason (D8).

## D6. Login moves files and manages jobs; code runs only in jobs

**Decision.** Researcher SSH sessions are restricted by `ForceCommand` to:
SFTP/scp, `rsync`, a small set of built-in file viewing commands, and the `mist`
CLI. Anything that executes user code is a job.

**Consequences.** The login host (also a storage and compute node) is protected
from stray compute. Internet downloads become jobs (D11). This is stricter than
typical HPC login nodes, and that is deliberate on a cluster with no dedicated
login hardware.

## D7. Login and storage on the TT box; control plane on the NVIDIA box

**Decision.**

- **TT box:** the durable storage (`/mist`), the login gateway and the
  provisioner.
- **NVIDIA box:** the k3s server, `mistd`, Postgres and the portal.

**Consequences.** Uploads land on local disk with no network hop. The TT box is
the single point of failure for logins and file access. The control plane and
the database sit on separate hardware from the bulk storage.

## D8. A researcher's team folder is their home

**Decision.** `/mist/teams/<team>/<user>/` is both `$HOME` and the default
working directory for jobs charged to that team. It is team-readable (`2750`).
`.cache`, `.config`, `.local` and `.ssh` inside it are private (`0700`): the
provisioner creates them that way, and the job entry wrapper resets them on
every job. A few large shareable caches are mapped to the team's `.cache/` by
environment variables.

**Consequences.** Every tool's cache and config lands somewhere sensible without
per-tool mapping. Credentials stored in the usual hidden folders stay private.
**Accepted risk:** a tool that writes a secret as a top-level file without
restricting its permissions exposes it to teammates (never to other teams).
Someone in two teams keeps two homes. Researchers need this explained, so it
appears under "What researchers see" in [storage.md](storage.md).

## D9. Durable storage plus local scratch; growth by adding volumes

**Decision.** `/mist` is the only durable storage. Node-local scratch is
temporary and wiped after each job. Durable capacity grows by adding **volumes**
(separate NFS exports), and each team folder lives entirely on one volume. No
distributed filesystem is used.

**Consequences.** Researchers never have to work out which machine holds a file.
Adding storage is a database record plus an export. Spare disk on compute nodes
goes to backups first, then scratch, and only then to new volumes.

## D10. The direct link runs at 10 GbE on existing network cards

**Context.** The direct cable uses Intel I210 ports, which max out at 1 Gb/s
(about 110 MB/s). Loading a 14 GB model took around two minutes.

**Decision.** Move the link to the 10GBASE-T ports already installed: the
Aquantia AQC107 (`ens6`) on the NVIDIA box and the Intel X710
(`enp10s0f0np0`) on the TT box. Use Cat6a cable.

**Consequences.** NFS reaches roughly 700–1000 MB/s, so the separate node-local
model cache is dropped from the design. The second X710 port can take one more
direct-connected node. A fourth node needs a 10 GbE switch.

## D11. Downloads are ordinary jobs on a `cpu` partition

**Decision.** The TT box's CPUs form a `cpu` partition, which sits next to the
storage. `mist run -p cpu -- <cmd>` and `mist shell -p cpu` cover every download
tool. There is no dedicated download service.

**Consequences.** Any source works (Hugging Face, S3, Kaggle, `git lfs`, `wget`),
using the researcher's own tokens. GPUs are never held idle during downloads.

## D12. Job outputs go to `/mist`; scratch is opt-in

**Decision.**

- **Output.** Stdout and stderr stream to `mist-<id>.out` in the submit folder.
  Mist appends its own messages there too (node, cancellation, time limit,
  out-of-memory).
- **Job record.** The database keeps the job's metadata, a copy of the script,
  and a tail of the log.
- **Scratch.** `$TMPDIR` points at node-local scratch, which is deleted when the
  job ends.
- **Time limits.** At the limit the job receives SIGTERM, then SIGKILL after a
  grace period (default 60 s).

**Consequences.** Nothing a researcher writes by default is ever lost. The
portal can show logs without reading team files.

## D13. Accelerator device files stay world-accessible; isolation comes from cgroups

**Context.** `/dev/nvidia*` and `/dev/tenstorrent/*` are mode `0666` on both
hosts.

**Decision.** Keep the host modes. Inside a job, the executor's device cgroup
(device plugin, or `DeviceAllow` in the node executor) exposes only the
allocated devices. Host-level access is prevented by D6 and by removing
unmanaged GPU consumers (for example an `ollama` service).

**Consequences.** This matches Slurm's model. If the device files were tightened
to `0660`, containers could no longer open the devices they were given, because
non-root jobs inherit the host permissions.

## D14. Misty owns people and teams; Mist owns accounts

**Decision.** Misty is authoritative for who exists, which teams exist, and who
belongs to them. Mist caches that data and adds what is specific to Mist:
usernames, UIDs and GIDs, SSH keys, limits and roles. Until misty's API is
available, a stub backed by a file stands in for it.

**Consequences.** Removing someone from a team in misty eventually removes their
Mist access to that team. Mist never edits people or teams.

## D15. Other teams' jobs are anonymised in queue views

**Decision.** Queue and partition views show every job's state, resources and
age, so researchers can see why they are waiting. The user, team, job name and
paths are shown only for the viewer's own teams.

**Consequences.** Job names and paths can't leak IP-bound project details
between teams.

## D16. One Go binary, plain Kubernetes objects

**Decision.** `mistd` is one Go binary with subcommands (`api`, `scheduler`,
`provision`). It talks to Kubernetes through `client-go` and creates plain Pods.
It uses no CRDs, operators, `kubectl` shell-outs or Kubernetes Jobs.

**Consequences.** Mist's own state machine stays in Mist's code and database,
which keeps it easy to understand and to swap executors.

## D17. Accelerators are whole devices; CPU and memory are capped per GPU

**Decision.**

- **Whole devices.** Jobs request whole GPUs or TT boards; two 1-GPU jobs share
  the A4000 box side by side.
- **Per-GPU caps.** Each GPU partition caps CPUs and memory per allocated GPU,
  so one job cannot leave another GPU unusable.
- **No GPU-less jobs on GPU nodes.** `gpu` partition jobs must request at least
  one GPU.
- **No GPU sharing.** Fractional GPUs (time-slicing) are not offered, because
  the A4000 has no hardware partitioning and time-slicing gives no memory
  isolation.

**Consequences.** Placement only counts devices, CPUs and memory. Time-slicing
could later be added as an opt-in for small debug work.

## D18. Mist admits jobs; Kubernetes may allocate DRA claims

**Supersedes:** D1's unconditional scheduler bypass. Mist alone owns queue order,
policy, admission and the target node. GPU/CPU Pods use `nodeName`. TT Pods omit
it and use an exact unique node selector so Kubernetes can allocate/reserve DRA
claims on the chosen node. A claim stall is a bounded launch failure, never an
invitation to move the job. Pin and test the installed DRA driver and board reset
scope before M4. Source: [Kubernetes DRA pre-scheduled Pods](https://kubernetes.io/docs/concepts/resource-management/dynamic-resource-allocation/how-dra-works/).

## D19. Personal work defaults to read-only for teammates

**Clarifies D8 and supersedes conflicting implementation defaults.** Jobs and
uploads use `0027`; ordinary personal files are `0640`, directories inherit
setgid and exclude group write. Only shared/cache areas have group-write default
ACLs. Private dot-directories remain `0700`. Owners can explicitly change modes;
provisioning never recursively rewrites researcher content. Verify read/write
behavior as another UID, not merely the mode of the top-level directory.

## D20. Access revocation includes live sessions and queued work

**Refines D6 and D14.** `mist-users` is permanent; disabled accounts never lose
the marker enforcing restricted SSH. Directory snapshots and gateway leases have
a maximum 5 min validity, with no stale-cache extension. Known removal invalidates
API sessions, stops affected pending/launching/running jobs and kills gateway
sessions before updating groups. Existing process credentials do not change when
membership files change. A root local deny store and watchdog enforce expiry even
if the API is down. Revocation completion is acknowledged, not assumed while a
node is unreachable. Sources: [Linux credentials](https://man7.org/linux/man-pages/man7/credentials.7.html),
[usermod password locking](https://man7.org/linux/man-pages/man8/usermod.8.html).

## D21. Execution intent, process outcome and cleanup are separate durable facts

**Refines D3 and D12.** Commit immutable admission/launch intent before invoking an
executor; recover the same execution ID after ambiguous responses. Never rerun
started work automatically. A finished process enters `CLEANING`; resources stay
charged until node cleanup is proven. Lost nodes retain quarantined allocations.
Store result/log tail before object deletion and retain receipts/tombstones.
Postgres preserves control state, not running processes or user files: wiping
Pods can lose execution and scratch. OOM/SIGKILL can prevent an output footer;
the API reports authoritative status and incomplete output explicitly.

## D22. Interactive jobs own a persistent terminal

**Decision.** One holder owns one shell/PTY for the execution lifetime. Attach is
transport to that PTY, never a new shell. One client at a time, bounded replay,
10 min local detach grace, and unchanged walltime apply under both executors.
API/relay restart may detach but cannot reset grace or shell state. Holder death
fails the job. User code stays inside the one allocation/cgroup.

## D23. Isolation must hold on the host as well as in the job spec

**Refines D4, D5 and D13.** NetworkPolicy is paired with host filtering, including
job traffic to its own node, explicit DNS exceptions and tested IPv6 behavior.
Native execution explicitly replaces supplementary groups and drops credentials
before user code; systemd `SupplementaryGroups=` alone only extends NSS groups.
The same real-node conformance suite gates K3s and native execution. Adding nodes
uses a switch unless a separate bridge/routing design has been verified; D10's
second-port suggestion is not a complete network design. Sources:
[Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
[systemd.exec](https://www.freedesktop.org/software/systemd/man/258/systemd.exec.html).
