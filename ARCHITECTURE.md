# Mist architecture

Mist is UTMIST's research compute platform. It works like the batch systems on
national HPC clusters. Each researcher gets a folder inside each of their team
folders and moves files with `scp`/`rsync`. Code runs as **jobs**: batch scripts
or short interactive shells. A scheduler places jobs on the GPU, CPU and
Tenstorrent hardware. Each job runs as the researcher's own Linux user, in their
own folder. Researchers never write Dockerfiles or declare mounts.

This document is the system overview and the coordination point between
component owners. Each component has a document with the contracts it must
honour. The reasons behind major choices are in the
[decision log](docs/agents/architecture/decisions.md).

> **Status:** target design, revised 27 October 2026 after architecture review. This design replaces the
> previous container-image platform (dataset and result pools, team
> namespaces), which was never deployed and has been removed from the tree; it
> remains in Git history before this change. It starts fresh: no users or data
> are migrated.

## Goals

1. **Development feels like a workstation.** A researcher sets up a Python
   environment once, in their own folder, and every job uses it. Files a job
   writes are simply there afterwards.
2. **Access follows teams.** Teammates can read each other's work. Other teams
   cannot see it at all. Some projects are under IP agreements, so this boundary
   is a hard requirement.
3. **Code runs only inside jobs.** Login sessions move files and manage jobs.
   Compute and accelerators are reached only through a scheduled job.
4. **Adding a node is cheap.** Adding a third or fourth machine means configuring
   that node, not redesigning the system.
5. **The A4000 GPUs come first.** The NVIDIA partition ships first; Tenstorrent
   follows on the same design.

## System at a glance

```mermaid
flowchart LR
    subgraph Users
        SSH[ssh / scp / rsync]
        Browser[Browser]
    end

    subgraph TT["utmist-tt (QuietBox)"]
        Gate[Login gateway<br/>sshd + mist-gate]
        Relay[mist-relay]
        Prov[Provisioner agent]
        NFS[("/mist<br/>(durable storage)")]
        CPUP[partition: cpu]
        TTP[partition: tt<br/>4× n300]
    end

    subgraph NV["utmist-z1opa08 (NVIDIA)"]
        Portal[Web portal]
        API[mistd API]
        Sched[mistd scheduler]
        DB[(Postgres)]
        K3s[k3s server]
        GPUP[partition: gpu<br/>2× RTX A4000]
    end

    Misty[(misty<br/>people and teams)]

    SSH -->|Tailscale| Gate --> Relay --> API
    Browser -->|Tailscale| Portal --> API
    API --> DB
    API --> Misty
    Sched --> DB
    Sched -->|Executor| K3s
    K3s --> GPUP & CPUP & TTP
    Prov -->|desired state| API
    Prov --> NFS
    GPUP -. "NFS over 10 GbE direct link" .-> NFS
```

## Components

| Component | Responsibility | Document | Proposed owner |
|---|---|---|---|
| Identity and accounts | People and teams from misty; Mist accounts, UIDs, GIDs, SSH keys; sign-up; provisioning Linux accounts and folders | [identity-and-accounts.md](docs/agents/architecture/identity-and-accounts.md) | Lucas |
| Storage | `/mist` layout and permissions, NFS, volumes, usage reporting, quotas, scratch, backups | [storage.md](docs/agents/architecture/storage.md) | Ansh |
| Login gateway | Restricted SSH (file transfer and `mist` commands only), identity relay, the `mist` CLI | [login-gateway.md](docs/agents/architecture/login-gateway.md) | TBD |
| API | The one HTTP API used by the portal, CLI and agents; authentication and authorization | [api.md](docs/agents/architecture/api.md) | Shared; each owner defines their endpoints |
| Scheduler | Partitions, QoS, limits, queue, priority, fairshare, placement, accounting | [scheduler.md](docs/agents/architecture/scheduler.md) | Brandon |
| Executors | Run an admitted job on a node; stream I/O; report status. K3s executor first, native node executor later | [executors.md](docs/agents/architecture/executors.md) | Brandon |
| Runtime environment | What a job sees: images, software, environment variables, caches, outputs, signals | [runtime-environment.md](docs/agents/architecture/runtime-environment.md) | Scott (to confirm) |
| Web portal | Lightweight UI over the API: account, SSH keys, teams, jobs, queue, usage, admin | [portal.md](docs/agents/architecture/portal.md) | Andrei |
| Deployment and network | Hosts, network paths, services, secrets, admin access, adding nodes | [deployment-and-network.md](docs/agents/architecture/deployment-and-network.md) | Ethan, with Ansh |

