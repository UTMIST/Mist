> Current department storage and portal settings: [department rollout](department-rollout.md).
> This document records the earlier foundation; its 100 GiB pilot was later expanded sparsely.

# Mist foundation: historical operating snapshot

> **Historical account-only snapshot.** The [department rollout](department-rollout.md)
> supersedes these instructions: team membership, scoped folders, enforced quotas,
> self-service images and the new portal are implemented. Use that guide and
> [current deployment operations](../deploy/private/README.md) for current behavior.
> The remaining sections record the preceding foundation, including its limitations.

Implemented on `feat/local-job-foundation`, October 8–9, 2026. This continues the
[original foundation](local-job-foundation.md) with shared datasets, output
retrieval, separate member accounts and persistent private hosting.

## Use the website

Open **http://100.73.139.66:8088** from a device on the existing Tailscale network.
The alternative MagicDNS address is http://utmist.tail459b5e.ts.net:8088.

1. Sign in with a Mist account, independent of the machine's Linux/SSH login.
2. Open **Datasets**, upload one file or a ZIP, and wait for **Ready**.
3. Open **Jobs**, choose CPU, NVIDIA GPUs or Tenstorrent boards, an approved
   image, and optionally the uploaded dataset. Submit a script or container.
4. Follow the actual status, placement and logs. Save files under `/outputs`.
5. Click **Files** on the job to download results. Results remain after exit.

The initial administrator's credentials are stored locally in
`/home/utmist/.config/mist/admin-bootstrap.json` (mode 0600), outside Git. View
that file locally, sign in, then change the password on **Account**. The file
contains a bootstrap password, not a password that automatically tracks changes.
Do not distribute its auth secret. Administrators add members on **Account**;
members can change passwords there. Deactivation revokes sessions. Public signup
is disabled. There is no email delivery/password-reset email service configured;
an administrator can reset a member password through Better Auth's authenticated
`/auth/admin/set-user-password` endpoint. Admin roles are managed by that service,
never by editing one's profile.

**HTTPS status:** Tailscale Serve is disabled on this tailnet. Its CLI reports
that an account administrator must enable Serve in the Tailscale console. The
current HTTP endpoint is bound to localhost and the Tailscale IP only. Remote
traffic uses Tailscale's encrypted network, but browsers do not treat HTTP as a
secure origin. No public Funnel/domain was enabled. After enabling Serve, run
`tailscale serve --bg --https=443 http://127.0.0.1:8088`, change
`MIST_SITE_ORIGIN` in the auth deployment to `https://utmist.tail459b5e.ts.net`,
and restart auth so cookies use Secure. The HTTPS origin is already in the
trusted-origin lists. Check Serve status and browser login before distributing
that URL. Do not claim HTTPS is available before that verification.

## Running architecture

```text
Team browser / CLI / either SSH host
        │ private Tailscale network
        ▼
Main node Nginx :8088 ── static production React build
        │ /api/* and /auth/*
        ▼
Go Mist API, Kubernetes Deployment (mist-system)
        ├── Better Auth service ── local SQLite PVC on main node
        ├── dataset metadata/uploads/results ── shared NFS PVC
        └── Kubernetes Jobs (mist namespace)
             ├── Main node: CPU or 1–2 NVIDIA GPUs
             └── QuietBox: 1–4 TT boards, two chips per board
                       │
           selected /inputs (read-only), own /outputs (writable)
                       │
          QuietBox /srv/mist-storage (NFSv4 over current LAN)
```

Nginx proxies directly to the existing API ClusterIP `10.43.1.116:3000`;
production does not rely on Vite, SSH sessions or the development port-forward.
The local preview at `127.0.0.1:3001` remains available and now uses real login.
Kubernetes schedules jobs using the requested resources and node profiles.
TT allocations use DRA boards. NVIDIA allocations use whole GPUs. Neither device
sharing within a GPU/board nor distributed mixed-backend training is implemented.

## Storage layout and limits

QuietBox exports `/srv/mist-storage` to only LAN IPs `10.0.0.175` and
`10.0.0.112`, using NFSv4 and root squash (anonymous UID/GID 65532). UFW admits
TCP 2049 from those two addresses. Its router Ethernet connection stays intact.
The main machine continues using its existing Wi-Fi connection.

A dedicated **100 GiB sparse ext4 image** on QuietBox's existing NVMe backs the
store. No physical disk was formatted. Actual usable capacity is slightly smaller
because of filesystem overhead. `/etc/fstab` mounts it at boot; `nfs-server`
is enabled. Keep the NVMe's free space above this pilot budget.

```text
/srv/mist-storage/
  metadata/datasets/<id>.json     owner, checksum, size, ready state
  .uploads/<id>/                 staging, removed on failure/completion
  datasets/<id>/content/          selected input files, read-only job mount
  jobs/<job-id>/outputs/          job's own persistent results
  legacy/{cpu,nvidia,tenstorrent}/<job-id>/  copied original results
```

The upload limit is 2 GiB per file/archive and 2 GiB expanded. ZIPs reject unsafe
paths, symlinks, special files, duplicate files and more than 10,000 entries.
Uploads stream to staging, are checksummed and published atomically. A failed
upload never becomes Ready. Metadata, unrelated jobs and other datasets are not
mounted into a workload. No archive is executed by the upload service.

`/inputs` and `MIST_INPUT_DIR` identify an attached dataset. `/outputs` and
`MIST_OUTPUT_DIR` identify persistent outputs; `/checkpoints/<job-id>` remains an
alias. Jobs without a selected dataset have no `/inputs` mount. Image entrypoints
and working directories remain unchanged unless explicitly overridden.

