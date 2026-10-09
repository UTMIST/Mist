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

- [x] Create a new foundation branch from the current working checkout.
- [x] Preserve existing pilot changes and inspect main for useful components.
- [x] Separate inventory and submission validation from HTTP/UI presentation.
- [x] Document new response/request shapes before connecting UI controls.
- [x] Keep changes reviewable and avoid unrelated rewrites.

### B. Task 2: hardware discovery and availability

Proposed endpoint: `GET /hardware`. Publish the final contract with the code.

- [x] Report node readiness, accelerator type, allocation unit, total,
  allocated, and currently available counts.
- [x] Count NVIDIA allocations from scheduled, nonterminal Kubernetes pods,
  including workloads outside Mist. Do not subtract every submitted job.
- [x] Count Tenstorrent devices from ResourceSlices and actual DRA claim
  allocations. Account for reservations while a workload starts.
- [x] Label TT counts as boards and show two chips per n300 board.
- [x] Treat machine/inventory failures explicitly; do not present unknown
  resources as confidently available.
- [x] Give the API only the additional read permissions required for inventory.
- [x] Keep k3s responsible for actual allocation. UI availability is a snapshot;
  a submitted job may wait even if devices looked free moments earlier.

TT's current vendor ResourceSlice memory capacity is incorrect. Do not use that
value to display accelerator memory or determine placement. The current system
has neither GPU fractions nor arbitrary TT chip sharing.

### C. Task 3: container image execution

- [x] Expose approved image choices and explicit image references, command,
  arguments, and CPU/memory/deadline controls through typed API/UI code.
- [x] Keep image approval on the server. An approved image can still fail to
  pull or be incompatible; report the actual error.
- [x] Preserve the image's working directory. The current executor overrides
  every workload with `/tmp`, which can break `python train.py` in an image
  whose code lives in `/app`.
- [x] Define command/argument behavior, including whether the image's default
  ENTRYPOINT/CMD may run. Kubernetes command/args are arrays; do not silently
  treat every string as a shell program.
- [x] Retain script and training-check modes with their tested runtime profiles.
- [x] Support approved custom CPU/NVIDIA images. Keep the tested TT image/profile
  restriction visible instead of promising arbitrary TT image compatibility.
- [x] Configure/document image pull credentials where needed; do not ask users
  to paste registry credentials into a job form.
- [x] Verify an actual custom image with packaged code, rather than testing
  only scripts injected into existing base images. A local fixture image may
  be imported into containerd for the initial test.
- [x] Document resource limits and deadlines. Current Job deadline includes
  waiting time as well as execution; make any change to this behavior explicit.

### D. Persistent output folders

- [x] Provide a predictable, writable per-job directory backed by the existing
  checkpoint PVC on the selected machine.
- [x] Create the directory reliably and verify permissions for the supported
  image profiles.
- [x] Preserve/document `MIST_CHECKPOINT_DIR` and `checkpoint_directory` so
  workloads know where to save their results.
- [x] Verify a real output file remains after the container exits.
- [x] Describe how to retrieve files during local testing and where they live.
- [x] Keep job identifiers/storage references independent of physical disk
  paths so shared storage can be added later.

Files written only into the container's writable layer are not persistent
results. The existing shared checkpoint roots are also not a complete tenant
isolation boundary; do not represent this pilot as ready for untrusted users.

### E. Task 1: finish website job controls

- [x] Connect the website to the new hardware and image contracts.
- [x] Keep real job submission, polling, logs, assigned node, failure reasons,
  exit codes, and cancellation.
- [x] Show requested resources and the persistent output location.
- [x] Make waiting-for-resources, machine unavailable, and image pull failures
  understandable.
- [x] Preserve connection recovery and protect against duplicate submissions.
- [x] Use existing UI conventions and accessible labels. Keep frontend concerns
  separate from the executor and typed API client.

The prototype Dashboard/Machines/auth pages are not proof of working metrics or
authentication. Surface real hardware information in the relevant workflow.

### F. Verify and hand off

- [x] Run focused backend tests for execution semantics, inventory accounting,
  validation, and cancellation where changed.
- [x] Run frontend type checking, production build, and meaningful interaction
  checks for new behavior.
