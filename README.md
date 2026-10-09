# Mist

UTMIST's compute platform. The Kubernetes pilot runs submitted CPU, NVIDIA,
and Tenstorrent workloads as real Jobs. The Jobs page and CLI use the same
API for submission, status, logs, and cancellation.

## Current pilot

- `utmist-z1opa08`: k3s server, CPU workloads, two NVIDIA RTX A4000 GPUs.
- `utmist-tt`: QuietBox worker, four n300 boards with two Wormhole chips each.
- Request one or two NVIDIA GPUs, or one to four Tenstorrent boards. Other
  jobs can use the remaining devices; requests wait when capacity is occupied.
- Jobs page: http://127.0.0.1:3001/jobs. API: http://127.0.0.1:3000.

The local pilot uses one configured owner, without user authentication,
credits, or team quotas. Keep access local. Tenstorrent scripts need compatible
TT-NN code; distributed training and arbitrary models have not been validated.

See [installation and live checks](deploy/k3s/README.md#mist-api-cli-and-jobs-page)
and [architecture and remaining work](docs/kubernetes-pilot.md).

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
export MIST_PILOT_OWNER=utmist
cd src
go run .
```

If the deployed API's port-forward already occupies port 3000, use that API
or stop only `mist-api-forward.service` before starting a local backend.

In another terminal:

```bash
cd web-interface
npm install --package-lock=false
npm run dev -- --host 127.0.0.1 --port 3001 --strictPort
```

The Vite proxy forwards `/api` to the API on port 3000. The installed user
services already run both endpoints on this machine:

```bash
systemctl --user status mist-api-forward mist-web
```

## CLI

From the repository root:

```bash
go -C cli build -o ../bin/mist .
printf "print('Hello from a real Mist job')\n" > /tmp/hello.py
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
go -C src test -run TestKubernetes ./...
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
