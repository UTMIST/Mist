# Testing

## Current Kubernetes department path

Use the Go and Node runtimes recorded in `deploy/private/build-and-deploy.sh`.

```bash
go -C src test ./... -run 'Test(Department|Kubernetes|Shared|Dataset|Auth|Member|Image|Hardware|Result)' -count=1
go -C cli test ./...
npm --prefix web-interface test
npx --prefix web-interface tsc --noEmit -p web-interface/tsconfig.json
```

The Go suite uses fake Kubernetes clients for permissions, scoped storage, queued
admission/restarts and pending TT allocation accounting. React tests cover job
submission/logs/cancellation/pagination and search/keyboard/focus behavior.

The real pilot checks are in `deploy/k3s/verify_department.mjs`,
`verify_department_browser.mjs`, `verify_department_runtime.py`,
`verify_department_remote_cli.py`, `verify_department_admission.py`, and
`verify_large_dataset.py`. They use protected local credentials and synthetic
fixtures. API restart, deactivation, quota exhaustion and firewall checks should
run in an idle maintenance window. They preserve historical user data; the large
file verifier removes only its own uploaded probe after its Job exits.

The browser verifier reuses its fixtures by default. Optional
`MIST_VERIFY_NEW_MEMBER=1 MIST_VERIFY_NEW_TEAM=1` exercises fresh creation dialogs,
deactivating/renaming its old fixtures and retaining their files/history. Each new
team fixture consumes a 1 GiB allocation; disabling does not release that capacity.

[Recorded evidence](../deploy/k3s/evidence/department-2026-10-09/README.md) covers
actual accelerator training, isolation, hard capacity, grants/revocation, CLI
submission from TT to NVIDIA, the larger dataset and browser workflows.

## Legacy Redis/Docker integration fixtures

The full historical Go integration suite needs the original Docker/Redis
fixtures. It was not represented as passing with only the Kubernetes pilot up.
The current Kubernetes execution path does not use Redis.

### Setup

For testing, docker engine and docker-compose should be installed - see [here](https://docs.docker.com/engine/install/).
Additionally, golang should be [installed](https://go.dev/doc/install).

### Running the historical tests

Start containers by running `docker-compose up` in the main directory (docker must be installed).

Run tests on the main application by running `go test` in the `src` directory. Note that `TestIntegration` will fail without the containers in [`docker-compose.yml`](../docker-compose.yml) up and running.
