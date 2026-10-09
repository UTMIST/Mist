# Department verification — October 9, 2026

Evidence from the actual two-node private pilot before repository cleanup.
The original Go/Docker/logger counts below belong to that earlier release;
[current cleanup verification](../../../../docs/repository-cleanup.md) records
the maintained suites after their obsolete implementations were removed.
Reports contain synthetic
verification data and job/device IDs; no passwords, cookies or auth secrets.
Scripts are in `deploy/k3s/verify_department*`. They require existing authorized
local credentials and recorded machine addresses; inspect them before rerunning.
They create bounded test fixtures and preserve files/history afterward.

| Check | Evidence |
|---|---|
| Fourteen API/storage/queue/training/isolation/legacy checks | [results.json](results.json) |
| Eleven browser checks, including pagination and mobile | [browser-results.json](browser-results.json) |
| Streamed 2 GiB + 4 MiB dataset, SHA256 and mounted Job input | [large upload](large-dataset-results.json), [job log](large-dataset-job.log) |
| Two concurrent TT jobs, distinct boards; failure/deadline | [runtime-results.json](runtime-results.json) |
| SSH on TT → CLI → NVIDIA on main | [remote-cli-results.json](remote-cli-results.json), [status](remote-cli-status.txt), [log](remote-cli-job.log) |
| Six server admission denials | [admission-results.json](admission-results.json) |
| Installed host chains, synthetic untrusted source | [portal-firewall.log](portal-firewall.log) |
| NVIDIA training metrics / actual allocation | [metrics](nvidia-results.json), [allocation](nvidia-allocation.json), [log](mist-8644eccafaf0b42c3d4add05.log) |
| TT training metrics / actual allocation | [metrics](tenstorrent-results.json), [allocation](tenstorrent-allocation.json), [log](mist-e5c04ea2cc126a786d6fe783.log) |
| Job writes fill only the bounded test filesystem | [capacity log](mist-1c4a852d366b9d2349e1cf7c.log) |
| Actual container cannot reach private services/credentials | [isolation log](mist-b46534ddeb9f18852b75f645.log) |
| Full release suites and final browser rerun | [release-validation.json](release-validation.json) |
| Dependency audit, zero reported vulnerabilities | [web audit](web-audit.json), [auth audit](auth-audit.json) |

Screenshots: [jobs desktop](final-jobs-desktop.png),
[jobs mobile](final-jobs-mobile.png), [job composer](final-job-composer.png),
[mobile composer](final-job-composer-mobile.png), [datasets](final-datasets-desktop.png),
[overview](final-overview.png).

Saved training weights were downloaded and checked during verification. They
remain in the protected local evidence directory and team job outputs; binary
weights and protected account/session state are not checked in.

## Local checks

```bash
go -C src test ./... -run 'Test(Department|Kubernetes|Shared|Dataset|Auth|Member|Image|Hardware|Result)' -count=1
go -C cli test ./...
npm --prefix web-interface test
npx --prefix web-interface tsc --noEmit -p web-interface/tsconfig.json
```

Full frontend ESLint, shell/Python syntax checks and the production frontend
build passed. Use the pinned Go/Node versions in the deployment script. The final
release run passed all 33 backend and 10 Docker module tests, with no skips, using
a dedicated Redis container and the existing CPU Docker fixtures. CLI and logger
suites, TypeScript and all 10 React tests passed. The final browser rerun passed
all 11 checks with no browser errors. See [release validation](release-validation.json).

The host firewall test used an isolated synthetic source on the actual installed
chains, not an external researcher's laptop. Regression training demonstrates
the tested runtimes/devices; arbitrary models, hostile-code VM isolation, HA,
per-member hard storage quotas and excluded rollout features are not claimed.

## Separate storage pools follow-up

[Small-file storage report](split-storage-results.json): seven live checks passed,
using a 260,729-byte dataset. Both NVIDIA GPUs and one TT board/two chips trained
from the uploaded data and saved/downloaded outputs through the new model PVC.
Existing accelerator outputs match their pre-migration evidence byte for byte.
Both pool shrink requests and combined budget overcommit were rejected without
changing persisted policy. The own small dataset was removed after both jobs.
Logs: [NVIDIA](split-nvidia.log), [Tenstorrent](split-tenstorrent.log).

Six isolated migration tests passed using only tiny temporary files: content,
mode/ownership/link preservation, idempotence, retry after publication, conflicts,
partial stages, safe manifests and API access through member-folder parents.
Live NFS checks identified zero statfs filesystem IDs; readiness uses distinct
mount device identities. Dataset/model exports have separate stable files.

The eleven browser checks and nine React tests passed after migration. Large
uploads and quota exhaustion were not repeated, at the user’s request. These
checks do not demonstrate uploads at 200/500 GiB or every disk-full scenario.
The initial unused 1.5 TiB bulk reservation was released using filesystem discard,
preserving data. After reclaiming deleted test blocks, the sparse backing file
used about 6 GiB physically and QuietBox had about 1.9 TiB free on its host disk.

## Packaged image through the regular-member UI

[Container mode report](container-mode-result.json), [filled form](container-mode-form.png),
[completed job](container-mode-completed.png). The visible desktop browser selected
NVIDIA → Container → one GPU and submitted the custom image with blank command
and argument fields. Actual job manifest inspection confirmed the image startup
was preserved and no Python script was injected. Real CUDA backpropagation,
convergence, saved weights/metrics and checkpoint reload passed. The image was
preloaded locally, so this does not claim a registry push/pull test.

The walkthrough found and fixed an image-field editing bug: clearing an override
now leaves the field blank while the member types a replacement. A regression
test covers clearing, intermediate typing and preserving image startup defaults.
