# Storage and upload limits

Current department storage on **QuietBox**, verified October 9, 2026. Both
compute nodes mount scoped NFSv4 paths over their existing LAN.

## Layout and authority

```text
QuietBox /srv/mist-storage-volume.img  (sparse base filesystem)
└── /srv/mist-storage/
    ├── metadata/                     API-owned requests and metadata
    ├── legacy/                       preserved historical files
    └── teams/<team-id>/              bounded dataset filesystem
        ├── common/datasets/
        ├── members/<member-key>/datasets/
        └── .models/                  separate bounded model/result filesystem
            ├── common/jobs/<job-id>/outputs/
            └── members/<member-key>/jobs/<job-id>/outputs/
```

Member directory keys are derived internally; use API/UI scopes rather than
constructing filesystem paths. Teammates can read their team's common/member
folders. Members write their own folders; common writing needs assigned authority.
Other teams need an explicit read/use grant. Cross-team grants never permit writes.
Administrators must belong to a team to use its storage.

Inputs mount read-only at `/inputs`; each job mounts only its own result directory
at `/outputs` and the checkpoint alias. Auth data lives on main's local SQLite
PVC. File ownership/permissions support the mounts; API and workload policy
provide authorization. They are not separate hard quotas for every member.

## Capacity

| Limit | Current value/behavior |
|---|---|
| New team datasets | **200 GiB total**, administrator configurable |
| New team models/results | **500 GiB total**, administrator configurable |
| Base logical capacity | **1,600 GiB** sparse ext4 image |
| Combined team allocation budget | **1,500 GiB** across dataset and model pools |
| Per-member hard quota | None; members share their team's corresponding pool |
| Expansion/shrink | Online expansion supported; shrink rejected |
| Team disable/removal | Preserves files and reserved allocation |

At most two full 700-GiB default allocations fit within the budget before other
reserved allocations are counted. Existing teams retain their configured sizes;
new defaults do not silently resize them. Administrators can allocate smaller
pools for more teams. Policy overcommit is rejected before persistence.

Dedicated bounded filesystems enforce both upload capacity and workload result
writes. PVC declarations alone do not enforce NFS capacity. The base/PVC sizes
may retain earlier declarative values; actual filesystem checks are authoritative.

The image is sparse, so logical quota is not a physical disk reservation. During
the audit the base contained about **1.4 GiB** of data, the backing file consumed
about **5.9 GiB** physically and QuietBox had **1.9 TiB** free. These are dated
observations, not permanent capacity guarantees. The earlier unused bulk
reservation was released; unrelated research files were preserved.

## Uploads

Use **Datasets → Add dataset**, choose your own/common folder and upload a file
or ZIP. Bodies stream through Nginx and the API. Capacity is bounded by dataset
free space minus a **64 MiB metadata reserve**, with an optional lower
`MIST_MAX_DATASET_GIB` ceiling. There is no fixed 64 GiB upload limit.

Transfers can run for up to **24 hours** and can be cancelled. An interrupted
transfer restarts from the beginning; resumable uploads are not implemented.
ZIP uploads require space for staging plus expanded content and reject unsafe
paths/links and expansion beyond the allowed limit. Dataset deletion is blocked
while any active/queued/terminating authorized job references it.

Use the saved Files panel to download results. There is no automatic results
cleanup or retention service. Jobs that write outside `/outputs` do not retain
those container files.

## Administration and inspection

Set dataset/model sizes in **Teams → Manage → Limits**. The QuietBox provisioning
service checks manifests, mounts bounded images and publishes distinct NFS exports.
The API reports storage ready only when both mounts have the expected identities
and capacities. Missing model storage cannot borrow dataset capacity.

On QuietBox:

```bash
systemctl status nfs-server mist-team-storage --no-pager
mountpoint /srv/mist-storage
df -h / /srv/mist-storage
findmnt -t ext4,nfs,nfs4
```

With root access, inspect `exportfs -v`, per-team mounts and the storage-agent
journal when readiness fails. Migration/expansion and service installation are
in [deployment operations](../deploy/private/README.md). Never truncate or unlink
mounted image files, recreate populated filesystems or treat team removal as cleanup.

Small-file migration, quota bounds and real accelerator input/output paths passed.
The earlier 2 GiB + 4 MiB test is historical evidence; large transfers and
exhaustion were not repeated after the user's request. No 200/500 GiB stress,
automatic backup or disk-failure recovery is claimed. See [testing](testing.md).
