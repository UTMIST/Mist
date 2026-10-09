# Department rollout execution

Authorized October 9, 2026: implement all seven agreed remaining parts on
`feat/local-job-foundation`, preserve existing accounts/jobs/files and verify the
deployed flow on both machines. No permission prompts or cloud deployment.

## Architecture and defaults

- Keep Better Auth as the identity/password authority. Team membership and
  policies are persisted in Kubernetes ConfigMaps with optimistic concurrency.
  Administrators use the same account for admin and ordinary team work.
- Each team has a namespace and separate bounded dataset/model NFS filesystems and PVCs. Inputs mount
  read-only; outputs mount only a job's own directory. Team storage is organized
  as common and member datasets/results. Teammates can read all team folders.
- Members manage their own uploads/results. Common-folder writers are explicitly
  assigned. Only administrators set cross-team grants (read/use, scoped to a
  common or member folder). Cross-team write grants are outside the initial
  implementation; no write authority is inferred from read authority.
- Dedicated ext4 filesystem images enforce team capacity on uploads and workload
  writes. A small QuietBox host service handles trusted provisioning requests,
  mounts, online expansion and NFS exports. It never formats physical disks or
  deletes team data. Provisioning requests live outside workload mounts.
- Combined team allocation budget is 1,500 GiB within the 1,600 GiB sparse
  store. New team defaults are 200 GiB datasets + 500 GiB models/results. Reject allocation
  above the budget. Storage expansion is supported; shrinking is rejected.
- Default team workload policy denies inbound connections and private-network
  outbound access, including direct NFS/API access. Permit DNS and public HTTPS.
  Extend workload admission restrictions to the managed team namespaces.
- A single API/controller replica persists submissions as suspended Kubernetes
  Jobs. Round-robin admission across teams reserves resources for admitted jobs;
  enforce per-team CPU/memory/GPU/board/concurrency/runtime and queue limits.
  Queue state survives restarts. Retry is explicit; do not duplicate training.
- Custom image references are self-service within team registry rules. Preserve
  tested default profiles and validate explicit TT container-vs-host runtime
  selection. Private registry credentials are not part of initial rollout.
- Revocation denies new requests immediately and suspends affected queued/running
  jobs, removing their pods/mounts. Completed output data is retained.
- Legacy per-account work remains available through a clearly selected legacy
  workspace; team work requires explicit membership, including administrators.
- Keep the site private through Tailscale machine sharing. Restrict ordinary
  shared-user network access to the portal while preserving infrastructure and
  known administrator SSH access. No cloud account, Serve or Funnel setup.

## Execution checklist

- [x] Team APIs, membership/admin controls and team workspace selector.
- [x] Team common/member storage, file browsing/download and cross-team grants.
- [x] Filesystem capacity enforcement, namespace and container/network isolation.
- [x] Resource policies and durable fair job admission/controller.
- [x] Self-service image policy, compatible runtime selection and guidance.
- [x] Account reset UI, clear job errors/cancellation, legacy preservation.
- [x] Deploy, verify full browser/API flows, accelerator training, real quota
      exhaustion, denial inside containers, revocation and queue restart tests.
- [x] Document operating limits, deployment/restart and evidence; commit results.

Automatic backups, HTTPS, monitoring/cleanup products, credits, Jupyter, chaining,
browser image archives/builds and distributed multi-machine training are excluded.
Capacity checks and queued-job reconciliation implement the agreed limits and job
behavior; they do not add a separate monitoring service.

## Verified results

Recorded October 9 on the private production portal. Reports and selected logs
are checked in under
[department evidence](../deploy/k3s/evidence/department-2026-10-09/README.md).

- 14 API/cluster/storage checks: membership, write denial, scoped read-only grants,
  running-job revocation, FIFO/fair turns, API restart, cancellation, limits,
  real container network/credential denial, real job-write ENOSPC, accelerator
  training, account deactivation, browser expansion, filesystem growth and
  preservation of all 47 legacy jobs.
- 11 browser checks: admin/member flow, account/team operations, upload/download,
  custom public image submission, logs/results, admin denial, password reset,
  deadline defaults, actual pagination and desktop/mobile layout without JS errors.
- Simultaneous TT training used distinct boards (`tt-0`, `tt-1`), one board/two
  chips per job. NVIDIA training used both GPUs; an SSH CLI submission from TT
  also ran successfully on main's NVIDIA GPU.
- Admission rejected host network/PID, Kubernetes tokens, privileged containers,
  arbitrary host paths and added capabilities. Host firewall allowed the portal
  and denied synthetic untrusted-source SSH/Kubernetes/NodePort/backend sockets.
- Failure exit code and execution deadline were observed without automatic retries.
- Focused Go suites, CLI suite, nine React tests, TypeScript and changed-file ESLint
  passed. Web/auth npm audits reported zero vulnerabilities at verification time.

