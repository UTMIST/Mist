# Private deployment operations

Full user/admin/storage/API guide: [complete foundation](../../docs/complete-foundation.md).

## Current services

- Nginx on main node: localhost and `100.73.139.66:8088`, static frontend and
  API/auth proxy. Other pre-existing Nginx endpoints are preserved.
- API and auth: Kubernetes deployments in `mist-system`, pinned to main node.
- Auth database: `mist-auth-data` local PVC, SQLite WAL. Not on NFS.
- Shared store: `/srv/mist-storage` on QuietBox, NFSv4, two static RWX PVCs.
- Backend image references: `mist-api:complete-20261009`,
  `mist-auth:members-20261009`, imported in main node's k3s containerd.

## Install/redeploy

`build-and-deploy.sh` builds the two images, imports them, applies manifests,
restarts the deployments and installs a production frontend release. Run it on
this main node as its existing Docker/Kubernetes-authorized user. Host operations
use the already-authorized Docker bind/chroot access; no sudo policy change is
needed. The script expects the existing auth bootstrap Secret and initialized
storage. It never creates an unauthenticated replacement.

For a new installation, create the protected bootstrap JSON outside Git with a
random auth secret (at least 32 characters) and a password (12–128 characters),
then create the `mist-auth-bootstrap` Kubernetes Secret with keys
`BETTER_AUTH_SECRET`, `MIST_ADMIN_EMAIL`, `MIST_ADMIN_PASSWORD`. Do not pass secret
values as shell arguments, commit a Secret manifest, or print the Secret YAML.
Prepare the storage server first: install `nfs-kernel-server nfs-common` on
QuietBox and `nfs-common` on main, and run
`deploy/k3s/storage/setup-quietbox.sh` as root in QuietBox's actual host mount
namespace. This creates/formats only a new image file and preserves disks/PVCs.

The checked-in configuration matches this two-node installation. Before moving
machines or IPs, update node selectors, NFS server/client addresses, Tailnet
listener and allowed origins, and Nginx's API ClusterIP. Regenerate neither the
SQLite database nor auth secret on ordinary deploys.

`install-web.sh` keeps timestamped releases under `/srv/mist-web/releases` and
atomically changes `current`. It validates Nginx before reload. Its systemd
override orders startup after Tailscale/k3s and retries failed binds.

## Check after startup

```bash
systemctl is-active k3s tailscaled nginx
KUBECONFIG=/home/utmist/.kube/config k3s kubectl get nodes
KUBECONFIG=/home/utmist/.kube/config k3s kubectl get pods -n mist-system
curl -fsS http://127.0.0.1:8088/api/session
```

On QuietBox check `systemctl is-active k3s-agent nfs-server`,
`mountpoint /srv/mist-storage`, `df -h /srv/mist-storage`, and `exportfs -v`
with appropriate root access. Login must work through the website; anonymous
`/api/jobs` must return 401. Check both device pools have their expected capacity.

## Backups and restoration

Run `backup-auth.sh` on main. It uses SQLite's online backup API, not a copy of a
live WAL file. Backups are mode 0600 in a mode-0700 directory. Back up the matching
Better Auth secret separately in protected storage. Copy snapshots to another
machine/disk: a backup on the same main disk does not protect against disk loss.

For shared files, copy ready datasets, metadata, results and legacy files from
QuietBox to a separate backup disk/location. Skip `.uploads`. Coordinate a paused
submission/upload window and finished jobs for a consistent full snapshot; do
not archive an image file while its filesystem is being written. No remote
backup destination was supplied, so off-machine scheduled backup is not enabled.

Restore auth with the deployment scaled to zero, restore a validated database
snapshot to the existing local volume with UID/GID 65532 permissions, and restore
the matching auth secret. Remove stale `-wal`/`-shm` files only while auth is
stopped, then start the deployment and verify login. Existing login sessions in
a snapshot may be stale; revoke them after a recovery. Never replace a live
SQLite database under a running auth process.

Restore shared directories with API submissions/uploads stopped and all writing
jobs finished. Preserve IDs and metadata ownership. Verify the NFS export/mount,
owner's dataset list and a downloaded checksum before resuming. The original
checkpoint PVCs remain available as a separate migration fallback.

## Troubleshooting

- API 401: sign in; SSH/Tailscale login is not a Mist session.
- Auth 503: check auth Deployment, Service/DNS and database; API fails closed.
- NFS mounts wait: QuietBox must be online; TCP 2049 must be admitted from the
  two node LAN IPs. Preserve its router link.
- Out of space: inspect the bounded shared filesystem and remove only finished,
  backed-up outputs or unneeded datasets. Member/job quotas are not yet separate.
- 429 at login: maintained auth rate limiting; retry after its indicated delay.
- Image denied: administrator approves exact references in `MIST_ALLOWED_IMAGES`.
  Pull secrets are named in `MIST_IMAGE_PULL_SECRETS` and exist in namespace `mist`.
- TT image/model failure: only the tested TT runtime profile is supported; do not
  infer model portability from an accelerator being allocatable.
- HTTPS: see the guide's Serve prerequisite; no public Funnel is configured.
