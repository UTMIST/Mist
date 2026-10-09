# Mist

UTMIST's research compute platform. Submit Python scripts or tagged container
images, choose CPU, NVIDIA GPUs or Tenstorrent boards, upload datasets, and
collect persistent results through the same website or CLI.

**Private website:** http://100.73.139.66:8088, accessible through authorized
Tailscale connectivity. A Mist account and team membership are also required.

## Current installation

| Machine | Role | LAN address | Tailscale address |
|---|---|---|---|
| `utmist-z1opa08` | k3s server, API/auth, website, CPU and two RTX A4000 GPUs | `10.0.0.175` | `100.73.139.66` |
| `utmist-tt` | k3s worker, four n300 boards/eight chips, shared storage | `10.0.0.112` | `100.95.175.37` |

The cluster and NFS use the existing LAN. Tailscale provides private portal and
administrator SSH access. Kubernetes/containerd run workload containers.

## Documentation

Start with the [documentation index](docs/README.md).

- [Using the portal, teams and jobs](docs/department-rollout.md)
- [k3s setup, accelerator allocation and diagnostics](docs/k3s-setup.md)
- [Tailscale and SSH setup](docs/tailscale-ssh.md)
- [Architecture and code organization](docs/architecture.md)
- [Storage and upload limits](docs/storage.md)
- [Deployment and operations](deploy/private/README.md)
- [Developer setup](docs/development.md), [CLI](cli/docs/setup.md), [testing](docs/testing.md)

Main contains the department release merged through
[PR #110](https://github.com/UTMIST/Mist/pull/110). The earlier main and operating
snapshots are preserved in the [historical archive](docs/archive/README.md).

## Development and checks

Use Go **1.25.1** and Node.js **22.16.0 or newer**. The Go workspace contains the
API (`src`) and CLI (`cli`). From the repository root:

```bash
go test ./src/... ./cli/...
npm --prefix web-interface ci
npm --prefix web-interface test
npm --prefix web-interface run lint
npm --prefix web-interface run build
```

For a local UI against the authenticated deployed API, follow
[developer setup](docs/development.md). For the existing installation, deploy with
`bash deploy/private/build-and-deploy.sh`; read its [prerequisites](deploy/private/README.md)
before running it. Fresh-host provisioning is covered separately by the k3s guide.

The current API uses Kubernetes only. The unfinished Redis/Docker supervisor,
fake frontend account/chart and dummy CLI configuration have been retired.
Docker remains build tooling for images; it does not schedule Mist jobs.

Custom accelerator images need compatible training libraries. Credits, Jupyter,
chaining, browser image archives/builds, private pull-credential management and
distributed multi-machine training are outside this release. Automatic backups,
HTTPS and monitoring/cleanup products were explicitly excluded. This is a private
HTTP installation with one control plane, one auth instance and one storage host.
