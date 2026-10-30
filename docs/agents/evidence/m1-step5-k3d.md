# M1 step 5: K3s executor on k3d (30 October 2026)

This is local integration evidence for the K3s executor. It is **not** evidence
for any M1 release gate: k3d cannot show GPU visibility, NFS, the host firewall
or the production cleanup reporter. Those are steps 7–8 on the real nodes. See
[implementation contracts](../architecture/implementation-contracts.md#m1-acceptance-matrix).

## Environment

| Component | Version |
|---|---|
| Host | WSL2, kernel 6.18.33.2-microsoft-standard-WSL2, cgroup v2 |
| Docker | 29.1.3 |
| k3d | v5.9.0 |
| k3s image | `rancher/k3s:v1.36.5-k3s1` |
| containerd (node) | 2.3.4-k3s1.36 |
| Network policy controller | kube-router v2.6.3-k3s1 (built into k3s) |
| Node features | `supplementalGroupsPolicy: true` |
| Runtime image | test stand-in: `ubuntu:22.04` + `mist-job-entry` |

## Command

```bash
scripts/k3d-test.sh
```

## Results

All eight `TestK3d*` tests passed. The executor ran impersonating
`system:serviceaccount:mist-system:mistd`, so it had only the permissions in
`deploy/k3s/rbac.yaml`.

| Test | What it showed |
|---|---|
| `BatchJobIsolation` | `uid=20001(alice) gid=40001(team-team-a) groups=40001(team-team-a),19000(mist-users),20001(alice)`. Only `team-a` under `/mist/teams`. No service-account token, read-only root, writable `/tmp` and `/scratch`, umask `0027`. The API service, the node's API server and kubelet were blocked; `1.1.1.1:53` was reachable. |
| `TerminateForwardsSIGTERM` | The script's TERM trap ran; reason `signaled` |
| `OutOfMemory` | 64 MiB limit; kubelet reported `OOMKilled`, giving reason `out_of_memory` |
| `LocalTimeLimit` | 3 s limit: the wrapper sent SIGTERM; exit 143, reason `deadline` |
| `SetupFailureIsLaunchFailed` | A missing output directory gave `launch_failed`/`launch_error`; the work never ran |
| `ExecutorRestart` | A new executor instance listed the running job from receipts, refused to relaunch it, and stopped it |
| `RBACIsNarrow` | Listing Secrets in `mist-jobs` or Pods in `kube-system` was forbidden |
| `SchedulerEndToEnd` | PENDING → LAUNCHING → RUNNING → CLEANING → COMPLETED; the allocation was held until the node's cleanup receipt |

## Finding

Before the isolation gate existed, a job's first connections (within about
1 s of container start) reached the API service, the node's API server and
kubelet. The same probes were blocked about 1.5 s later. kube-router programs
Pod rules after start, so the gate init container was added. See
[executors.md](../architecture/executors.md#network).
