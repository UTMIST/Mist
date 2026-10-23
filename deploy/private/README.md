# Private deployment operations

Current user/admin and architecture guide: [department rollout](../../docs/content/department-rollout.md).
Execution/evidence: [rollout checklist](../../docs/content/department-rollout-execution.md).
Setup: [k3s](../../docs/content/k3s-setup.md), [Tailscale/SSH](../../docs/content/tailscale-ssh.md),
[storage](../../docs/content/storage.md), and [developer setup](../../docs/content/development.md).

## Services and persistent state

| Component | Location | Persistent state |
|---|---|---|
| Nginx production portal | Main, localhost and `100.73.139.66:8088` | `/srv/mist-web/releases`, atomic `current` symlink |
| Go API, auth, and admission controller | `mist-system`, main, one Recreate replica | Team/queue ConfigMaps; Kubernetes Jobs; Local `mist-auth-data` SQLite WAL PVC |
| k3s server / agent | Main / QuietBox | Existing cluster and device operators |
| NFSv4 storage | QuietBox | Ordinary `/srv/mist-data/teams` dataset/result folders over direct Ethernet |
| Team provisioning agent | QuietBox `mist-team-storage.service` | Validated requests, ordinary directories and sampled usage |
| Portal host firewall | Both `mist-portal-firewall.service` | Owned INPUT/FORWARD chains and host config |

API image tag is `mist-api:folders-20261021`, imported into main's k3s containerd.
New jobs run in `mist-team-<id>` namespaces. Historical `mist` workloads/PVCs remain.
Never scale the current API above one replica: admission requires one controller.

## Build and redeploy on main

Run from a checkout of the reviewed current main on the NVIDIA host. Required:
Go 1.26.0, Node 22.16+, working Docker access, the private kubeconfig, existing
auth Secrets, GPU/TT operators and mounted QuietBox storage. The script is for
this existing installation; it is not a fresh-cluster/bootstrap installer.
Checking out/merging Git does not restart or deploy services automatically.
The API is Kubernetes-only; Redis and the retired Docker supervisor are unnecessary.


```bash
bash deploy/private/build-and-deploy.sh
```

The script builds API/CLI and the API container image, imports it, applies
RBAC/admission/network manifests, waits for API rollout, installs web
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
rollback for research users. The pre-cleanup department release `3431ffd` contains
the verified team architecture; rebuild a compatible department version rather
than restoring the archived pre-platform main for normal users.

## QuietBox host setup and storage cutover

Follow [shared-folder storage operations](../k3s/storage/README.md). This supersedes
image-backed setup, online filesystem growth and model-pool migration instructions.
New storage uses `/srv/mist-data` over `192.168.240.2` and new NFS volume bindings.
Existing image files and bindings are retained for rollback; fresh storage was
requested without data migration. No physical disk is formatted.

Team dataset/model values are advisory. Track actual shared-disk free space and
sampled team file usage. Uploads preserve 1 GiB of disk headroom; running jobs can
still exhaust storage. New Kubernetes PV/PVC sizes are binding declarations,
not capacity reservations or write limits.

The October 21 rollout also preserves existing Better Auth password hashes and
deactivated accounts in Go auth, and requires real unexpired sessions. Session
cookies based only on user IDs are rejected. Auth data remains on its existing
local PVC; a consistent pre-cutover database snapshot is retained privately.

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

## Services to restart and inspect

Use a root-capable administrator session for service mutations. Logs are available
through `journalctl -u <service>` on the owning host and `kubectl logs` for pods.
Ordinary application redeployment should use the build script; restart k3s/NFS
only when resolving their own infrastructure problem because workloads depend on them.

| Component | Owner | Status command |
|---|---|---|
| Portal | Main | `systemctl status nginx --no-pager` |
| API, auth, and controller | Cluster | `k3s kubectl -n mist-system rollout status deployment/mist-api` |
| Control plane | Main | `systemctl status k3s --no-pager` |
| Worker | QuietBox | `systemctl status k3s-agent --no-pager` |
| Shared storage | QuietBox | `systemctl status nfs-server mist-team-storage --no-pager` |
| Private network | Both | `systemctl status tailscaled --no-pager` |
| Portal restrictions | Both | `systemctl status mist-portal-firewall --no-pager` |

The `mist-api-forward` and `mist-web` user services are local development helpers,
not requirements for the production portal. The default-namespace test Nginx and
k3s's Traefik also do not serve the current portal; it uses host Nginx on 8088.

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
findmnt -T /srv/mist-data
df -h /srv/mist-data
```

With root access, inspect `exportfs -v` and the provisioning service journal if a
team is not ready. Anonymous `/api/jobs` must return 401; the website must allow
real login and show two NVIDIA GPUs/four TT boards. Both nodes must be Ready.

## Troubleshooting

- **401:** Sign in again. SSH/Tailscale identity is separate from Mist identity.
- **403:** Confirm active team membership, scope writing authority and current
  policy. Administrators also need team membership to use files/jobs.
- **503:** Check auth and API pods, DNS, shared directory readiness and provisioning
  status. Access and missing team mounts fail closed.
- **Queued:** Check per-team concurrency/resources, node health and device
  availability. Aged large requests drain their target node; no preemption.
- **507 / ENOSPC:** Team filesystem is full. Check actual QuietBox disk space; advisory allowances do not add capacity.
  Delete only unneeded datasets that have no active job references. No automatic result cleanup was added.
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
