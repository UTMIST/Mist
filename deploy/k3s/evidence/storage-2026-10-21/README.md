# Shared-folder storage release verification

Verified on the real two-machine installation October 21, 2026 (America/New_York).
Source branch: `feat/simple-storage-ethernet-20261021`, based on main `ba2bb86`.

## Evidence

- [Validation summary](validation.json): source checks, small Ethernet sample,
  authentication prerequisites and operating boundaries.
- [Live API/training results](results.json): eight checks including regular-member
  access, upload checksum/download, ZIP traversal rejection, team isolation,
  active-dataset deletion protection, both accelerators and sampled usage.
- [Actual workload mounts](workload-volumes.json): direct Ethernet NFS exports,
  selected read-only inputs, scoped outputs and disabled service-account tokens.
- [Deployed API artifact](deployed-api.json): image identity and binary checksum,
  matched to the built candidate.
- [QuietBox audit](host-storage.json): ordinary ext4 directories, zero image files
  in the new root, service state, shared capacity and persistent Ethernet profile.
- [Browser checks](browser-results.json), [dataset screen](datasets-browser.png),
  [job screen](jobs-browser.png): real regular-member login, advisory display,
  an actual browser upload and visible completed jobs; no JavaScript errors.
- Training logs: [NVIDIA](nvidia.log), [Tenstorrent](tenstorrent.log).

Training uploaded **255,854 bytes**, requested **one NVIDIA GPU** and **one n300
board (two chips)**, read `/inputs/regression.json`, trained actual accelerator
operations and saved/reloaded/downloaded checkpoints from `/outputs`. Loss and
held-out validation checks passed. The browser added an 8-byte CSV afterward.

All **43 API + 6 CLI Go tests** pass with race detection, alongside vet,
**16 React tests**, TypeScript, lint/build and **6 Python folder tests**. The CLI
entry package has no test files; no actual test was skipped. Vite/launch-editor
were patched within the existing major version and npm reports zero vulnerabilities.

The host is now configured with the new API/web release and local folder service.
The separate old auth deployment remains scaled to zero for rollback. Existing
auth credentials and deactivated-account state were preserved. No physical disk
was formatted, old storage data deleted, large quota allocated, or bulk upload run.

## Failures resolved during verification

- Go-auth main previously accepted user-ID cookies, could not verify existing
  Better Auth password hashes, omitted an old schema's required `emailVerified`
  value, and did not honor old deactivated accounts. Targeted fixes and regression
  tests were required to deploy latest main safely.
- The first dynamic team bindings retained the old export prefix. Verification
  caught the mount failure; the new test jobs were cancelled, only the new wrong
  bindings were replaced, and the correct Ethernet/folder exports were tested.
  The successful results linked above are from the final rerun.
- NFS 4.2 reused an existing transport to the old LAN alias. New bindings use a
  separate NFS 4.1 connection, verified on the Ethernet path.

Private credentials, sessions, database backups and rollback manifests are kept
outside Git in `/home/utmist/mist-storage-validation-20261021`. The reusable
[live verifier](../../verify_storage_folders.py) creates its own test account and
workspaces; never publish its `member-private.json` file.

Operations and rollback: [storage guide](../../storage/README.md).
