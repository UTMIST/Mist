# Testing

Use the pinned Go/Node runtimes in [developer setup](development.md). The root
workspace joins the API and CLI. From the repository root:

```bash
go test ./src/... ./cli/... -count=1
go test -race ./src/... ./cli/... -count=1
go vet ./src/... ./cli/...
npm --prefix web-interface ci
npm --prefix web-interface test
npx --prefix web-interface tsc --noEmit -p web-interface/tsconfig.json
npm --prefix web-interface run lint
npm --prefix web-interface run build
python3 scripts/check-docs.py
```

## Automated scope

Go tests use fake Kubernetes clients and small temporary filesystem fixtures for
job state/cancellation/validation, hardware allocation, authentication/isolation,
team grants/quotas/admission/restarts, scoped storage and uploads/ZIP checks.
Application tests cover unavailable-cluster startup and the scoped compatibility
status endpoint. CLI tests exercise real request/response behavior through test
servers. React tests cover submission, image defaults/editing, pagination,
logs/cancellation and accessible shared controls. Migration tests use tiny files. They require root to verify ownership; a normal
user run reports skips. Run them as root in an isolated environment, for example:

```bash
sudo python3 -m unittest discover -s deploy/k3s/storage -p test_migration.py -v
```

The tests replace the store root with a temporary directory and do not access
live storage. Docker can provide an isolated root environment when sudo is
unavailable.

Current automated suites do not require Redis or Docker. The old supervisor,
Docker manager/logger tests and fake CLI-config tests were removed with their
retired implementations. This is not a test skip: those products are no longer
part of the application. The earlier full legacy suite results remain as dated
[pre-cleanup evidence](../deploy/k3s/evidence/department-2026-10-09/release-validation.json).

## Real installation checks

| Verifier | Purpose |
|---|---|
| `deploy/k3s/verify_department.mjs` | API, auth, teams, grants, queue/revocation, storage and training |
| `deploy/k3s/verify_department_browser.mjs` | Real admin/member UI, custom image flow, pagination and responsive layout |
| `deploy/k3s/verify_department_runtime.py` | Concurrent TT allocations, failure and execution deadline |
| `deploy/k3s/verify_department_remote_cli.py` | SSH to TT, submit through CLI, execute on NVIDIA |
| `deploy/k3s/verify_department_admission.py` | Admission denials for unsafe workload specifications |
| `deploy/k3s/verify_split_storage.py` | Separate dataset/model pools, migration preservation and small-file training |
| `deploy/k3s/verify_large_dataset.py` | Earlier large-upload fixture; not part of ordinary checks |
| `deploy/private/verify-portal-firewall.sh` | Installed host chains with a synthetic untrusted source |

These require existing protected credentials, node addresses and operators.
Inspect the script before rerunning: API restart, revocation/deactivation, quota
exhaustion and firewall tests belong in an idle maintenance window. Verification
fixtures preserve history/files; new browser teams reserve 1 GiB per pool even
after being disabled. Do not repeatedly create fixtures without accounting for
reserved storage.

The retained `verify_tenstorrent_allocation.py` is an administrator infrastructure
diagnostic; it bypasses the portal's team admission. Pre-team browser/API
verifiers have been retired because they no longer reflect current auth/storage.

## Evidence and limits

The [October 9 evidence](../deploy/k3s/evidence/department-2026-10-09/README.md)
records real CUDA/TT training, separate board/GPU reservations, input/output
mounts, checkpoint reload/download, grants/revocation, queue restart and browser
flows. The regular-member Container demonstration preserved its own image CMD;
that image was locally preloaded, not registry-pushed.

The [cleanup record](repository-cleanup.md) separates new source checks from
previous live-release evidence. A running service is not silently redeployed by
checking out a branch or editing documentation.

Large uploads and exhaustion are not repeated at the user's request. The
separate-pool follow-up used a 260,729-byte dataset. No 200/500 GiB transfer stress,
every possible edge case, arbitrary research model, HA or disk-failure recovery
is claimed. Excluded features remain outside the release.