Owners should confirm or reassign rows here. Before implementing a component,
read [implementation-contracts.md](docs/agents/architecture/implementation-contracts.md).
It defines cross-component invariants, required acceptance evidence, and decisions
that must be resolved before a milestone can ship. Component documents are the
normative contracts; the implementation plan orders the work. D18–D23 supersede
specific older decisions as listed in the decision log.

## How the parts fit together

Each box is one owner's area; every arrow between boxes is a contract listed in
the table below. Deployment and network (hosts, the 10 GbE link, Tailscale,
secrets) sits under all of it; see its own diagram in
[deployment-and-network.md](docs/agents/architecture/deployment-and-network.md).

```mermaid
flowchart TB
    R([Researcher])
    Misty[(misty)]

    subgraph GW["Login gateway · owner TBD"]
        sshd[sshd]
        gate[mist-gate]
        cli[mist CLI]
        relay[mist-relay]
    end

    subgraph PO["Portal · Andrei"]
        ui[Web UI]
    end

    subgraph API["mistd api · shared"]
        mw[Auth middleware + routing]
    end

    subgraph ID["Identity · Lucas"]
        dir[misty client / stub]
        acct[(accounts, teams, SSH keys)]
        prov[Provisioner agent]
    end

    subgraph SC["Scheduler · Brandon"]
        sched[Scheduling cycle]
        ledger[(jobs, allocations, usage)]
    end

    subgraph EX["Executors + k3s · Brandon"]
        k3sx[K3s executor]
        entry[mist-job-entry]
    end

    subgraph RT["Runtime · Scott"]
        img[Runtime images]
        sw["/mist/software"]
    end

    subgraph ST["Storage · Ansh"]
        vol[("/mist volumes")]
        scratch[(Node scratch)]
    end

    R -->|"ssh, scp, rsync"| sshd --> gate --> cli
    gate -->|"file transfer as the user"| vol
    cli -->|unix socket| relay
    sshd -.->|AuthorizedKeysCommand| relay
    relay -->|"HTTP /v1 + asserted user"| mw
    R -->|browser| ui -->|"HTTP /v1 + session"| mw
    mw -->|identity.Authorize| acct
    mw -->|job endpoints| sched
    dir --> Misty
    dir --> acct
    prov -->|"GET provision/state"| mw
    prov -->|"users, groups, folders"| vol
    sched --> ledger
    sched -->|Executor interface| k3sx
    acct -.->|per-job mist-nss projection| k3sx
    k3sx -->|"pod: uid, team gid, devices"| entry
    k3sx -->|"mounts team folder + software"| vol
    k3sx --> scratch
    img -.->|ships| entry
    img -.-> sw
```

