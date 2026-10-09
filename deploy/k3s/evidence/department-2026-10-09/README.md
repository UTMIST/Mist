# Department verification — October 9, 2026

Evidence from the actual two-node private pilot. Reports contain synthetic
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

Changed-file ESLint and shell/Python syntax checks also passed. Use the pinned
Go/Node versions in the deployment script. The full historical Go suite requires
its legacy Redis/Docker integration fixtures and was not claimed to pass.

The host firewall test used an isolated synthetic source on the actual installed
chains, not an external researcher's laptop. Regression training demonstrates
the tested runtimes/devices; arbitrary models, hostile-code VM isolation, HA,
per-member hard storage quotas and excluded rollout features are not claimed.
