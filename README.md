# Mist

UTMIST's research compute platform. Submit Python scripts or tagged container
images, choose CPU, NVIDIA GPUs or Tenstorrent boards, upload datasets, and
collect persistent results through the same website or CLI.

**Private website:** [http://100.73.139.66:8088](http://100.73.139.66:8088), accessible through authorized
Tailscale connectivity. A Mist account and team membership are also required.

## Storage update — October 21

New submissions use ordinary team dataset/result folders on QuietBox, accessed
through the direct Ethernet link. Storage allowances are advisory; team access
is enforced. See [current storage operations](deploy/k3s/storage/README.md) for
layout, setup, API semantics, verification and rollback. The earlier operating
snapshots describe the retired image-backed layout.

## Current installation


| Machine          | Role                                                      | LAN address  | Tailscale address |
| ---------------- | --------------------------------------------------------- | ------------ | ----------------- |
| `utmist-z1opa08` | k3s server, API/auth, website, CPU and two RTX A4000 GPUs | `10.0.0.175` | `100.73.139.66`   |
| `utmist-tt`      | k3s worker, four n300 boards/eight chips, shared storage  | `10.0.0.112` | `100.95.175.37`   |


The cluster and NFS use the existing LAN. Tailscale provides private portal and
administrator SSH access. Kubernetes/containerd run workload containers.

## Documentation

Start with the [documentation index](docs/README.md).

- [Using the portal, teams and jobs](docs/content/department-rollout.md)
- [k3s setup, accelerator allocation and diagnostics](docs/content/k3s-setup.md)
- [Tailscale and SSH setup](docs/content/tailscale-ssh.md)
- [Architecture and code organization](docs/content/architecture.md)
- [Storage and upload limits](docs/content/storage.md)
- [Deployment and operations](deploy/private/README.md)
- [Developer setup](docs/content/development.md), [CLI](cli/docs/setup.md), [testing](docs/content/testing.md)

Main contains the department release merged through
[PR #110](https://github.com/UTMIST/Mist/pull/110). The earlier main and operating
snapshots are preserved in the [historical archive](docs/content/archive/_index.md).

## Development and checks

Use Go **1.26.10 or newer** and Node.js **22.16.0 or newer**. The Go workspace contains the
API (`src`) and CLI (`cli`). From the repository root:

```bash
go test ./src/... ./cli/...
npm --prefix web-interface ci
npm --prefix web-interface test
npm --prefix web-interface run lint
npm --prefix web-interface run build
```

For a local UI against the authenticated deployed API, follow
[developer setup](docs/content/development.md). For the existing installation, deploy with
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

### Documentation

Humans are responsible for editing the human facing docs. This is because the human facing docs will be read by humans, and it is rude to give them untested instructions to follow.

Documentation should include explanations of design decisions for later platform maintainers.

## Optional Misty login

Optional Misty email login is documented in
[the rollout guide](docs/agents/misty-login.md). It requires HTTPS, an explicit
expected Misty environment, and reviewed identity mappings for existing accounts.
It is disabled by default and has not been enabled on the live private portal.

## Frontend Testing

how to use fake backend for testing frontend.

(temporary, functionality, to be removed in future commit.)

```bash
# Terminal 1
npm --prefix web-interface run dev -- --host 127.0.0.1 --port 3001 --strictPort

# Terminal 2
npm --prefix web-interface run fake-backend
```



&nbsp;
