# Mist department rollout

Implemented October 9, 2026 on `feat/local-job-foundation`. This guide supersedes
the account-only workspace instructions in [complete foundation](complete-foundation.md).
The older `main` preview and original accounts, jobs, datasets and checkpoint PVCs
are preserved.

## Use the portal

Open **http://100.73.139.66:8088** after connecting to the existing Tailscale network
or accepting a share of the main machine. A Mist account is also required; a
Tailscale share does not grant a Mist role or team membership.

1. Sign in. Choose your team in the sidebar **Workspace** selector.
2. Open **Datasets → Add dataset**, choose **My folder** or an authorized common
   folder, and upload a file or ZIP. New team storage is prepared asynchronously; the portal shows a waiting
   state and enables upload when ready. Wait for the dataset ready message.
3. Open **Jobs → New job**. Select hardware and an image tag/digest, attach a dataset if
   needed, and submit your Python script or container entrypoint/arguments.
4. Jobs appear as compact history rows, paginated with 10/25/50 rows per page.
   Search across jobs/people/IDs or filter status. Click a row to expand resource details,
   **Logs**, **Files**, and cancellation when authorized.
5. Save results under `/outputs`. They remain after exit and can be downloaded
   from the job or browsed through **Datasets → Team folders**.

**Research pilot** is an initial workspace containing the administrator and
verified sample training results. **Legacy files and jobs** shows preserved
account-owned resources. New uploads/submissions require a team; this prevents
using the legacy workspace to bypass quotas.

The administrator signs in through the same screen and can also perform normal
research work. Initial credentials remain in the protected bootstrap file at
`/home/utmist/.config/mist/admin-bootstrap.json`. That file does not track later
password changes. Never share or commit its auth secret.

## Administration

On **Account → Members**, use **New member** to create accounts. Search the
compact list to deactivate/reactivate a member or open their password reset
dialog. **My account** contains your own password settings. A reset revokes existing sessions. There is no email delivery
service; hand the temporary password to its owner privately. Members can change
their own password. Public signup remains disabled.

On **Teams**:

- Use **New team** to create a team and choose its initial storage allocation. The portal enrolls
  the creating administrator as a common-folder writer.
- Open **Manage → Teammates** to add existing active accounts. Members may belong to several teams. Update
  **Can write common folder** independently of membership.
- Open **Manage → Limits** to set team CPU, RAM, NVIDIA GPUs, TT boards, concurrency, queued-job capacity,
  maximum runtime, storage and allowed registry hosts.
- Open **Manage → Sharing** to share a **common** or **member** folder with a specific other team. Grants are
  read/download/use permissions. They never permit writing or deleting the
  source team's data.
- Remove membership, revoke a grant or disable a team. Subsequent requests lose
  access immediately. Affected queued/running jobs are suspended and their pods
  are removed; saved data remains. The controller checks account deactivation
  and current admin roles every reconciliation.

Administrators can manage team settings but must explicitly belong to a team to
use its workspace. Admin status does not silently grant access to all files.
Removing a member keeps their stored data available to the remaining team.

## Permissions

| Operation | Ordinary teammate | Common writer | Administrator enrolled in team |
|---|---|---|---|
| Read common/member folders and team jobs | Yes | Yes | Yes |
| Upload/delete own datasets; save own job results | Yes | Yes | Yes |
| Upload/delete common datasets; save common results | No | Yes | Yes |
| Cancel a teammate's job | No | No | Yes |
| Manage membership, limits or cross-team grants | No | No | Yes |
| Read another team's storage without a grant/membership | No | No | No |

Dataset deletion is blocked while a queued, running or terminating job references
it, including a job in a recipient team. Finished outputs have no automatic
retention/cleanup policy. Inputs are read-only for all jobs.

## Running architecture

```text
Researcher browser / authenticated CLI on either machine
                      |
           private Tailscale portal :8088
                      |
             Nginx on NVIDIA machine
              /api/*       /auth/*
                      |
             Go API + single controller
                |                 |
        Better Auth            Kubernetes
       local SQLite PVC      team policy ConfigMaps
                                suspended Jobs
                                     |
                          fair admission + scheduler
                            /                   \
                 CPU / NVIDIA node          QuietBox TT node
                 1–2 whole GPUs             1–4 n300 boards
                            \                   /
                        scoped NFS mounts over LAN
                       read-only /inputs; own /outputs
                                     |
                   QuietBox bounded team filesystems
```

Production runs without Vite or an SSH session. Nginx, k3s/k3s-agent, Tailscale,
NFS and the team volume service start through systemd. API/auth run in
`mist-system`; workloads run in `mist-team-<id>`. Auth remains a single SQLite
instance on the main machine's local volume.

## Storage and enforced capacity

