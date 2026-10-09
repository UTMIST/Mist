# Developer setup

Use Go **1.25.1** and Node.js **22.16.0 or newer**. Run repository commands from
`/home/utmist/Mist` on main, or your own checkout on a development machine.

On the existing main machine, the pinned runtimes are available at:

```bash
export PATH=/home/utmist/.local/share/mist-runtimes/go-1.25.1/bin:/home/utmist/.local/share/mist-runtimes/node-22.16.0/bin:$PATH
go version
node --version
```

## Frontend against the existing authenticated API

Main's `mist-api-forward` user service already forwards the API to
`127.0.0.1:3000`; `mist-web` runs a local Vite preview on `127.0.0.1:3001`.
These are development conveniences. The deployed Nginx portal runs independently.

```bash
systemctl --user status mist-api-forward mist-web
```

If those ports are free, use two terminals on main:

```bash
export KUBECONFIG=/home/utmist/.kube/config
k3s kubectl -n mist-system port-forward service/mist-api 3000:3000 --address 127.0.0.1
```

```bash
npm --prefix web-interface ci
npm --prefix web-interface run dev -- --host 127.0.0.1 --port 3001 --strictPort
```

Open http://127.0.0.1:3001 and sign in with your Mist account. This points to real
accounts, team storage and hardware; submitting jobs or changing policies affects
the installation. Do not start duplicate listeners while the user services run.

For a UI on another development machine, set `MIST_API_PROXY` to an API endpoint
reachable from it. Configure its exact browser origin in the API/auth trusted
origins before relying on session requests; Tailscale connectivity alone does
not add a trusted browser origin.

## Backend and CLI

The root `go.work` joins `src` and `cli`; it replaces the unused root module.

```bash
go test ./src/... ./cli/... -count=1
go vet ./src/... ./cli/...
go -C src build -o ../bin/mist-api .
go -C cli build -o ../bin/mist-cli .
```

`NewKubernetesApp` exposes the real API. Unit tests use fake Kubernetes clients
and temporary small files, so they need neither Redis nor Docker. Live verifiers
use the existing authenticated installation and protected credentials.

For backend changes, use an isolated test cluster/namespace with its own auth
and storage, or the documented maintenance deployment of this installation.
Standalone auth-disabled mode remains an explicit trusted development option;
it does not reproduce department authentication/team isolation. Never deploy it
for research users. `MIST_EXECUTOR=docker` is no longer supported and fails at
startup instead of selecting the retired supervisor.

## Deploying the existing installation

```bash
bash deploy/private/build-and-deploy.sh
```

This requires existing administrative kubeconfig, Docker/root access, auth
Secrets, operators and mounted storage. It is not a fresh-machine installer.
Read [operations](../deploy/private/README.md) and [testing](testing.md) first.
Current user/admin behavior is in [the operating guide](department-rollout.md).
