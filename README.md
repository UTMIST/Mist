# Mist

UTMIST's compute platform. The Kubernetes pilot runs submitted CPU, NVIDIA,
and Tenstorrent workloads as real Jobs. The Jobs page and CLI use the same
API for submission, status, logs, and cancellation. The local foundation adds
live hardware inventory, self-service container images, team workspaces,
scoped shared storage and a durable fair job queue.

## Current pilot

- `utmist-z1opa08`: k3s server, CPU workloads, two NVIDIA RTX A4000 GPUs.
- `utmist-tt`: QuietBox worker, four n300 boards with two Wormhole chips each.
- Request one or two NVIDIA GPUs, or one to four Tenstorrent boards. Other
  jobs can use the remaining devices; requests wait when capacity is occupied.
- Website: http://100.73.139.66:8088. API on that origin: `/api`; auth: `/auth`.

The department rollout includes admin/member login, team common/member folders,
explicit cross-team read/use grants, enforced storage/resource limits, durable
fair admission and revocation. The portal uses interactive compute choices,
clear feature panels and searchable, paginated job rows that expand for details.
Use **http://100.73.139.66:8088** privately through Tailscale.

See [the current department operating guide](docs/department-rollout.md),
[private deployment operations](deploy/private/README.md),
[completed execution plan and evidence](docs/department-rollout-execution.md), and
[portal design](docs/design-system.md). The earlier foundation guides record
historical phases.

Credits, Jupyter, chaining, browser image archives/builds and arbitrary distributed
training remain outside this release. Custom TT code/images must use compatible
TT libraries. Automatic backups, HTTPS and monitoring/cleanup products were
explicitly excluded. Hosting remains private HTTP over Tailscale.

## Developer preview against this cluster

**Keep production authentication/team enforcement enabled for research users.**
The standalone trusted-owner mode below is only for development.

Use Go 1.25.1 and Node.js 22.16.0 or newer. Apply the workload prerequisites
and claim templates described in the deployment guide first. Kubernetes is
now the default executor; Redis and Docker supervisors are unnecessary for
this path. This server has both runtimes under `~/.local/share/mist-runtimes`:

```bash
export PATH=/home/utmist/.local/share/mist-runtimes/go-1.25.1/bin:/home/utmist/.local/share/mist-runtimes/node-22.16.0/bin:$PATH
```

```bash
export KUBECONFIG=/home/utmist/.kube/config
# Standalone development defaults to a trusted local owner with auth disabled.
# For the deployed authenticated/shared API, use its existing port-forward.
export MIST_PILOT_OWNER=local-dev
cd src
go run .
```

If the deployed API's port-forward already occupies port 3000, use that API
or stop only `mist-api-forward.service` before starting a local backend.
Standalone `go run` uses the default base-image allowlist. Set
`MIST_ALLOWED_IMAGES` to the deployment's approved references when developing
with the packaged example images or other custom images.

In another terminal:

```bash
cd web-interface
npm ci
npm run dev -- --host 127.0.0.1 --port 3001 --strictPort
```

The Vite proxy forwards `/api` and `/auth` to the API on port 3000. The installed user
services already run both endpoints on this machine:

```bash
systemctl --user status mist-api-forward mist-web
```

## CLI

From the repository root:

```bash
go -C cli build -o ../bin/mist .
printf "print('Hello from a real Mist job')\n" > /tmp/hello.py
export MIST_API_URL=http://100.73.139.66:8088/api
bin/mist auth login --email your-member-email@example.org
bin/mist job submit /tmp/hello.py --compute CPU
bin/mist job list --all
bin/mist job status <job-id>
bin/mist job logs <job-id>
bin/mist job cancel <active-job-id>
```

Use `--compute NVIDIA --devices 1` for one GPU, or
`--compute TT --devices 1` for one two-chip board. The submitted script must
actually use the chosen accelerator. `--api-url` or `MIST_API_URL` selects an
API endpoint. See [CLI setup](cli/docs/setup.md) for resource options.

## Tests and legacy development

```bash
go -C src test -run 'Test(Kubernetes|Hardware|GPURequest|CustomImage|TenstorrentCustom|Shared|Member)' ./...
go -C cli test ./...
cd web-interface
npm test -- --run
npm run build
npx tsc --noEmit
```

The older Docker/Redis executor is available with `MIST_EXECUTOR=docker`.
Its supervisor execution is incomplete. For its existing tests, start a
local Redis instance and run `go -C src test ./...`; Docker integration tests
also need Docker permissions and their test image. This path is retained for
compatibility and is separate from the validated Kubernetes executor.