The budget applies to all shared files together, not separately to each PVC.
Two static RWX PV/PVCs expose the same backing store to different namespaces.
A PVC request alone is not a hard quota; the image's filesystem enforces capacity.
Upload admission leaves 1 GiB spare, but running jobs can still fill storage:
write errors must be handled by job code. Per-member/job disk quotas are future
work. Do not store SQLite on NFS.

Original local checkpoint PVCs remain intact. Their completed outputs were
copied to `legacy/` and remain downloadable through retained job IDs. New jobs
use shared storage. QuietBox is now a storage dependency: if it is offline, mounts,
uploads, downloads and jobs that need those files may wait. NFS is not a backup.

### Cleanup and retention

Nothing deletes datasets or results automatically. Delete unneeded datasets
through the website; active-job references prevent deletion. For job results,
first verify the owning job has finished and no pod can write to it, then archive
and remove only that job's directory from `jobs/<job-id>/outputs` on QuietBox.
Keep the parent directory for retained Kubernetes Job history. Do not delete
storage metadata, the mounted image file, or entire legacy PVC directories.
Monitor `df -h /srv/mist-storage` on QuietBox. Remove only test output archives
that have already been saved elsewhere. Legacy copies use additional space;
original PVCs are intentionally not removed by this migration.

## Authentication and ownership

Better Auth 1.7.7 handles passwords, sessions, database migrations, role checks,
rate limiting and revocation. Go verifies each session with the auth service;
unavailable auth returns 503, anonymous access 401. Browser mutations require
trusted origins. Cookies are HttpOnly and SameSite=Lax; Secure requires the HTTPS
configuration described above. Session duration is seven days.

New members receive stable owner labels derived from their auth user ID. Existing
`utmist` jobs are mapped to the initial administrator's configured email. Other
members cannot list, inspect, cancel, read logs from or download that owner's
jobs, and cannot use/delete its datasets. Hardware totals are visible to members
because those devices are shared. Administrators' membership permissions do not
automatically give access to another member's job files.

This is a private, trusted team pilot. Approved containers, TT's read-only host
runtime mounts and NFS Unix permissions do not constitute a hardened hostile
multi-tenant sandbox. Public hosting, hostile code isolation, member quotas,
credits, priority/fair queues, Jupyter and job chaining require later work.

## CLI

```bash
export MIST_API_URL=http://100.73.139.66:8088/api
bin/mist-cli auth login --email your-member-email@example.org
bin/mist-cli job list --all
bin/mist-cli job submit train.py --compute NVIDIA --devices 1
bin/mist-cli auth logout
```

Login prompts without echoing the password; `--password-stdin` supports controlled
automation. Cookies are saved atomically in a mode-0600 CLI config. Logout revokes
the session. Fake legacy tokens are no longer used. An alternate config can be
selected with `--config`; never paste a session cookie into a public document.

## API additions

All data routes require an authenticated cookie when `MIST_AUTH_URL` is configured.

| Route | Function |
|---|---|
| `GET /session` | Auth enabled flag and current member; no session token |
| `/auth/*` | Same-origin proxy to maintained Better Auth endpoints |
| `GET /storage` | Actual capacity/free bytes and upload limit |
| `GET /datasets` | Current owner's ready datasets |
| `POST /datasets?filename=data.csv&name=Example` | Raw streaming file upload |
| `POST /datasets?filename=data.zip&format=zip` | Checked ZIP upload/extraction |
| `GET /datasets/<id>` | Own ready dataset metadata |
| `DELETE /datasets/<id>` | Delete own dataset if no active job references it |
| `POST /jobs` with `dataset_id` | Mount own ready dataset read-only |
| `GET /jobs/<id>/files` | Own job's regular result files |
| `GET /jobs/<id>/files/download?path=model.pt` | Stream attachment/download |

The website prefixes Go routes with `/api`; auth stays at `/auth`. Listing limits
outputs to 5,000 files/10,000 entries/20 directory levels. Download paths use Go's
`os.Root` to prevent traversal and escaping symlinks. Save plain regular files
inside `/outputs` if you want them to be listed/downloaded.

## Deployment, restart and rollback

See [deployment scripts](../deploy/private/README.md). k3s server/agent, NFS and
Nginx are system services. Kubernetes restores API/auth deployments. The frontend
is a static release, with `/srv/mist-web/current` pointing at the active build.
Deployment configuration, RBAC, claim templates and image policies are checked
in; bootstrap credentials and cluster tokens are not.

Rollback frontend by repointing `current` to an earlier retained release and
reloading Nginx. Rollback an API image with `kubectl set image` after importing
that image. Keep auth, shared storage and login enabled during rollback; the old
unauthenticated foundation API is not a suitable private member-service rollback.
Database migrations require a pre-upgrade backup and version-specific review.

## Verification

Repeatable checks:

- `deploy/k3s/verify_complete_browser.mjs`: actual login, dataset upload, CPU,
  two NVIDIA GPUs and a TT board consuming the same uploaded dataset, downloads,
  member creation/password change/isolation/deactivation, denied anonymous signup
  and cross-origin writes. Uses protected local credentials; logs no secrets.
- `deploy/k3s/verify_quietbox_submission.py`: authenticated submission from
  QuietBox, NVIDIA training assigned to the main machine, downloaded loss metrics.
- `src/shared_storage_test.go`: upload limits/atomic publication, ZIP attacks,
  read-only dataset binding, escaping download symlinks, ownership and auth outage.
- Existing job/device/queue/cancellation/image tests remain in the repository.

Detailed evidence is under `/home/utmist/mist-complete-results-2026-10-09`;
final verified results are recorded in `deploy/k3s/complete-results.json`. An accelerator smoke test proves this small
training workload; it does not prove arbitrary model/runtime compatibility.
