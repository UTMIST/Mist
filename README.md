# Mist

UTMIST's compute platform. The Kubernetes pilot runs submitted CPU, NVIDIA,
and Tenstorrent workloads as real Jobs. The Jobs page and CLI use the same
API for submission, status, logs, and cancellation. The local foundation adds
live hardware inventory, approved container image execution, and persistent
per-job output directories.

## Current pilot

- `utmist-z1opa08`: k3s server, CPU workloads, two NVIDIA RTX A4000 GPUs.
- `utmist-tt`: QuietBox worker, four n300 boards with two Wormhole chips each.
- Request one or two NVIDIA GPUs, or one to four Tenstorrent boards. Other
  jobs can use the remaining devices; requests wait when capacity is occupied.
- Website: http://100.73.139.66:8088. API on that origin: `/api`; auth: `/auth`.

The five-part foundation now includes separate Mist member accounts, dataset
uploads, shared NFS storage on QuietBox, and browser output downloads. Use the
production website at **http://100.73.139.66:8088** from the existing Tailnet.
The local development preview remains at http://127.0.0.1:3001/jobs.

See [the complete foundation operating guide](docs/complete-foundation.md),
[private deployment operations](deploy/private/README.md),
[the original execution plan](docs/foundation-execution-plan.md), and
[the shared storage/deployment plan](docs/shared-storage-deployment-plan.md).

Credits, fair queues, Jupyter, chaining and arbitrary distributed training remain
outside this foundation. TT scripts must use compatible TT-NN code. Tailscale
HTTPS Serve requires enabling the feature on the tailnet; the current endpoint
is private HTTP over Tailscale, with no public hosting configured.

## Run locally against this cluster

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
