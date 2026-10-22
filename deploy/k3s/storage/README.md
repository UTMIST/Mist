# Shared folders over direct Ethernet

Storage implementation for the two-machine installation, October 21, 2026.
The authoritative data for new submissions is an ordinary directory on QuietBox's
existing disk. There are no new disk images, loop devices or per-team mounts.

```text
NVIDIA enp6s0 192.168.240.1/30
             |
       direct 1-Gbps Ethernet
             |
QuietBox enp200s0 192.168.240.2/30
  /srv/mist-data/
    .mist-storage-layout.json
    metadata/provision-requests/       API-owned desired teams
    metadata/provision-status/         readiness and sampled usage
    teams/<team-id>/
      datasets/
        metadata/datasets/             published dataset records
        .uploads/                      upload staging
        common/datasets/<id>/content/
        members/<hash>/datasets/<id>/content/
      results/
        common/jobs/<id>/outputs/
        members/<hash>/jobs/<id>/outputs/
```

## Access and limits

- Accounts and sessions remain in the main-node local SQLite PVC, separate from
  shared files. Go handles authentication and team authorization.
- All members can read their team's folders. A member writes their own folder;
  common writes require permission. Other teams require an explicit grant.
- Kubernetes mounts only the selected dataset and the job's output directory.
  Datasets are read-only at `/inputs`; results are writable at `/outputs`.
- `storage_gib` and `model_storage_gib` remain API fields, with defaults of
  200 and 500 GiB. They are **advisory allowances**. They neither reserve physical
  space nor prevent job writes. They may be raised/lowered without migration.
- Every team shares the underlying disk. No equal-space guarantee or automatic
  backup, replication, dataset cache or result deletion is provided.
- Uploads use current disk free space minus 1 GiB, plus an optional
  `MIST_MAX_DATASET_GIB` ceiling. Jobs can still exhaust the shared disk.
  ZIP processing needs both archive and extraction space. Transfers stream;
  interrupted uploads restart. Existing CPU/device/runtime policies remain hard
  admission limits.

## Provisioning and usage

`team_folders.py` is a local root-owned service without a network listener or
cluster credentials. It accepts bounded API-owned manifests, creates directories,
rejects symlinks/unexpected mounts and publishes `shared-folders-v1` readiness.
Usage counts logical regular-file bytes, excludes symlinks, and deduplicates hard
links within each pool. The host samples every 30 seconds; active writes may make
these approximate. The API does not walk the NFS directory tree for every poll.

`GET /storage` reports:

- `capacity_bytes`, `available_bytes`: actual shared filesystem space.
- `dataset_used_bytes`, `model_used_bytes`: sampled team file usage.
- `model_capacity_bytes`, `model_available_bytes`: compatibility aliases for the
  same shared disk, not a separate model filesystem or reservation.
- `dataset_allocation_gib`, `model_allocation_gib`: advisory values.
- `storage_limits_enforced: false`, `storage_mode: shared-folders`.
- `usage_observed_at`, `usage_stale`, `storage_low`: freshness and disk warning.

The old `GET /teams` field `storage_budget_gib` remains informational shared
capacity for compatibility. Allocations are not summed into a reservation budget.

## Host setup

Run commands as root in the **owning host's** mount/network namespace. Scripts
are installation-specific; verify interface names/addresses on another system.

On NVIDIA:

```bash
bash deploy/k3s/storage/setup-storage-link.sh main
```

On QuietBox, from a checkout/copy of these files:

```bash
bash deploy/k3s/storage/setup-storage-link.sh quietbox
bash deploy/k3s/storage/setup-quietbox.sh
install -d -m 0755 /usr/local/lib/mist
install -m 0755 deploy/k3s/storage/team_folders.py /usr/local/lib/mist/team_folders.py
install -m 0644 deploy/k3s/storage/mist-team-storage.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now mist-team-storage
```

The NetworkManager profile `mist-storage-direct` persists addresses without a
gateway. Existing LAN/default routes, Tailscale and k3s node addresses stay in use.
NFS permits only `192.168.240.1` and `192.168.240.2`; the firewall allows main's
TCP 2049 on `enp200s0`. Root squash maps root requests to the storage API identity.

New bindings use NFS **4.1**. During inspection, Linux reused an existing 4.2
connection to QuietBox's old LAN address even when mounting its Ethernet alias.
A separate 4.1 connection makes the new Ethernet transport verifiable without
tearing down existing old mounts. Verify both the mount and TCP connection, not
only the requested mount address.

The initial cutover uses new `mist-folder-api`/`mist-folder-jobs` bindings and
per-team `team-datasets`/`team-results` claims, leaving old bindings intact.
Apply `shared.yaml` and the API deployment, then wait for new team readiness.
`ensureVolume` refuses an existing binding pointing to a different NFS export.

## Prelaunch reset and rollback

The user chose fresh storage without data migration. Old files under
`/srv/mist-storage` and its backing images remain untouched as rollback material.
Old retained Jobs may still appear in history; their old files are not copied to
the new root. New datasets and results use `/srv/mist-data`.

Before cutover, record the API/auth deployment specifications, web release target
and team policies. Create a consistent SQLite backup using its database backup
API. Pause the old API/auth before the Go-auth API takes the shared auth PVC.

Rollback restores the previous API/auth deployment and provisioning unit, and
repoints Nginx's web release symlink. Old volumes were not replaced or deleted.
An operational rollback after accepting new writes must account for those writes;
this is a prelaunch cutover, not automatic bidirectional migration.

## Verify

```bash
python3 -m unittest discover -s deploy/k3s/storage -p test_folders.py -v
python3 deploy/k3s/verify_storage_folders.py --out /tmp/mist-storage-check
```

The live verifier creates two workspaces and a regular account, checks small
uploads/downloads, archive rejection and team isolation, then trains on one
NVIDIA GPU and one n300 board. It retains a protected account file in its output
directory for optional browser review; never commit that file. Completed test
jobs/results remain inspectable. It does not migrate or delete old research data.

Optional browser review uses Playwright from `MIST_PLAYWRIGHT_MODULE` (the local
installation defaults to `/tmp/mist-browser-check/node_modules/playwright/index.mjs`):

```bash
MIST_STORAGE_EVIDENCE_DIR=/tmp/mist-storage-check node deploy/k3s/verify_storage_folders_browser.mjs
```

Recorded live results: [October 21 evidence](../evidence/storage-2026-10-21/README.md).
