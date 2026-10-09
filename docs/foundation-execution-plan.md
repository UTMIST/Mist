# Mist foundation execution plan

Recorded: October 8, 2026. This file preserves the agreed scope and should be
updated with implementation progress, test evidence, and remaining work.

## Goal and decisions

Make the local website usable for running container jobs on the existing
NVIDIA and Tenstorrent machines. Start with the working Kubernetes pilot,
rather than rebuilding execution. The user has requested implementation on
a new branch, organized code, and proper documentation.

Today's scope is tasks 1–3 plus persistent output folders and verification.
Shared dataset storage, uploads, and result downloads are the next foundation
task. Website deployment and login follow the foundation.

Decisions already made:

- Develop and test locally. Team members can use the CLI over SSH or open the
  local website through an SSH tunnel. A public website is unnecessary now.
- Mist API creates Kubernetes Jobs; k3s schedules them; containerd runs the
  images. Normal job execution does not use SSH.
- Images are built and pushed to a registry, then selected by reference. A
  browser image archive uploader or image build service is outside today's scope.
- CPU and NVIDIA support approved custom images. Tenstorrent initially uses
  its existing tested image and host runtime profile.
- NVIDIA allocations are whole GPUs. Tenstorrent allocations are whole n300
  boards, with two accelerator chips per board.
- Start with persistent node-local results folders. Add shared storage later.
- For shared storage, the preferred initial host is Tenstorrent because it has
  more free disk space. NFS over the existing network is the simplest proposal.
- Wi-Fi is adequate for initial testing. A future second Ethernet cable can
  provide a dedicated storage link. Preserve Tenstorrent's existing router link:
  its k3s configuration and network access currently depend on it.
- Preserve existing work and Git history. Implement on a new branch. Retiring
  or merging the existing main branch is a later repository decision.
- No permission prompts are needed for the agreed implementation and tests.
  Do not introduce unrelated public exposure, account changes, or disk formatting.

## Architecture

```text
Local website / CLI
         |
         v
     Mist API
         |
         v
    k3s cluster
     /       \
    v         v
NVIDIA PC   Tenstorrent
containerd  containerd
    |         |
    v         v
Job container + allocated accelerator + persistent results folder
```

The full whiteboard plan also includes membership/authentication, credits,
priority policy, a storage box, Jupyter, job chaining, and optional agents.
Kubernetes supplies placement and device allocation. Mist supplies admission,
user policy, UI, dataset handling, and artifact access. Selecting an accelerator
does not convert CPU code into CUDA or Tenstorrent code, or distribute one model
across machines automatically.

## Current baseline

Repo: `/home/utmist/Mist`. Before this implementation, the branch is
`fix/local-runtime-setup` and contains substantial uncommitted k3s pilot work.
Preserve these changes when creating the new branch.

An independent main preview exists at `/home/utmist/Mist-main-preview`.
Use main's reusable UI/components/types where helpful. Its Jobs data and several
actions are prototypes, and its legacy Docker/Redis executor is incomplete.

| Machine | Kubernetes node | Tailnet IP | Cluster LAN IP | Resources |
| --- | --- | --- | --- | --- |
| Main/NVIDIA | `utmist-z1opa08` | `100.73.139.66` | `10.0.0.175` | 2 NVIDIA RTX A4000 GPUs |
| QuietBox | `utmist-tt` | `100.95.175.37` | `10.0.0.112` | 4 n300 boards / 8 Wormhole chips |

Both nodes report Ready with k3s `v1.36.4+k3s1`. Workload namespace is `mist`;
API namespace is `mist-system`.

- API already runs inside k3s. Local access is `http://127.0.0.1:3000`.
- Working Jobs page: `http://127.0.0.1:3001/jobs`.
- Vite proxies `/api` to the API; this proxy does not ship in the static build.
- User services: `mist-api-forward.service`, `mist-web.service`.
- Main preview is separate: UI port 3011, API port 3010.
- Existing Jobs page supports scripts, accelerator counts, training checks,
  real states, logs, and cancellation.
- API is a trusted local, single-owner pilot. Owner is configuration, not an
  authenticated identity. Authentication and tenant isolation remain work.
- Existing checkpoints are in three node-local `local-path` PVCs. Their declared
  request is 256 MiB each; this is not a meaningful dataset storage budget.
- Shared storage and dataset upload/download services have not been configured.

Storage readings taken during planning:

| Machine | Filesystem capacity | Used | Available |
| --- | --- | --- | --- |
| NVIDIA | 439 GiB | 87 GiB | 330 GiB |
| Tenstorrent NVMe | 3.6 TiB | 1.6 TiB | 1.9 TiB |

NVIDIA also has an unmounted ~447 GiB `/dev/sdb`. Its contents are unknown;
do not treat it as empty or format it as part of this plan.

NVIDIA currently reaches QuietBox over Wi-Fi via the router. QuietBox's active
router Ethernet link negotiates 1 Gbps. Both machines have unused 10 Gbps
Ethernet hardware; a future direct link requires compatible ports/cable and
verification. Being on the same tailnet or k3s cluster does not share disks.