- [x] Run a real custom CPU image job through the website.
- [x] Run fresh NVIDIA and TT training checks through the website.
- [x] Verify occupied/free counts during execution and after completion.
- [x] Verify waiting/reuse and cancellation release for both allocation paths.
- [x] Verify failed commands and image pulls produce useful states/errors.
- [x] Verify saved outputs persist after pod completion.
- [x] Update the deployed API image if necessary; editing Go source does not
  update the binary currently running in k3s.
- [x] Record actual results and date them. Older October 3 acceptance evidence
  does not substitute for tests of today's changes.
- [x] Document startup, API contracts, image preparation, output access, tests,
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
- Branch: `feat/local-job-foundation`. Commit `2f9b178` preserves the prior working
  pilot and this plan. Commit identity uses the connected GitHub account's public
  noreply address for this commit only; global Git settings were not changed.
- Backend additions: `hardware.go`, `images.go`, `outputs.go`, and `job_states.go`.
  Inventory reads node/pod/claim/slice state; image defaults are preserved;
  outputs have per-job mounts and an initialization step; history reads are batched.
- Frontend: typed inventory/catalog clients, a shared polling hook, separate
  submission/card/log components, and live Machines information.
- Focused backend tests pass. Frontend interaction tests (5), type checking, and
  production build pass. A test initially submitted before the catalog loaded;
  it was corrected to wait for the form's actual readiness. Final lint also
  passed after removing unnecessary test casts and shadowed callback names.
- Example images `mist-training:cpu-v1` and `mist-training:nvidia-v1` were built
  and imported on the NVIDIA node. API image `mist-api:foundation-20261008`
  is deployed; the final binary image ID is recorded in `foundation-results.json`.
- Fresh browser checks passed: packaged CPU training with image ENTRYPOINT/CMD
  and `/app` WORKDIR, both NVIDIA GPUs, TT two-board/four-chip training, exit 17,
  cancellation, real states/logs, and the live Machines page.
- Fresh allocation checks passed for NVIDIA and TT: whole-machine occupied/free
  counts, queued follower, cancellation with retained logs, automatic device
  reuse, and return to 2 GPUs / 4 boards available.
- Persistent output checks used new read-only containers after training exited.
  CPU model/results, both NVIDIA checkpoints, and four TT chip checkpoints and
  metrics were readable on the proper nodes. SHA256/file evidence was saved.
- A deliberately missing approved image exposed its real pull reason in the API
  and browser, then failed with DeadlineExceeded. Temporary image approval was
  removed, and prior job/cancellation history survived API restarts.
- Final focused backend tests: 12 passed. Frontend interaction tests: 5 passed;
  type checking, production build, and lint of changed frontend files passed.
  CLI tests passed. The full src suite passed in 33.325 seconds with isolated
  Redis and the existing Docker test image. Its initial 15-second legacy CPU
  test timed out during large-image disk activity; the suite passed after the
  imports finished. No legacy executor behavior was changed.
- The first browser verifier expected a completed TT claim to retain allocations.
  Kubernetes had correctly released them. The verifier now checks completed
  request/log evidence and checks actual DRA devices during live reservations.
- Fresh RBAC checks passed: required inventory lists allowed; Secrets, node
  writes, pod exec, and Jobs in other namespaces denied. Private registry
  Secret wiring is implemented/documented; no private registry login was tested.
- Detailed evidence: `/home/utmist/mist-foundation-results-2026-10-08`.
  Compact checked-in summary: `deploy/k3s/foundation-results.json`.
  Final idle snapshot: both nodes Ready, 0 allocated / 2 available NVIDIA GPUs,
  0 allocated / 4 available TT boards. All 40 retained Mist Jobs were terminal.
  Job history read measured 0.193 seconds, compared with the 3.586-second baseline.
- Today's agreed implementation and verification are complete: parts 1–3 plus
  persistent local outputs. The percentage updates referred to today's scope.
  The complete platform still needs part 4 (shared datasets/downloads) and
  part 5 (deployment/login). The team handoff is `docs/local-job-foundation.md`.
- Screen question checked: automatic suspend on AC has timeout 0 (disabled),
  so the display may turn off while work continues. Power settings were not changed.

## References

- [Kubernetes Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- [Container images](https://kubernetes.io/docs/concepts/containers/images/)
- [K3s private registries](https://docs.k3s.io/installation/private-registry)
- [Kubernetes persistent volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
- [Local path provisioner](https://github.com/rancher/local-path-provisioner)
- [Tenstorrent platform support](https://docs.tenstorrent.com/tt-operator/latest/platform-support.html)
