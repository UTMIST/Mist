# Private deployment operations

Current user/admin and architecture guide: [department rollout](../../docs/department-rollout.md).
Execution/evidence: [rollout checklist](../../docs/department-rollout-execution.md).

## Services and persistent state

| Component | Location | Persistent state |
|---|---|---|
| Nginx production portal | Main, localhost and `100.73.139.66:8088` | `/srv/mist-web/releases`, atomic `current` symlink |
| Go API and admission controller | `mist-system`, main, one Recreate replica | Team/queue ConfigMaps; Kubernetes Jobs |
| Better Auth | `mist-system`, main, one Recreate replica | Local `mist-auth-data` SQLite WAL PVC |
| k3s server / agent | Main / QuietBox | Existing cluster and device operators |
| NFSv4 storage | QuietBox | 1,600 GiB sparse image; separate bounded dataset/model filesystems |
| Team provisioning agent | QuietBox `mist-team-storage.service` | Validated manifests, fstab mounts and NFS exports |
| Portal host firewall | Both `mist-portal-firewall.service` | Owned INPUT/FORWARD chains and host config |

API/auth image tags are `mist-api:departments-20261009` and
`mist-auth:departments-20261009`, imported into main's k3s containerd. New jobs
run in `mist-team-<id>` namespaces. Historical `mist` workloads/PVCs remain.
Never scale the current API above one replica: admission requires one controller.

## Build and redeploy on main

```bash
bash deploy/private/build-and-deploy.sh
```

The script builds API/CLI and both container images, imports them, applies
RBAC/admission/network manifests, waits for auth/API rollout, installs web
packages, type-checks/builds the SPA and atomically installs its production
release. It requires the existing Docker and Kubernetes access. Host imports
and web installation use authorized Docker bind/chroot/nsenter access; the
process must enter the actual host mount/network namespace for host changes.

Bootstrap credentials remain outside Git in mode-0600 local files and Kubernetes
Secrets. `prepare-internal-secret.py` reuses the internal credential on redeploy.
Never recreate the auth database, change its signing secret or print Secret YAML
as part of an ordinary deploy. The script fails if bootstrap credentials are
missing. Fresh-host installation needs deliberate address/node configuration,
storage preparation and protected Secret creation; this is a deployment of the
existing two-machine installation.

Frontend-only release:

```bash
npm --prefix web-interface run build
# As root in the actual main host namespace:
bash deploy/private/install-web.sh /home/utmist/Mist
```

Use the pinned Node/Go runtime locations from the build script. A frontend
rollback can repoint `/srv/mist-web/current` atomically to a previous complete
release. Keep its API contract compatible. For a backend regression, rebuild a
known department version with the same manifests/Secrets/state. The earlier
account-only API does not enforce team policies and is not a safe ordinary
rollback for research users.

## QuietBox host setup

The existing `deploy/k3s/storage/setup-quietbox.sh` creates only the bounded base
image when missing; it never formats a physical disk. Base exports use `crossmnt`
to expose explicitly mounted child filesystems. Before changing storage, inspect
mounts and existing exports. Do not replace a live filesystem with an empty image.

Install the new provisioning service as root on QuietBox:

```bash
install -d -m 0755 /usr/local/lib/mist
install -m 0755 deploy/k3s/storage/team_volumes.py /usr/local/lib/mist/team_volumes.py
install -m 0644 deploy/k3s/storage/mist-team-storage.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now mist-team-storage
```

The agent reads API-owned requests outside workload mounts, enforces the combined
1,500 GiB dataset + model allocation budget, refuses unknown images/nonempty covered directories,
and supports online growth. Team disable/removal never deletes files or frees an
allocation. Shrinking is rejected. New teams default to 200 GiB datasets and 500 GiB models/results. Per-member
folders share the applicable team pool's hard filesystem capacity. PV/PVC sizes are declarations; ext4 enforces actual writes.

NFS permits only `10.0.0.175` and `10.0.0.112` over the existing LAN. Keep QuietBox
online and its router/network connection intact. No separate switch or direct
cable is required for this pilot.

## Dataset request limits