QuietBox's known ext4 backing image now has **1,600 GiB logical capacity**.
It is sparse: unused capacity does not reserve that amount of physical disk space.
The initial bulk reservation was released with filesystem discard (`fstrim`),
without shrinking the filesystem or deleting existing data. At the October 9
check, QuietBox had about **1.9 TiB free** on its host disk; the backing file used
about 6 GiB after reclaiming the deleted test dataset’s blocks. Actual shared
data was about 1.4 GiB; filesystem metadata also uses space.

The combined allocation budget is **1,500 GiB**, additionally bounded by the
base filesystem capacity less 20 GiB. New teams default to **200 GiB datasets +
500 GiB models/results**. API field `storage_gib` means datasets; the new
`model_storage_gib` means models/results. Legacy create requests specifying only
`storage_gib` apply that size to both pools for compatibility. Two teams at these defaults fit; more teams need smaller
admin allocations or additional capacity. Disabled teams retain files and their
allocations. Existing pilot teams retain their previous sizes in each pool;
they are not silently expanded to the new defaults.

Each team has two independently bounded ext4 filesystems. Common and member
folders share the applicable team pool; member folders do not have separate hard
quotas. Filesystem overhead reduces usable capacity below the nominal allocation.
No physical disk was formatted.

```text
/srv/mist-storage/
  metadata/provision-requests/       API-owned requests, outside job mounts
  metadata/provision-status/         readiness records for both pools
  .team-volumes/team-<id>.img         dataset filesystem image
  .team-volumes/team-<id>-models.img  model filesystem image
  teams/team-<id>/                   dataset filesystem
    metadata/datasets/               API-owned published metadata
    .uploads/                        API-owned upload staging
    common/datasets/<id>/content/
    members/<hash>/datasets/<id>/content/
    .models/                         separate model filesystem
      common/jobs/<id>/outputs/
      members/<hash>/jobs/<id>/outputs/
  datasets/, jobs/, legacy/           preserved foundation files
```

`mist-team-storage.service` validates trusted requests, creates images, mounts them,
records fstab entries and exports each filesystem separately over NFS. Existing
unknown filesystems are never formatted. Online growth is supported; shrinking
and deletion are refused. The API verifies both capacities and distinct mount
device identities, including when NFS reports a zero filesystem ID. A missing
model mount cannot borrow the dataset allocation.

Existing team job outputs are copied into the model pool, preserving hashes,
permissions, ownership and symlinks without following them. Originals are removed
only after the complete published copy is verified. Conflicts fail closed;
interrupted copies can be retried. Perform migration with submissions paused and
no active team jobs. Dataset paths, job IDs and virtual folder URLs stay stable.
Inputs use read-only `team-storage`; outputs use `team-models` and only the
specific job directory. Actual ext4 capacity enforces writes; PV/PVC numbers are
allocation declarations. Existing bound static NFS claims are preserved during
growth rather than attempting unsupported PVC resizing.

Upload admission uses actual remaining **dataset** capacity, with a 64 MiB
metadata reserve. There is no fixed 64 GiB upload cap. Administrators can set an
optional smaller `MIST_MAX_DATASET_GIB` ceiling (1–1,048,576); that ceiling does not
create disk space. The portal shows used/free space separately for datasets and
models/results. ZIP extraction needs space for the archive and expanded files;
it rejects traversal, symlinks and special files, with at most 10,000 entries.

Nginx streams uploads without body buffering; the authenticated API enforces
storage bounds. Browser/API transfer windows are 24 hours. Uploads support
progress and cancellation; interrupted transfers restart. Resumable upload is
not implemented. Earlier baseline verification transferred and removed a
2 GiB + 4 MiB probe. Follow-up verification uses small files, as requested;
no 200/500 GiB upload or exhaustion test is performed.

Jobs can exhaust their model filesystem and must handle `ENOSPC`/`EDQUOT`.
Dataset capacity remains separate, as do other teams' filesystems. The host still
needs free space for both sparse image growth and software outside Mist. Logical
quotas are not physical space reservations or a backup.

NFS permits only the two node LAN addresses and uses root squash. The machines'
existing router/Wi-Fi/Ethernet connections remain intact. QuietBox must be online
for shared storage; NFS is not a backup.

## Admission and resource rules

| Limit | Default per team | Supported setting |
|---|---|---|
| CPU / memory | 8 cores / 32 GiB | Up to 32 cores / 128 GiB across jobs |
| NVIDIA GPUs | 2 | 0–2 whole GPUs |
| TT boards | 4 | 0–4 boards, two chips each |
| Simultaneous / queued jobs | 2 / 50 | 1–16 / 1–200 |
| Dataset storage | 200 GiB | At least 1 GiB; combined dataset/model allocations at most 1,500 GiB |
| Model/result storage | 500 GiB | At least 1 GiB; independently bounded from datasets |
| Maximum runtime | 24 hours | 10 seconds–24 hours |