At that initial phase, the full historical Go integration suite had not run
against its legacy Redis/Docker fixtures. The final release run below completed
those suites successfully. The deployed Kubernetes execution
path does not use Redis. Accelerator tests are regression training, not proof of
arbitrary-model portability. External researcher laptop/cloud ACL verification,
HA, hard per-member storage quotas and deliberately excluded features are not
claimed.

Research pilot remains active with the administrator and sample files/results.
Verification-only accounts are deactivated and verification-only teams disabled;
files/history are retained. Documentation describes current setup, operational
limits and redeployment. Changes were implemented on `feat/local-job-foundation`. See the publication
record below for the subsequent archive and pull request.

## Latest UI and dataset refinement

- Focused native dialogs for job submission, dataset upload, team creation and
  settings, member creation and password reset; desktop and mobile layouts checked.
- Shared searchable Picker, with keyboard selection, empty results, focus return
  and menus that open above near the bottom of the viewport. Shared Tabs support
  arrow keys and Home/End. Device/allocation sliders retain exact number entry.
- Compact team cards, paginated searchable member/history rows, status filters,
  collapsible hardware/file details and a storage usage bar. Compute cards and
  the warm neutral/olive theme are retained. Script editing comes before optional
  data/output settings; technical dataset metadata stays behind a disclosure.
- Uploads stream with a configurable 64 GiB ceiling bounded by actual team free
  capacity. Real **2 GiB + 4 MiB** upload, SHA256, Job input read and removal of
  only the verifier's probe passed. Four-hour requests and explicit cancellation
  are supported; resumable upload is outside this implementation.
- Latest checks: all 11 browser checks, nine React tests, TypeScript, targeted
  ESLint, focused Go/CLI suites, Python parsing and shell syntax passed.

That refinement was completed on `feat/local-job-foundation`. Publication is
recorded below.

## Follow-up: separate large dataset/model pools (requested October 9)

Confirmed defaults: **200 GiB datasets + 500 GiB models/results per team**,
admin configurable. This changes the earlier 64 GiB ceiling and shared quota.
The recorded baseline above passed before this follow-up. Follow-up checks use
small files; large transfers/exhaustion are excluded at the user’s request.

- [x] Expand the known image sparsely; release unused bulk physical preallocation.
- [x] Separate bounded dataset/model filesystems and migrate existing outputs.
- [x] Extend policy/admin controls and expose actual free space for both pools.
- [x] Enforce upload capacity against datasets and job writes against models.
- [x] Verify migration, bounds/denials, small uploads and accelerator jobs.
      Large upload/exhaustion tests are excluded at the user’s request.
- [x] Update current documentation and commit the completed follow-up.

Follow-up evidence: seven small-file live checks, six migration tests, eleven
browser checks, nine React tests, focused Go suites, CLI tests, TypeScript, ESLint
and syntax checks. Verified NFS mount identity, separate exports, member-parent
permissions and preserved checkpoints. QuietBox’s unused bulk reservation and
blocks belonging to the deleted large probe were reclaimed; existing research
files outside Mist remain untouched. See current [rollout](department-rollout.md)
for sparse storage capacity and the two-pool API compatibility rules.

## Release verification and publication (October 9)

- Full Go backend: **33 tests passed, zero skips**, including legacy integration
  tests against a dedicated Redis container with no published ports. Preview
  Redis data was preserved.
- Nested Docker module: **10 tests passed, zero skips**, including concurrent
  container/volume and limit fixtures. Test containers and volumes were removed.
- Full CLI/logger suites, **10 React tests**, TypeScript, full frontend ESLint
  and production build passed. All **11 live browser checks** passed again with
  no browser errors. Both cluster nodes and API/auth deployments are Ready.
- Visible regular-member **Container** submission trained a real CUDA network
  using a custom image's own CMD. The walkthrough found and fixed clearing the
  image field, with a regression test. Image was preloaded, not registry-pushed.
- Large upload and exhaustion tests were not repeated at the user's request;
  no 200/500 GiB stress, every-edge-case or arbitrary-model claim is made.

Current documentation covers user/admin operations, image submission, storage,
deployment, limits, design and verification. The historical `main` tip
`1050b022e36399f4b3b1dba1a3b3d670fcfca539` is preserved on the remote branch
[`archive/main-before-k3s-20261009`](https://github.com/UTMIST/Mist/tree/archive/main-before-k3s-20261009).
The department implementation is prepared for main in
[PR #110](https://github.com/UTMIST/Mist/pull/110), using a squash merge.
The initial merge attempt required an independent review. PR #110 was then
merged on October 9 at 08:06:48 UTC as commit
`3431ffdb713fac72881fc7f61f2750a82fe3f916`; main now contains the verified release.
The account used by this agent has maintain/push access, and the repository still
requires an independent reviewer for subsequent pull requests. The live
private portal already runs the verified implementation. Git publication does
not reset accounts, jobs or files, or switch the live services again.

See [the final release record](../deploy/k3s/evidence/department-2026-10-09/release-validation.json)
and [testing instructions](testing.md).
