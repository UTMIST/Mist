# Complete foundation: parts 4 and 5

Authorized October 8, 2026 after completion of parts 1–3. Continue on
`feat/local-job-foundation` from `8f1b7f1`. The user requests finishing the
remaining foundation instead of handing it to other developers. No permission
prompts are needed for this work. Preserve existing jobs, checkpoints, network
links and unrelated disk contents.

## Remaining scope

### Part 4: shared datasets and result downloads

- [x] Create `/srv/mist-storage` on QuietBox's existing NVMe filesystem.
- [x] Provide NFSv4 shared storage to the two current Kubernetes nodes, over
  their existing LAN. Keep QuietBox's router Ethernet cable connected.
- [x] Install the NFS host client/server packages and persistent configuration.
- [x] Add shared persistent volumes and mount storage in the API.
- [x] Upload datasets through the API and website with size limits/progress,
  stable dataset IDs, checksums, ready/error handling, and atomic publication.
- [x] Select a dataset for a job; attach only that dataset read-only at `/inputs`.
- [x] Store new job outputs on shared storage; preserve the `/outputs` and
  checkpoint aliases. Keep old local checkpoints retrievable.
- [x] List and download saved job files with ownership checks, streaming, and
  protection against traversal and escaping symlinks. Do not execute archives.
- [x] Verify an uploaded dataset is consumed by jobs on both physical machines,
  with actual accelerator computation and persistent downloadable results.

Use a bounded pilot budget (initially 100GiB for new datasets/outputs), an
upload limit (initially 2GiB per dataset), and explicit cleanup/retention docs.
NFS is central storage, not a backup. Preserve the existing checkpoint PVCs.
Metadata must not be writable by workload containers through output mounts.

### Part 5: login and private deployment

- [x] Use maintained Better Auth for separate Mist member accounts. Keep the
  existing Go executor and add a small auth service with persistent SQLite on
  a node-local volume, not SQLite on NFS.
- [x] Seed the first administrator from protected local credentials, outside
  Git/tool output. Disable public signup; administrators add members.
- [x] Authenticate API access, derive job/dataset ownership from the session,
  and verify isolation with two actual member sessions.
- [x] Add login/logout, current account, password change and member management
  UI. Remove prototype profile/account behavior from the active workflow.
- [x] Serve the production frontend with same-origin API/auth proxying and
  persistent services; avoid relying on Vite for the deployed website.
- [x] Make the site accessible through the existing private Tailscale network.
  Use HTTPS through Tailscale Serve if already available; otherwise keep a
  private Tailnet endpoint and record the HTTPS prerequisite accurately.
- [x] Add startup, restoration, membership, image approval, storage quota,
  backup and cleanup documentation and repeatable deployment scripts.
- [x] Verify browser login → upload → select dataset/image/device → run → logs
  → download, from both machines, plus denial of anonymous/other-owner access.
- [x] Commit tested changes and record exact deployed image/evidence paths.

## Decisions

- Keep image-reference submission and current NVIDIA/TT allocation units.
  Arbitrary TT models, distributed training, credits, Jupyter, chaining and
  image build/archive upload services are outside the five-part foundation.
- Deploy privately; no public domain, Funnel, paid service or external account
  provisioning is implied. Use existing host and cluster access.
- A shared tailnet infrastructure account does not identify individual team
  members; Mist accounts provide independent ownership.
- Existing owner `utmist` and retained jobs remain visible to the initial
  administrator through an explicit migration mapping.
- HTTP cookies alone are not authorization. Go verifies sessions against the
  auth service, and state-changing routes require trusted browser origins.
- Test first, then replace the deployed API/frontend with the verified build.

## Resume information

- Go/Node runtimes: `~/.local/share/mist-runtimes/go-1.25.1/bin/go` and
  `~/.local/share/mist-runtimes/node-22.16.0/bin/node`.
- Kubeconfig: `/home/utmist/.kube/config`; k3s nodes `utmist-z1opa08`, `utmist-tt`.
- QuietBox SSH: `utmist-tt@100.95.175.37`, identity
  `/home/utmist/.ssh/id_ed25519_quietbox_codex`, IdentitiesOnly/BatchMode.
- Authorized host operations can use Docker bind/chroot. Package installation
  needs host networking because host systemd-resolved listens on loopback.
- Current API deployed in `mist-system`; workloads in `mist`.
- Main tailnet hostname: `utmist.tail459b5e.ts.net`; IP `100.73.139.66`.
- Baseline evidence: `deploy/k3s/foundation-results.json`,
  `/home/utmist/mist-foundation-results-2026-10-08`.

## Progress

- Plan recorded; repository clean at start. QuietBox SSH and authorized Docker
  host access work. QuietBox still has approximately 1.9TiB available.
- Tailscale is connected; no Serve configuration exists. Existing Nginx is
  active. Preserve its current default service while adding the Mist endpoint.

## Completed verification (October 9 UTC)

- Actual production browser flow passed: login, dataset upload, CPU and both NVIDIA
  GPUs, one TT board/two chips, logs, saved file download, account/member actions,
  ownership isolation and deactivation. No browser runtime errors.
- QuietBox submitted an authenticated NVIDIA training job, which ran on the main
  node and returned downloadable loss metrics.
- Focused Go tests (including race checks), CLI tests/login/logout, frontend
  interaction tests, TypeScript and production build passed.
- Old checkpoint PVCs preserved; legacy outputs copied to shared storage.
- NFS/client firewall configuration and production restart/backup checks are
  documented in `docs/complete-foundation.md` and `deploy/private/README.md`.
- Private HTTP website is live. Tailscale Serve is disabled; HTTPS requires the
  tailnet administrator to enable Serve, accurately recorded as a prerequisite.

Final review: frontend dependencies now use the checked-in npm lockfile; the
obsolete Bun lockfile was removed. CLI login/logout use real sessions, and
`job submit --dataset` attaches an owned dataset. SQLite backup integrity was
`ok`; restart checks preserved historical/new jobs, datasets and downloadable
results. The production website does not depend on a terminal session.

## Department rollout scope decision — October 9, 2026

The user plans to introduce Mist to several research teams next week and has
explicitly excluded these additions from that rollout:

- Automatic backups and additional recovery testing.
- HTTPS/Tailscale Serve enablement.
- Monitoring and storage cleanup features.

The remaining proposed priorities are team membership/permissions and workload
isolation, team resource quotas, fair queuing, representative research workload
and capacity tests, and a documented container-image submission/approval process.
These are proposed follow-up work, not completed foundation features.

The current access model remains private HTTP over the existing Tailscale
network. Existing manual operating procedures are documented in the foundation
guide. Credits, special job priorities, Jupyter, chaining, and browser image
upload/build services remain outside the immediate rollout scope.
