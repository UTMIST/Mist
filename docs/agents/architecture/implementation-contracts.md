# Implementation contracts and release gates

This is the entry point for agents implementing the new Mist system. Read
[ARCHITECTURE.md](../../../ARCHITECTURE.md), the relevant component contracts,
[D18–D23](decisions.md#d18-mist-admits-jobs-kubernetes-may-allocate-dra-claims), and
[the jobs implementation plan](../plans/jobs-and-scheduling.md) before changing
code. These documents describe a target, not evidence that it is implemented.
Do not edit `docs/humans`.

## Authority and scope

- Component documents define behavior. This file defines integration/release
  checks. The jobs plan orders implementation. Later decision entries supersede
  only the older clauses they name; historical decisions remain for context.
- Keep authorization, defaults, scheduling and derived UI fields in the backend.
  The portal polls ready-to-display state. Share API wire types and fixtures.
- M1 is GPU/CPU batch and command execution. M2 adds persistent terminals; M3
  fairness/accounting UI; M4 TT; M5 native execution/web terminal. Do not substitute
  a later feature for an unresolved M1 isolation or recovery requirement.
- Development fixtures may unblock isolated work, but must be replaced at the
  release gate. Never deploy a principal header, fake sign-in provider or fake
  cleanup acknowledgement as production authorization/evidence. M1 may use the
  admin-maintained misty directory YAML pending the real API; it must have actual
  authorized members, auditable updates and the same generation/revocation path.
  This directory stand-in does not replace real proof of email ownership.
- If an implementation cannot meet a contract, update the component contract,
  add a superseding decision where needed, and adjust acceptance checks together.
  Do not silently weaken a guarantee to make a test pass.

## Invariants

| Invariant | Owning contract | Required behavior |
|---|---|---|
| Current access | [Identity](identity-and-accounts.md#revocation-and-provisioning-barrier-m1) | Atomic generation changes; fresh authorization at admission; local lease expiry and session revocation |
| Team filesystem boundary | [Storage](storage.md#access-rules) | Personal defaults read-only to teammates; shared ACLs writable; other teams absent from jobs |
| Restricted gateway | [Gateway](login-gateway.md#sshd-configuration) | Permanent restriction group, no startup-hook execution, confined transfers, root-tracked sessions |
| One admitted execution | [Scheduler](scheduler.md#reconciliation) | Immutable ExecutionID/spec, durable intent, no automatic rerun after start |
| No premature reuse | [Executor](executors.md#cleanup-and-reconciliation) | Finish and cleanup distinct; unresolved/lost occupancy remains reserved |
| Isolated job network | [Deployment](deployment-and-network.md#job-network-boundary-m1) | Own-node and remote internal services blocked; DNS/internet allowed |
| Durable terminal | [Executor](executors.md#mist-job-entry) | One shell/PTY; transport detach does not destroy or replace it |
| Minimal client logic | [API](api.md) | Backend normalizes requests, authorizes actions and redacts public summaries |

## M1 acceptance matrix

Use three fixture accounts: Alice and Bob in team A, Carol in team B; additionally
make Alice a member of B for multi-team tests. Use real UIDs, real mounts and real
SSH clients for host tests. Record installed versions and commands/results in
`docs/agents/` with the implementation PR; never include credentials or team data.

| ID | Scenario | Required evidence | Owners |
|---|---|---|---|
| M1-01 | Personal and shared files created by jobs, SFTP, modern/legacy scp and rsync | Bob reads Alice's ordinary personal file but cannot overwrite/delete it; shared files are writable; private dot-directories deny Bob; Carol cannot access A. Exercise default and explicit permission-preserving transfer modes. | Storage, Gateway |
| M1-02 | Membership removal with live SFTP/rsync, multiplexed SSH, queued/launching/running jobs | API access denied; old sessions killed; all affected nonterminal jobs cancelled/stopped; new login sees updated groups only after provision acknowledgement. Other-team jobs remain authorized. | Identity, Gateway, Scheduler |
| M1-03 | Disable and key removal during API outage; watchdog restart | Local deny beats cached keys; restriction group remains; stale lease cannot renew; existing sessions expire within remaining lifetime; admin access still works. | Identity, Gateway |
| M1-04 | Crash before/after admission commit, Pod create response loss, scheduler leadership loss | Recover the same execution; never duplicate work; no allocation leak/release from ambiguous NotFound; revoked/cancelled intent never intentionally replayed. Delayed external calls are reconciled before node reuse. | Scheduler, Executor |
| M1-05 | Cancel in PENDING/LAUNCHING/RUNNING/STOPPING/CLEANING/terminal; exit before Started event | Stable outcome and event history under duplicate/out-of-order delivery; stop cause precedence follows scheduler contract; no resurrection. | Scheduler |
| M1-06 | Finish/cleanup failure, node partition, API deletion before node confirmation | CLEANING retains allocation; NODE_FAIL retains quarantine; cleanup resumes after restart; no reuse until matching node receipt. Consecutive scratch-heavy jobs do not over-admit. | Executor, Storage |
| M1-07 | CPU and GPU jobs probe network on both hosts | Own-node and peer NFS/SSH/API/kubelet/Postgres, cluster service aliases and LAN/Tailscale blocked; DNS and public HTTPS work; IPv6 cannot bypass; host mounts/admin SSH still work. | Deployment, Executor |
| M1-08 | Resource pressure and scheduler outage | One GPU visible per job; CPU/memory enforced; OOM and scratch eviction reasons correct; local/backstop deadline kills with controller unavailable; unmanaged workloads cannot consume admitted capacity. Test kernel/runtime observations, not only wrapper output. | Executor, Deployment |
| M1-09 | API permissions, retry and hostile paths | Duplicate submission key gives one job; conflicting payload rejected; non-team detail/log/attach denied; queue free of embedded identities; symlink escapes and unsafe env/argv rejected or contained. | API, Identity |
| M1-10 | Gateway command and startup bypass attempts | No arbitrary shell via rsync/scp flags, shell metacharacters, environment, rc files, proc paths or sockets; active/disabled/admin `sshd -T -C` output matches policy. | Gateway |
| M1-11 | Two concurrent real GPU users plus CPU download | Outputs and saved environment work; UI matches API; cancellation works; real identity/provisioning integrated; all prior gates pass. | All |

Fake-clock tests establish state-machine behavior; k3d exercises ordinary CPU
integration. Neither proves real device, NFS, SSH, firewall or cleanup behavior.
Golden Pod tests check construction, not isolation. An unavailable hardware test
is recorded as unverified and blocks the corresponding release gate.

## Later milestone gates

- **M2:** detach and reattach to the same shell PID, cwd, variables and running
  foreground process; resize and signals work; second attachment conflicts;
  output buffering is bounded; API/relay restart does not reset grace; holder
  failure does not create a fresh shell; nonzero shell exit fails; expiry and
  cancellation kill every descendant and release only after cleanup.
- **M3:** simulations include continuous 1-GPU arrivals against a 2-GPU
  reservation, CPU/TT shared-node contention, startup/grace/cleanup delays,
  unhealthy nodes and limit changes. Prove no lower-priority backfill repeatedly
  postpones a reservation. Report strict-QoS starvation separately. Usage records
  remain nonoverlapping after crashes and duplicate completion delivery. Test
  encrypted backup restoration, including database identity/allocation state.
- **M4:** fresh DRA claims allocate on exactly Mist's node without `nodeName`;
  stalled claims time out and clean up; hugepages are admitted in the ledger;
  board IDs map to reset scope; concurrent board jobs survive a neighbor's reset
  or concurrency is disabled; failed reset quarantines affected capacity.
- **M5:** run the full suite unchanged on native execution, plus inspect
  `/proc/self/status` for exact groups/capabilities of the multi-team user;
  no host processes/sockets/other-team paths visible; persistent shell and all
  descendants stay in the same resource and kill boundary across agent restart.
  Pin/test available systemd/kernel features before migrating a node.

## Decisions required before production

These are explicit implementation/deployment gates, not permission to invent an
insecure fallback. Owners record the selected values in configuration and link
validation evidence from their implementation PR.

| Gate | Owner | Must resolve |
|---|---|---|
| M1 sign-in | Identity | Real email-ownership proof and provider configuration; fixture auth excluded |
| M1 gateway integration | Gateway, Identity | Named owner; SSH/PAM session hook, transport/cgroup tracking, confined transfer implementation and fail-closed watchdog |
| M1 cleanup evidence | Executor, Deployment | Node reporter transport, safe object/path lookup, durable receipts and behavior while node/kubelet unavailable |
| M1 network/runtime | Deployment | Version pins, strict supplemental-group support, swap off, tested firewall ordering, TLS names/certificates |
| M1 storage | Storage | Actual scratch filesystem/capacity, host headroom, durable-storage layout and acknowledged advisory-quota availability limit |
| M1 runtime | Runtime | Named owner, actual image digests/registry and package versions; no literal `12.x` image tag |
| M3 backups | Storage, Identity | Tool, capacity, retention and coordinated DB/files restore; restore must preserve UID/GID mapping |
| M4 devices | Executor | Driver version, board-versus-chip resource model, hugepage requirements and reset isolation |

Operational defaults (timeouts, caps, lease lengths) are centralized server
configuration with validated bounds. Changes cannot retroactively mutate admitted
LaunchSpecs. Security-sensitive defaults must stay covered by conformance.