Uploads are bounded by actual dataset free space minus a 64 MiB metadata reserve.
An optional `MIST_MAX_DATASET_GIB` ceiling can lower this (1–1,048,576 GiB).
There is no fixed 64 GiB cap. Nginx streams bodies without buffering; authenticated
API admission enforces capacity. Browser and Go allow 24-hour transfers. ZIP
archives need both staging and expanded space. Cancellation is supported;
interrupted transfers restart from the beginning.

## Existing store expansion

`storage/grow-quietbox-store.py --gib 1600` expands only the known, correctly
labelled image, after checking host headroom. It grows sparsely and never formats
physical disks or shrinks existing filesystems. The current image was expanded
online; its initial physical preallocation was released with
`fstrim -v /srv/mist-storage`. Do not unlink or truncate a mounted backing file.
The host had about 1.9 TiB free after release. Logical capacity is not a physical
reservation; account for future image growth alongside existing research files.

Migration must run without active team jobs and with API submissions paused.
Restart the updated storage agent, wait for both pool capacities to be Ready,
then deploy the updated API. Old policies with no `model_storage_gib` retain the
old allocation independently for each pool. Preserve credentials/auth data.

## Portal network rules

Install `portal-firewall.sh` under `/usr/local/lib/mist` and the accompanying
systemd unit on both hosts. `/etc/mist-portal-firewall.conf` contains
`MIST_PORTAL_PORT=8088` on main and `MIST_PORTAL_PORT=0` on QuietBox. Enable/restart
`mist-portal-firewall` after installation. The unit follows Tailscale/k3s service
restarts; manually reapply after UFW/netfilter resets.

The script restricts ordinary sources on `tailscale0` to main TCP 8088 and denies
forwarded cluster traffic. The explicit administrator/infrastructure IPv4 and
IPv6 allowlist is in the script. Update it before relying on a new administrator
device. LAN rules and cloud Tailscale ACL/sharing settings are unchanged.
`verify-portal-firewall.sh` checks the actual installed chains using an isolated
synthetic untrusted source and removes its temporary namespace/rules afterward.
It requires root in the main host network namespace.

## Check after startup

Main:

```bash
systemctl is-active k3s tailscaled nginx mist-portal-firewall
KUBECONFIG=/home/utmist/.kube/config k3s kubectl get nodes
KUBECONFIG=/home/utmist/.kube/config k3s kubectl get pods -n mist-system
curl -fsS http://127.0.0.1:8088/api/session
```

QuietBox:

```bash
systemctl is-active k3s-agent tailscaled nfs-server mist-team-storage mist-portal-firewall
mountpoint /srv/mist-storage
df -h /srv/mist-storage /srv/mist-storage/teams/team-*
```

With root access, inspect `exportfs -v` and the provisioning service journal if a
team is not ready. Anonymous `/api/jobs` must return 401; the website must allow
real login and show two NVIDIA GPUs/four TT boards. Both nodes must be Ready.

## Troubleshooting

- **401:** Sign in again. SSH/Tailscale identity is separate from Mist identity.
- **403:** Confirm active team membership, scope writing authority and current
  policy. Administrators also need team membership to use files/jobs.
- **503:** Check auth and API pods, DNS, child filesystem mounts and provisioning
  status. Access and missing team mounts fail closed.
- **Queued:** Check per-team concurrency/resources, node health and device
  availability. Aged large requests drain their target node; no preemption.
- **507 / ENOSPC:** Team filesystem is full. An administrator can grow it within
  the allocation budget. Delete only unneeded datasets that have no active job
  references. No automatic result cleanup was added.
- **Image rejected:** Use an explicit tag/digest from an allowed registry. No
  per-image approval is needed. Private pull-credential management is excluded.
- **Image pull/runtime failure:** Verify the image is publicly accessible and
  includes its dependencies. Custom TT images need compatible TT libraries;
  select container runtime. The tested default TT profile remains available.
- **Revocation pending:** An admin mutation returns 503 if job suspension could
  not be completed; correct cluster connectivity and retry.
- **Login rate limited:** Retry after the indicated delay. Session checks are
  authoritative and do not share the restrictive password-login rate limit.

Existing `backup-auth.sh` is a manual foundation utility; no automatic backup,
recovery, HTTPS/Serve, monitoring or cleanup service was added. HTTP remains
private over Tailscale. There is one control plane, one auth database instance
and one NFS storage host; no high-availability failover is claimed.
