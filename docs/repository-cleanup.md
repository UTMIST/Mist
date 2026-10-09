# Repository cleanup — October 9, 2026

Based on main commit `3431ffd`, the department release merged through PR #110.
Changes are prepared on `chore/main-cleanup-20261009`. The main ruleset still
requires a pull request and an independent review; Git publication is separate
from deploying services.

## Removed and organized

- Retired the unfinished Redis scheduler/supervisor, status registry, fake auth
  handlers, Docker manager and prototype CPU/CUDA/ROCm images/scripts. Removed
  their tests, Redis Compose/config and unused custom logger/config. Docker
  remains image build/import tooling for the current platform.
- Removed the empty tracked `go` file and unused root Go module. `go.work` now
  joins the active API and CLI modules. `go mod tidy` pruned retired dependencies.
- Split API lifecycle (`main.go`, `app.go`), HTTP job handlers (`jobs_http.go`),
  shared errors/responses (`http.go`) and job types (`job_types.go`) from the
  mixed prototype. Current wire fields, authentication/team enforcement and
  Kubernetes state/history handling remain compatible.
- Removed unreachable `teamHTTPError` after compiler-based dead-code analysis.
  A final analysis with tests reports no remaining unreachable Go functions.
- Removed the CLI dummy configuration command/tests, unused input/job helpers,
  editor workspace artifact, malformed command tag and empty command fallback.
  Persistent config-path handling is in production code; output helpers are
  compiled only for tests.
- Removed the unused frontend fake-user/logout helper, obsolete chart and
  `use-immer`/`immer` packages. Existing interactive components remain in use.
- Removed five pre-team API/browser verifiers that no longer reflect deployed
  authentication/storage. Current department/storage/accelerator verifiers and
  dated evidence remain.
- Archived seven superseded guides and preserved old document entry points.
  Added a current documentation index, k3s/Tailscale/SSH/storage/development
  handoff and a source/architecture map. Corrected submission HTTP status,
  current TT runtime/image rules and main's merged release state.
- Removed the plaintext password from the old SSH guide. Git history retains it;
  retire that credential if valid. This cleanup did not rotate account passwords.

## Verification

- [Sanitized validation report](../deploy/k3s/evidence/department-2026-10-09/cleanup-validation.json).
- Full current Go workspace tests with race detection: **29 API tests + 6 CLI
  tests**, no skipped tests; CLI entrypoint has no test files. Vet passes.
- API and CLI static builds, CLI/root/job help and retired-executor rejection pass.
- **10 React tests**, TypeScript, full frontend lint and production build pass.
  Removing the unused dependency reported **zero npm vulnerabilities**.
- **6 migration tests** pass as root in a disposable container with read-only
  test source and tiny temporary files. An ordinary non-root invocation skips
  these ownership tests; it is not represented as successful coverage.
- Both live cluster nodes, operator deployments/daemonsets and API/auth services
  were inspected. Host service enablement, regular OpenSSH/Tailscale SSH settings,
  LAN interfaces, token-file permissions, GPU/KMD inventory and storage usage
  were checked on the owning hosts. No rejoin/reboot/operator upgrade was run.
- The compiled candidate API is checked on a temporary authenticated localhost
  listener against real Kubernetes/auth: startup/health, anonymous session,
  anonymous-job denial and graceful SIGTERM. Team admission is disabled for
  this read-only probe, so it cannot run a second department controller.
- Local documentation links/anchors and current shell examples are checked by
  `python3 scripts/check-docs.py`, without executing the examples.

The existing portal continues to run the verified department release. These
source changes do not redeploy it. Previous live CUDA/TT/browser/isolation
[release evidence](../deploy/k3s/evidence/department-2026-10-09/README.md) remains
valid for that release; it is not claimed as a fresh training/browser run of the
cleanup candidate. No large storage tests, production restarts, account/job/file
mutations or full-disk cleanup were performed.

Full-host storage and account data live outside the repository. Removing old
source/fixtures does not remove existing Docker containers/volumes or Kubernetes
Jobs/PVCs. Historical account jobs/files remain accessible through the current
legacy workspace. No blanket prune, formatting or history rewrite is used.

## Reproduce current source checks

See [testing](testing.md), [developer setup](development.md) and
[documentation index](README.md). Raw local validation output is kept outside Git
under `/home/utmist/mist-cleanup-validation-20261009`; publish only sanitized
summaries. Credentials, session state, kubeconfig and SSH private keys stay out
of committed documentation/evidence.