Each job remains limited to 8 cores and 32 GiB. The default execution deadline
is 600 seconds; the browser defaults to the lower team maximum when needed. The deadline begins after admission and includes pod/image startup.
Queued jobs have a separate 24-hour wait limit.

Submissions are persisted as suspended Jobs. The single controller admits at
most one FIFO head per team per pass, rotating the last-served team through a
persisted cursor. Admitted jobs reserve their requested resources even while
their pods are starting. After 30 seconds, an eligible waiting head reserves its
node against later admissions so a large request can receive draining capacity.
Running work is not preempted for fairness. k3s then binds pods to the appropriate
node and NVIDIA/DRA provides the actual claimed devices.

Queue state survives API restart; there is no Redis dependency in this execution
path. Cancellation and revocation preserve a bounded log snapshot. A failed job
does not automatically rerun training (`backoffLimit=0`); resubmission is explicit.
Do not scale the API/controller above one replica: multiple controllers require
a leader lease or equivalent admission coordination. The deployment uses Recreate.

## Container image workflow

Teams build and push their image to a registry allowed in their policy, then enter
its tag or digest in the portal. Default registry hosts are Docker Hub, GHCR and
NVIDIA's public registry. No individual image approval is required.

```text
Developer builds image → pushes tagged image to allowed registry
                                 |
                         enters reference in Mist
                                 |
                        node pulls image with containerd
                                 |
                   entrypoint/command runs as a Kubernetes Job
```

Include dependencies and compatible accelerator libraries in the image. Use a
digest when reproducibility matters. Docker archive upload/build in the browser
and team-managed private registry credentials are excluded from this release.
Normal public image tags, default entrypoints, explicit commands/arguments,
environment variables, scripts and working directories are supported.

### Container mode in the portal

1. Select your team, then **Jobs → New job**.
2. Choose **NVIDIA** and the GPU count, then **Container**.
3. Enter a tagged image containing your code and dependencies.
4. Leave **Command executable** and **Arguments** blank to use the image's
   `ENTRYPOINT`/`CMD`. Enter them only to override the image startup.
5. Submit, expand the history row, and open **Logs** or **Files**. Save files
   under `/outputs` to retain/download them after the job finishes.

A regular-member UI demonstration submitted a custom image containing
`/app/train.py` and `CMD ["python", "/app/train.py"]`. No script or command override
was injected. Its two-layer PyTorch network trained for 200 steps on one RTX A4000
using `cuda:0`, reduced loss from 14.088 to 0.004936, saved weights/metrics and
verified checkpoint reload. [Container mode evidence](../deploy/k3s/evidence/department-2026-10-09/container-mode-result.json).
The demonstration image was preloaded on the main node; it was not pushed to a
public registry. Normal team workflows build/push to an allowed registry.

The default NVIDIA image contains CUDA-compatible PyTorch. Tenstorrent can use
the tested installed runtime with the default pinned image, or **Runtime included
in my image** for a compatible custom container. Custom containers receive DRA
devices and huge pages; they do not receive the host Python/TT-Metal mounts.
Successful regression training is not a guarantee that every model/library
supports TT. Intra-device sharing and distributed mixed-backend training remain
outside this release.

## Network boundary

Workload namespaces have a deny-by-default ingress/egress policy. They may resolve
DNS and reach public IPv4 HTTPS, but cannot directly connect to the private API,
Kubernetes, NFS, Tailscale SSH or another team's containers. Their storage mounts
are made by the trusted kubelet. Pods do not receive service-account tokens, host
network/PID/IPC access, privileged access or extra capabilities. The only permitted
host paths are the read-only tested TT runtime paths. GPU drivers still share the
host kernel; this is not VM isolation for hostile external code.

Host rules restrict ordinary shared Tailscale sources to main's TCP 8088. Both
machines deny their direct backend/SSH/NodePort access and forwarded cluster
traffic for those sources. Infrastructure addresses `100.73.139.66`,
`100.95.175.37` and the known administrator `100.99.194.52` (and their matching
Tailscale IPv6 addresses) retain administrative access. LAN access is unchanged.
Adding an administrator device requires updating that explicit host allowlist.

`mist-portal-firewall.service` installs these rules after Tailscale/k3s and is
reapplied with their service restarts. Tailscale's cloud sharing/ACL policy was
not edited. Tests exercised the actual installed chains with an isolated simulated
untrusted source; a browser on an external shared researcher's laptop is outside
the test environment. Manual netfilter/UFW resets should be followed by restarting
this service. The portal remains private HTTP; no Serve, Funnel or public hosting.

## CLI on either machine

