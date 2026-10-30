# Mist

UTMIST's research compute platform. Mist works like the batch systems on
national HPC clusters: each researcher gets a folder inside their team folders,
logs in over SSH, moves files with `scp`/`rsync`, and runs batch or interactive
jobs that a scheduler places on NVIDIA GPU, CPU and Tenstorrent hardware.

## Status

Under redesign (October 2026). The previous container-image platform was never
deployed and has been removed from the tree; see Git history before 27 October
2026 if you need it. The new implementation starts from the design docs.

## Design

- [ARCHITECTURE.md](ARCHITECTURE.md): system overview, components, owners,
  milestones and code layout.
- [docs/agents/architecture/](docs/agents/architecture/decisions.md): per-component
  contracts and the decision log.
- [docs/agents/plans/](docs/agents/plans/jobs-and-scheduling.md): implementation
  plans per owner.

## Cluster

| Machine          | Role                                                      | LAN address  | Tailscale address |
| ---------------- | --------------------------------------------------------- | ------------ | ----------------- |
| `utmist-z1opa08` | Two RTX A4000 GPUs, CPU                                    | `10.0.0.175` | `100.73.139.66`   |
| `utmist-tt`      | Four n300 Tenstorrent boards (eight chips), shared storage | `10.0.0.112` | `100.95.175.37`   |

## Development

A Nix dev shell (`flake.nix`, `shell.nix`) provides Go, Node.js, Python, k3s and
kubectl. Run the Go tests with:

```bash
go test ./...
```

Database tests start an embedded Postgres as the current user (downloaded to
`~/.embedded-postgres-go` on first run). To use an existing server instead, set
`MIST_TEST_DATABASE_URL` to a superuser URL; each test creates its own database.

For local development against the fixture accounts (alice, bob, carol,
root-admin), build with `-tags mistdev` and pass `-dev-fixture` to both `mistd
api` and `mistd scheduler`; requests then name the user in `X-Mist-Dev-User`.
Production builds contain neither and reject the header. Run the tests with the
tag too:

```bash
go test -tags mistdev ./internal/api ./cmd/...
```

Executor and API integration tests run on a local k3d cluster (needs Docker without
sudo, plus k3d and kubectl):

```bash
scripts/k3d-test.sh
```

Check documentation links with:

```bash
python3 scripts/check-docs.py
```

### Documentation

Humans are responsible for editing the human facing docs. This is because the human facing docs will be read by humans, and it is rude to give them untested instructions to follow.

Documentation should include explanations of design decisions for later platform maintainers.
