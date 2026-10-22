# Shared folders and Ethernet storage — October 21, 2026

## Scope

Implement on `feat/simple-storage-ethernet-20261021` from main `ba2bb86`.
The user authorized implementation and verification on both machines, with fresh
storage, no data migration and no large storage tests. Preserve main's Go auth.

## Target

- Dedicated direct Ethernet: NVIDIA `enp6s0` at `192.168.240.1/30`, QuietBox
  `enp200s0` at `192.168.240.2/30`. No gateway; retain existing LAN/cluster paths.
- QuietBox ordinary directory `/srv/mist-data`, exported over NFS on the direct
  link; no filesystem images or formatting. Keep old storage as rollback material.
- Team layout: `teams/<id>/datasets/{common,members,...}` and
  `teams/<id>/results/{common,members}`. Only trusted API metadata lies at the root.
- Existing dataset/model allowance fields remain API-compatible but become
  advisory. Actual disk free space bounds uploads. CPU/GPU/runtime limits remain
  enforced. Team grants and scoped workload mounts remain authoritative.
- A small root-owned local provisioning agent creates directories, refuses unsafe
  paths/mounts, and publishes logical byte usage. The API validates readiness and
  serves the usage record without recursively scanning NFS on every request.
- Preserve `/inputs`, `/outputs`, dataset IDs and wire fields for new jobs.

## Execution

1. Establish baseline tests and inspect running Jobs, mounts and network profiles.
2. Persist Ethernet addresses; verify both directions and measure a small transfer.
3. Replace image provisioning with folder provisioning; adapt backend, UI and tests.
4. Install fresh NFS root/provisioner, prepare current team policies there and
   switch storage volumes during a controlled API pause. Do not delete old disks.
5. Verify small uploads/downloads, cross-team isolation, and actual NVIDIA/TT jobs.
6. Record sanitized evidence, document operations, commit and push a reviewable PR.

## Verification boundaries

No hundreds-of-GB allocations/uploads, host-disk formatting, unrelated auth
redesign or reconfiguration of cluster node addresses. Keep secrets outside Git.
Main's independent-review rule still applies to merging the eventual PR.

## Completed

- Both persistent Ethernet profiles configured and bidirectional connectivity
  verified. A 16 MiB direct-I/O NFS sample measured 43 MB/s write, 101 MB/s read.
- Fresh ordinary-folder storage deployed; no old-data migration or deletion.
  New NFS 4.1 volume bindings avoid reusing the old 4.2 LAN transport.
- Folder provisioner, backend usage/readiness, advisory UI and developer fake
  backend updated. Existing grants, scopes and accelerator admission retained.
- Latest main's Go auth required targeted compatibility/session fixes before
  deployment: valid database sessions, existing scrypt hashes/schema, and inactive
  accounts. Existing credentials were checked privately on a database snapshot.
- Real regular-member upload/download/isolation checks, NVIDIA/TT training,
  checkpoint reload/download and browser review passed with small files.
- [Sanitized evidence](../../deploy/k3s/evidence/storage-2026-10-21/README.md) and
  [operations](../../deploy/k3s/storage/README.md) contain the current contracts.

Human-authored October 9 storage operating snapshots under `docs/content` still
describe the previous image-backed release. README/operations now point to the
current guide; maintainers should reconcile those snapshots when updating human
documentation. This task follows the repository's README/agent-doc editing scope.