```bash
export MIST_API_URL=http://100.73.139.66:8088/api
bin/mist-cli auth login --email your-mist-email
bin/mist-cli team list
bin/mist-cli team use team-db3933031a6c2437
bin/mist-cli job submit train.py --compute NVIDIA --devices 1
bin/mist-cli job list --all
bin/mist-cli auth logout
```

On QuietBox the updated binary is installed at `~/.local/bin/mist-cli`; use that
path in place of `bin/mist-cli`. Use an actual team ID returned by `team list`. Login prompts without echoing the
password. The selected team and cookie are stored in mode-0600 config. `--team`
or `MIST_TEAM` overrides the saved workspace for a command; `team use legacy`
selects historical account jobs. `job submit --scope common` requires common write
permission; `--tt-runtime container` chooses an image-provided TT runtime. Other
machines can submit to any allowed hardware through the same portal API; SSH is
not how k3s launches their jobs.

## Developer API contract

Browser calls use the portal's `/api` prefix; Nginx strips it for Go. Auth calls
use `/auth`. Both use the Better Auth session cookie. Send `X-Mist-Team` with an
actual team ID for team work; `team_id` is also supported on download links.
Identity, current role, membership and write scope are checked on the server.
Missing team selection can read historical account data but cannot submit/upload.

| Method and path (after `/api`) | Operation |
|---|---|
| `GET /session`, `/teams` | Current account and permitted team list |
| `POST /teams` | Admin creates `{name, storage_gib, model_storage_gib}`; enroll separately |
| `PATCH /teams/{id}` | Admin updates name, disabled flag or a complete policy |
| `POST /teams/{id}/members` | Admin enrolls/updates `{user_id, common_writer}` |
| `DELETE /teams/{id}/members/{user_id}` | Admin removes membership |
| `POST /teams/{id}/grants` | Admin adds `{target_team, scope}` read/use grant |
| `DELETE /teams/{id}/grants/{grant_id}` | Admin revokes a grant |
| `GET /hardware`, `/images` | Actual capacity and compatible image/runtime profiles |
| `POST /jobs`, `GET /jobs`, `GET /jobs/{id}` | Submit and inspect team jobs |
| `POST /jobs/{id}/cancel`, `GET /jobs/{id}/logs` | Authorized cancellation and logs |
| `GET /jobs/{id}/files` | Saved output listing; `/files/download?path=...` downloads |
| `GET /storage`, `/storage/folders` | Actual filesystem space and permitted folders |
| `GET /storage/files?source_team=...&scope=...` | Browse a scoped folder; add `path` and `download=true` to download |
| `GET /datasets`, `GET /datasets/{id}` | Available own/shared datasets and metadata |
| `POST /datasets?filename=...&scope=...` | Raw file body; optional `name` and `format=zip` |
| `DELETE /datasets/{id}` | Delete a writable dataset with no active job references |

`scope` is `common` or a member's auth user ID, never a filesystem path. Job
submission uses `storage_scope` for its output location. A simple container body:

```json
{
  "name": "Baseline experiment",
  "type": "command",
  "accelerator": "cpu",
  "image": "python:3.11-slim",
  "command": ["python"],
  "args": ["/app/train.py"],
  "timeout_seconds": 600
}
```

The image must contain `/app/train.py`; inputs mount at `/inputs`, outputs at
`/outputs`. Submit scripts with `script` and `script_name` instead. NVIDIA/TT
requests also specify `device_count`; TT counts **boards**, not chips. The request
and response types are in `web-interface/src/api.ts`, and authority lives in Go,
not browser validation. The server returns 201 for new teams/datasets, 200 for
submitted jobs, and JSON errors for rejected requests.

## Deploy and hand off

The main-node build command is `bash deploy/private/build-and-deploy.sh`.
It preserves bootstrap/internal auth credentials, builds the API/CLI and imports API/auth images,
applies policy/RBAC manifests, waits for rollouts, type-checks and builds the SPA,
and installs an atomic Nginx release. See [private operations](../deploy/private/README.md)
for host services and recovery of deployment changes. API/auth image tags are
`mist-api:departments-20261009` and `mist-auth:departments-20261009`.

Build the CLI with `go -C cli build -o ../bin/mist-cli .`. New developer tasks
should retain the identity → team policy → queued Job → scoped storage boundary.
Never accept owner/roles from a submission or mount an entire store into a job.

Tests: [department verifier](../deploy/k3s/verify_department.mjs),
[browser verifier](../deploy/k3s/verify_department_browser.mjs), [large-dataset verifier](../deploy/k3s/verify_large_dataset.py), Go team tests,
CLI tests and React job/control tests. Checked-in evidence is linked from the execution
plan. Protected verification state contains credentials and is never checked in.

Automatic backups/recovery, HTTPS, monitoring/cleanup products, credits, Jupyter,
job chaining, browser image archives/builds and public/cloud hosting remain
explicitly excluded. There is one control plane and no new high-availability
failover mechanism.