| Boundary | Provider → consumer | Contract | Defined in |
|---|---|---|---|
| Who is registered, which teams | misty → Identity | `Directory` interface (stub until the real API) | [identity](docs/agents/architecture/identity-and-accounts.md#misty) |
| Who may do what | Identity → every API handler | `identity.Authorize(principal, action, resource)` | [identity](docs/agents/architecture/identity-and-accounts.md#roles) |
| Linux accounts and folders | Identity → Provisioner → Storage | `/internal/v1/provision/state`, layout rules | [identity](docs/agents/architecture/identity-and-accounts.md#provisioner), [storage](docs/agents/architecture/storage.md#layout) |
| CLI calls as the researcher | Gateway → API | Relay socket, `X-Mist-Asserted-User` + relay token | [login gateway](docs/agents/architecture/login-gateway.md#identity-relay) |
| Everything clients see | API → Portal, CLI | `/v1` endpoints and wire types | [api](docs/agents/architecture/api.md) |
| Running a job | Scheduler → Executors | `executor.Executor`, `LaunchSpec`, events | [executors](docs/agents/architecture/executors.md#interface) |
| What a job sees | Executors + Runtime → researcher code | Job environment contract, `mist-job-entry` | [runtime](docs/agents/architecture/runtime-environment.md) |
| Where files live | Storage → Executors, Gateway | Volumes, mount paths, permissions | [storage](docs/agents/architecture/storage.md#volumes-and-growth) |

## Core concepts

- **Person.** A human registered in misty. Misty is the source of truth for
  people, teams and membership.
- **Account.** A person's Mist identity: one immutable `username`, one stable
  Linux UID, and SSH keys. It is created when a registered person signs up.
- **Team.** A misty team. On Mist, each team has a Linux group (`team-<slug>`) and
  a folder `/mist/teams/<slug>/`.
- **Partition.** A named pool of nodes that share one kind of hardware: `gpu`
  (NVIDIA), `cpu` (CPU-only, used for data prep and downloads) and `tt`
  (Tenstorrent). A job targets exactly one partition.
- **QoS.** A class of service, `normal` or `interactive`, carrying limits and a
  priority weight.
- **Job.** A request to run a script (batch) or a shell (interactive). It names
  one partition, one QoS, one **charged team**, its resources and a time limit.
  It runs as the submitter's UID. Accelerators are allocated whole and one at a
  time: two 1-GPU jobs run side by side on the A4000 box.
- **Charged team.** The team a job is billed to and scoped to. The job sees only
  that team's folder and the shared software. It cannot see the submitter's other
  teams.
- **Volume.** One durable storage location, an NFS export on one server. Each
  team folder lives entirely on one volume. There is one volume today.

## The researcher's journey

One researcher, Alice on team `vision`, goes from sign-up to results. The boxes
mark which owner's component handles each step.

```mermaid
sequenceDiagram
    autonumber
    actor R as Alice
    box Portal · Andrei
        participant W as Portal
    end
    box Login gateway · TBD
        participant G as Gateway + relay
    end
    box API · shared, Identity · Lucas
        participant A as mistd api
        participant P as Provisioner
    end
    box Scheduler + executors · Brandon
        participant S as Scheduler
        participant E as K3s executor
    end
    box Runtime · Scott, Storage · Ansh
        participant J as Job
        participant F as /mist
    end

    Note over R,F: Join
    R->>W: Sign in
    W->>A: Is Alice registered in misty?
    A-->>W: Yes, teams [vision]. Account alice, uid 20007
    R->>W: Add SSH public key
    P->>A: Poll desired state
    P->>F: Create user, groups, /mist/teams/vision/alice

    Note over R,F: Bring code and data
    R->>G: ssh, then rsync ./project
    G->>F: Write to /mist/teams/vision/alice/project
    R->>G: mist run -p cpu -- hf download some-model
    G->>A: Submit command job (cpu)
    A->>S: Job queued
    S->>E: Launch on utmist-tt
    E->>J: Start as alice, team vision
    J->>F: Model saved to team .cache

    Note over R,F: Set up an environment
    R->>G: mist shell -p gpu --gpus 1
    G->>A: Submit interactive job
    S->>E: Launch on utmist-z1opa08 (1 GPU)
    E->>J: Start shell holder
    G->>A: Attach terminal (WebSocket)
    A->>E: Attach
    R->>J: uv sync (venv lands in her folder)
    R->>J: exit, job completes

    Note over R,F: Train
    R->>G: mist submit train.sh
    G->>A: Submit batch job 412
    A-->>R: 412 PENDING (RESOURCES)
    S->>E: GPU frees up, launch 412
    E->>J: Start train.sh
    J->>F: Stream mist-412.out, write checkpoints
    R->>G: mist logs -f 412
    J-->>E: Exit 0
    E-->>S: Finished (CLEANING; allocation held)
    E->>E: Persist result, delete execution, confirm node cleanup
    E-->>S: Exited (cleanup receipt)
    S->>A: 412 COMPLETED

    Note over R,F: Take results home
    R->>G: rsync checkpoints back to laptop
    G->>F: Read as alice
```

## Key flows

### Sign-up and first login

1. A person signs in to the portal. Identity confirms with misty that they are
   registered and loads their teams.
2. Mist creates their account: it reserves a username and a UID and records team
   memberships.
3. The person adds an SSH public key in the portal.
4. The provisioner agent on the storage/login host reconciles the change. It
   creates the Linux user and groups, and `/mist/teams/<team>/<user>/` for each
   team.
5. The person runs `ssh <user>@<login host>`. They land in a folder with one
   entry per team.

### Moving files

`scp`, `rsync` and `sftp` go through the login gateway, which sits on the storage
host and writes straight to disk. Unix permissions decide what each person can
read and write.

Downloads from the internet (Hugging Face, S3, Kaggle and so on) run as jobs on
the `cpu` partition, using `mist run -p cpu -- <command>` or `mist shell -p cpu`.

### Batch job

```mermaid
sequenceDiagram
    participant U as Researcher (login)
    participant G as mist-gate + relay
    participant A as mistd API
    participant S as Scheduler
    participant E as Executor (K3s)
    participant N as Node
    U->>G: mist submit train.sh
    G->>A: POST /v1/jobs (relay-asserted user)
    A->>A: validate spec, membership, limits
    A-->>U: job 412 PENDING
    S->>S: order by priority, check limits and free capacity
    S->>E: Launch(job 412, node, resources)
    E->>N: start as uid, team gid, team folder, devices
    N-->>E: running … exited(0)
    E-->>S: Finished, then Exited after confirmed cleanup
    S->>A: job 412 COMPLETED; allocation released
    U->>G: tail -f mist-412.out
```

The job starts in the folder it was submitted from. Its output streams to
`mist-<id>.out` there. Checkpoints and results go wherever the code writes them,
in `/mist` by default.

### Interactive job

`mist shell --gpus 1` submits a job with QoS `interactive`. When the job starts,
the API attaches the researcher's terminal to a shell inside it. The job ends
when the shell exits. If the connection drops, the job waits for a grace period
so the researcher can run `mist attach`, then it is cancelled. A browser terminal
will later reuse the same attach endpoint.

## Data ownership

Postgres on the control node is the single source of truth for Mist state.
Kubernetes objects, Linux accounts and folders are *derived* state: agents
reconcile them from the database.

| Data | Authority | Owner |
|---|---|---|
| People, teams, membership | misty (cached in Mist) | Identity |
| Accounts, UIDs, GIDs, SSH keys | Mist DB | Identity |
| Linux users, groups, folders | Derived by the provisioner | Identity, Storage |
| Volumes, team placement, usage | Mist DB | Storage |
| Partitions, QoS, limits | Mist DB (admin-configured) | Scheduler |
| Jobs, job events, usage records | Mist DB | Scheduler |
| Pods and other execution objects | Derived by the executor; cleanup acknowledged before reuse | Executors |
| Files | `/mist` | Storage |

Each owner owns their tables and migrations. Other components go through that
owner's Go package or API, not their tables. Packages participating in admission
and revocation accept the same database transaction so authorization and job
changes serialize; an HTTP authorization check followed by an unrelated commit
is not sufficient. Durable job intents and executor observations survive restarts.
User files are not derived state and must never be recreated or deleted by job
reconciliation.

## Code layout

```text
cmd/mistd/            api, scheduler, provision, cleanup-report subcommands
cmd/mist/             CLI (on the login gateway; laptops later)
cmd/mist-gate/        SSH ForceCommand dispatcher
cmd/mist-relay/       login-node identity relay and SSH key lookup
cmd/mist-job-entry/   in-job wrapper: output file, signals, exit reasons
internal/api/         HTTP server, middleware, shared wire types
internal/identity/    accounts, misty client and stub, provisioning state
internal/storage/     layout rules, volumes, usage
internal/scheduler/   queue, policy, placement, accounting
internal/executor/    Executor interface; k3s/ and node/ implementations
internal/db/          connection and migrations (one directory per owner)
images/               job runtime images
deploy/               k3s manifests, systemd units, host setup
web-interface/        portal
```

The previous platform's `src/`, `cli/`, `deploy/` and `web-interface/` were
removed on 27 October 2026; the portal is rebuilt in `web-interface/` against
the new API.

## Milestones

| Milestone | Outcome |
|---|---|
| **M1: GPU batch** | Sign-up (misty stubbed), SSH login, file transfer, `mist submit/run/jobs/show/logs/cancel` on `gpu` and `cpu`, verified permissions/revocation/network isolation, crash recovery and cleanup accounting, 10 GbE direct link |
| **M2: Interactive** | `mist shell` and `mist attach` with reattach grace; `interactive` QoS |
| **M3: Fair scheduling** | Priority, fairshare by team, backfill, accounting, admin limits, queue views; backups in place |
| **M4: Tenstorrent** | `tt` partition, TT runtime image, board reset between jobs |
| **M5: Native executor and web terminal** | Node executor without containers, behind the same interface; browser terminal; port forwarding into jobs |

Each component document lists its work per milestone.

## Open questions

| Question | Owner |
|---|---|
| Real misty API: shape, and how membership changes reach Mist (polling or webhooks) | Identity |
| How people prove who they are at sign-in (Google or UofT SSO, or email magic link) | Identity |
| Enforced storage quotas: needs `/mist` on a dedicated filesystem with project quotas | Storage |
| Retention after access revocation: archive or transfer departed members’ folders (access revocation itself is specified) | Identity, Storage |
| Backup tool, schedule and retention | Storage |
| Default limits per partition and per user once real usage is known | Scheduler |
| Monitoring and alerting beyond the portal | Deployment |