## Implementation order

### A. Establish the branch and API contracts

- [ ] Create a new foundation branch from the current working checkout.
- [ ] Preserve existing pilot changes and inspect main for useful components.
- [ ] Separate inventory and submission validation from HTTP/UI presentation.
- [ ] Document new response/request shapes before connecting UI controls.
- [ ] Keep changes reviewable and avoid unrelated rewrites.

### B. Task 2: hardware discovery and availability

Proposed endpoint: `GET /hardware`. Publish the final contract with the code.

- [ ] Report node readiness, accelerator type, allocation unit, total,
  allocated, and currently available counts.
- [ ] Count NVIDIA allocations from scheduled, nonterminal Kubernetes pods,
  including workloads outside Mist. Do not subtract every submitted job.
- [ ] Count Tenstorrent devices from ResourceSlices and actual DRA claim
  allocations. Account for reservations while a workload starts.
- [ ] Label TT counts as boards and show two chips per n300 board.
- [ ] Treat machine/inventory failures explicitly; do not present unknown
  resources as confidently available.
- [ ] Give the API only the additional read permissions required for inventory.
- [ ] Keep k3s responsible for actual allocation. UI availability is a snapshot;
  a submitted job may wait even if devices looked free moments earlier.

TT's current vendor ResourceSlice memory capacity is incorrect. Do not use that
value to display accelerator memory or determine placement. The current system
has neither GPU fractions nor arbitrary TT chip sharing.

### C. Task 3: container image execution

- [ ] Expose approved image choices and explicit image references, command,
  arguments, and CPU/memory/deadline controls through typed API/UI code.
- [ ] Keep image approval on the server. An approved image can still fail to
  pull or be incompatible; report the actual error.
- [ ] Preserve the image's working directory. The current executor overrides
  every workload with `/tmp`, which can break `python train.py` in an image
  whose code lives in `/app`.
- [ ] Define command/argument behavior, including whether the image's default
  ENTRYPOINT/CMD may run. Kubernetes command/args are arrays; do not silently
  treat every string as a shell program.
- [ ] Retain script and training-check modes with their tested runtime profiles.
- [ ] Support approved custom CPU/NVIDIA images. Keep the tested TT image/profile
  restriction visible instead of promising arbitrary TT image compatibility.
- [ ] Configure/document image pull credentials where needed; do not ask users
  to paste registry credentials into a job form.
- [ ] Verify an actual custom image with packaged code, rather than testing
  only scripts injected into existing base images. A local fixture image may
  be imported into containerd for the initial test.
- [ ] Document resource limits and deadlines. Current Job deadline includes
  waiting time as well as execution; make any change to this behavior explicit.

### D. Persistent output folders

- [ ] Provide a predictable, writable per-job directory backed by the existing
  checkpoint PVC on the selected machine.
- [ ] Create the directory reliably and verify permissions for the supported
  image profiles.
- [ ] Preserve/document `MIST_CHECKPOINT_DIR` and `checkpoint_directory` so
  workloads know where to save their results.
- [ ] Verify a real output file remains after the container exits.
- [ ] Describe how to retrieve files during local testing and where they live.
- [ ] Keep job identifiers/storage references independent of physical disk
  paths so shared storage can be added later.

Files written only into the container's writable layer are not persistent
results. The existing shared checkpoint roots are also not a complete tenant
isolation boundary; do not represent this pilot as ready for untrusted users.

### E. Task 1: finish website job controls

- [ ] Connect the website to the new hardware and image contracts.
- [ ] Keep real job submission, polling, logs, assigned node, failure reasons,
  exit codes, and cancellation.
- [ ] Show requested resources and the persistent output location.
- [ ] Make waiting-for-resources, machine unavailable, and image pull failures
  understandable.
- [ ] Preserve connection recovery and protect against duplicate submissions.
- [ ] Use existing UI conventions and accessible labels. Keep frontend concerns
  separate from the executor and typed API client.

The prototype Dashboard/Machines/auth pages are not proof of working metrics or
authentication. Surface real hardware information in the relevant workflow.

### F. Verify and hand off

- [ ] Run focused backend tests for execution semantics, inventory accounting,
  validation, and cancellation where changed.
- [ ] Run frontend type checking, production build, and meaningful interaction
  checks for new behavior.
- [ ] Run a real custom CPU image job through the website.
- [ ] Run fresh NVIDIA and TT training checks through the website.
- [ ] Verify occupied/free counts during execution and after completion.
- [ ] Verify waiting/reuse and cancellation release for both allocation paths.
- [ ] Verify failed commands and image pulls produce useful states/errors.
- [ ] Verify saved outputs persist after pod completion.
- [ ] Update the deployed API image if necessary; editing Go source does not
  update the binary currently running in k3s.
- [ ] Record actual results and date them. Older October 3 acceptance evidence
  does not substitute for tests of today's changes.
- [ ] Document startup, API contracts, image preparation, output access, tests,
  and remaining work for the team.

Avoid unrelated operator upgrades or repeat full infrastructure acceptance tests
without a concrete regression risk. Small synthetic training checks demonstrate
device execution and learning, but each team's actual model needs compatibility
validation; distributed training and TT autograd are not established by these checks.

