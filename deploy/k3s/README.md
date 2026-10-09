# Kubernetes deployment assets

Current installation: two k3s nodes, authenticated team jobs, scoped NFS storage
and private Tailscale portal. Start with the [k3s setup/handoff](../../docs/k3s-setup.md)
and [deployment operations](../private/README.md).

## Manifest and tooling map

| Assets | Purpose |
|---|---|
| `mist-api.yaml`, `mist-auth.yaml` | Single API/controller and Better Auth deployment |
| `team-rbac.yaml`, `team-workload-policy.yaml` | Team provisioning authority and workload admission |
| `nvidia-gpu-operator-values.yaml` | Pinned host-driver/CDI configuration |
| `tenstorrent-operator-values.yaml`, `mist-tenstorrent-claims.yaml` | TT operator configuration and retained legacy claims |
| `tenstorrent-workload-policy.yaml` | Admission protection for legacy `mist` diagnostics |
| `storage/` | NFS manifests, bounded pool provisioning, migration/growth |
| `training_smoke.py`, `examples/` | Accelerator checks and packaged-image examples |
| `gpu-smoke-job.yaml`, `training-*-job.yaml`, `submit_tenstorrent_smoke.py` | Administrator diagnostics in legacy namespace |
| `verify_department*`, `verify_split_storage.py`, `verify_large_dataset.py` | Current authenticated live verifiers |
| `verify_tenstorrent_allocation.py` | Infrastructure-level TT allocation diagnostic |
| `evidence/department-2026-10-09/` | Current release evidence |
| Earlier `*-results.json` | Historical dated test evidence |

Direct manifest diagnostics require cluster administrator access and bypass
portal team admission. Researchers should use the authenticated portal or CLI.
Archived pre-team API/browser verifiers have been retired from executable source.

The application image build uses [Dockerfile.api](Dockerfile.api). API/auth images
are imported into main's k3s containerd, not scheduled by a Docker supervisor.
Use `bash deploy/private/build-and-deploy.sh` for the existing installation.

[Old deployment history](../../docs/archive/k3s-deployment-history.md) and
[original node setup](../../docs/archive/k3s-setup-history.md) preserve earlier
observations; their unauthenticated/node-local instructions are superseded.