## Today's definition of done

```text
Local website
      |
Choose approved image + command + hardware + device count
      |
Mist API creates a k3s Job
      |
Selected machine runs the container
      |
Real states/logs + correct device accounting + persistent output file
```

Implementation should be verifiable from the browser, with failures and
cancellation behaving correctly and documentation sufficient for another
developer to resume the work.

## Next foundation task: shared datasets and downloads

This is task 4, after today's execution scope:

1. Create a storage folder on QuietBox with a bounded disk budget.
2. Configure private NFS access for both machines and Kubernetes workloads.
3. Add streaming dataset uploads, progress/error handling, a stable dataset ID,
   and a ready state before jobs may use an upload.
4. Attach only the selected dataset read-only and the job's outputs writable.
5. Add result listing/downloads with ownership checks and retention rules.
6. Test one uploaded dataset with jobs on both machines.

Proposed physical layout, not yet created:

```text
Tenstorrent: /srv/mist-storage/
├── datasets/
│   └── dataset-id/
└── jobs/
    └── job-id/
        └── outputs/
```

Container paths may become `/inputs` and `/outputs`; preserve compatibility
with the existing checkpoint contract or document the migration explicitly.
NVIDIA reads TT-hosted files over the current LAN, which includes Wi-Fi.
Caching large datasets on NVIDIA and a dedicated Ethernet link can follow.
Central storage on one machine needs backup/retention planning and is unavailable
when that host fails. Archives need safe extraction and upload limits.

## Later: deployment and product features

Task 5 follows the foundation:

- Serve a production frontend and route `/api` to the existing Mist API.
- Provide a team-accessible Tailnet address/hostname and persistent services.
- Implement login, job/file ownership, and appropriate member limits before
  supporting multiple users.
- Add public domain/HTTPS only if public access is requested.

Credits, fairness/priority policies, Jupyter, chaining, agents, detailed monitoring,
portable TT runtime images, and distributed model training are separate workstreams.

## Resume notes and useful files

### Code

- `src/kubernetes.go`: executor, validation, resource requests, job status/logs/cancel.
- `src/kubernetes_test.go`: existing Kubernetes executor tests.
- `src/api.go`: API mode selection and request types.
- `web-interface/src/api.ts`: typed API client.
- `web-interface/src/components/JobsPage.tsx`: working job UI.
- `web-interface/src/components/JobsPage.test.tsx`: existing UI checks.
- `deploy/k3s/mist-api.yaml`: API Deployment, RBAC, Service, and checkpoint PVCs.
- `deploy/k3s/mist-tenstorrent-claims.yaml`: 1–4 board claim templates.
- `deploy/k3s/tenstorrent-workload-policy.yaml`: accelerator workload admission policy.
- `deploy/k3s/training_smoke.py`: actual NVIDIA/TT regression training check.
- `deploy/k3s/verify_mist_api.py`: earlier API allocation/queue/training acceptance tool.

### Existing documentation and evidence

- `docs/kubernetes-pilot.md`, `deploy/k3s/README.md`, `src/jobs.md`.
- `deploy/k3s/mist-api-results.json`: October 3 acceptance summary.
- `/home/utmist/mist-api-results-2026-10-03`: detailed prior acceptance artifacts.

### Local tools and access

- Go: `/home/utmist/.local/share/mist-runtimes/go-1.25.1/bin/go`.
- Node: `/home/utmist/.local/share/mist-runtimes/node-22.16.0/bin/node`.
  Prepend that runtime's directory to PATH when running npm/npx.
- Kubeconfig: `/home/utmist/.kube/config`.
- QuietBox SSH: user `utmist-tt`, host `100.95.175.37`, existing identity file
  `/home/utmist/.ssh/id_ed25519_quietbox_codex`; use IdentitiesOnly and BatchMode.
- Browser checks can use the temporary Playwright installation at
  `/tmp/mist-browser-check/node_modules/playwright` and existing cached Chromium.
- User is in the Docker group. Sudo requires a password; prior authorized root
  cluster operations used a Docker host bind/chroot. Do not print secret values.
- Do not include credentials, join tokens, registry secrets, or private keys in
  documentation, Git, or tool output.

Model recommendation for the user's remaining quota: GPT-6.1 Sol, Medium
reasoning, Standard speed. This was advice, not a settings change. Saved settings
were Max/Fast at the time of planning. Exact quota consumption cannot be predicted.

## Progress log

- Planning: agreed scope recorded; existing API health and both Ready nodes checked.
- Implementation: pending at initial creation of this file.
- New branch, code changes, verification evidence, and final handoff: update here
  as execution proceeds.

## References

- [Kubernetes Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- [Container images](https://kubernetes.io/docs/concepts/containers/images/)
- [K3s private registries](https://docs.k3s.io/installation/private-registry)
- [Kubernetes persistent volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
- [Local path provisioner](https://github.com/rancher/local-path-provisioner)
- [Tenstorrent platform support](https://docs.tenstorrent.com/tt-operator/latest/platform-support.html)
