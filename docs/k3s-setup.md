# k3s setup and accelerator handoff

Verified on **October 9, 2026**. This guide describes the existing two-node
installation, fresh-install boundaries and diagnostic commands. Routine
application deployment is in [operations](../deploy/private/README.md).

## Installed cluster

| Item | Main/NVIDIA | QuietBox original |
|---|---|---|
| Kubernetes node | `utmist-z1opa08` | `utmist-tt` |
| Linux user | `utmist` | `utmist-tt` |
| Role/service | server + worker, `k3s` | worker only, `k3s-agent` |
| k3s | `v1.36.4+k3s1` | `v1.36.4+k3s1` |
| OS/kernel | Ubuntu 22.04.5 LTS / 6.8.0-138-generic | Same |
| containerd | `2.3.4-k3s1.36` | Same |
| LAN/InternalIP | `10.0.0.175` | `10.0.0.112` |
| LAN interface | `wlxe0d362c98a69` (Wi-Fi) | `enp201s0` (Ethernet to router) |
| Tailscale IP | `100.73.139.66` | `100.95.175.37` |
| Accelerator | 2 × NVIDIA RTX A4000 | 4 × n300 boards, 2 Wormhole chips per board |
| Operator | GPU Operator `v26.7.1` | TT-Operator `0.3.0`, Fabric Manager `0.2.30`, DRA `0.0.59` |

Main's host NVIDIA driver is **590.48.01**. QuietBox's `tenstorrent` kernel
module is loaded and `/dev/tenstorrent/0` through `3` exist. Operators retain
these host drivers. Both nodes were Ready and all operator deployments/daemonsets
were Ready at the audit. Service enablement was checked; a new reboot was not tested.

```text
Researcher → Tailscale → main Nginx :8088 → Mist API/auth
                                      |
                  k3s server :6443 at 10.0.0.175
                         /                    \
                  CPU/NVIDIA               TT agent
                                            10.0.0.112
                         \                    /
                         NFS on QuietBox :2049
```

The server already includes kubelet/containerd and can run workloads. Do not
start a separate agent on main: the earlier duplicate agent conflicted on port
6444. QuietBox has no independent `k3s` server service; its old test cluster was
retired before joining this server.

## Verify from main

Use the existing private admin kubeconfig. These commands do not print secrets:

```bash
export KUBECONFIG=/home/utmist/.kube/config
k3s kubectl get nodes -o wide
k3s kubectl get deployments,daemonsets -A
k3s kubectl get pods -n mist-system -o wide
k3s kubectl get deviceclasses,resourceslices
helm list -A
```

The admin kubeconfig is private infrastructure access, not a researcher client
configuration. Keep it and node tokens out of Git and job containers.

On main, expect `systemctl is-active k3s` to report active. On QuietBox, expect
`systemctl is-active k3s-agent` to report active. Both services are enabled at boot.

## Fresh server installation

This is for a new, prepared host. Do not reinstall the working control plane to
redeploy Mist or assume this reconstructs its existing accounts/storage.
Pin the observed version when reproducing it:

```bash
curl -sfL https://get.k3s.io | sudo env INSTALL_K3S_VERSION='v1.36.4+k3s1' sh -s - server
sudo systemctl is-active k3s
sudo k3s kubectl get nodes -o wide
```

The installer creates `/etc/rancher/k3s/k3s.yaml`. Choose stable, reachable node
addresses and unique hostnames. Pin `node-ip`/`flannel-iface` in configuration
when multiple interfaces could select the wrong route. This installation uses
LAN addresses, not Tailscale addresses, for its Kubernetes nodes.

## Join a fresh worker

First verify the worker can reach **10.0.0.175:6443**. A machine on another LAN
cannot join this setup solely by being in the same tailnet; cluster advertising,
Flannel, NFS routes and firewalls would need a planned network change.

For a clean worker, an administrator securely installs an appropriate persistent
join token in `/etc/rancher/k3s/agent-token`, owned by root with mode **0600**.
The persistent join token is obtained through authorized access to the server's
`/var/lib/rancher/k3s/server/node-token` or configured agent-token source;
do not paste it into Git, terminal transcripts or this guide. A temporary
bootstrap token is not a substitute for the persistent token used after restart.

QuietBox's current `/etc/rancher/k3s/config.yaml` contains:

```yaml
server: https://10.0.0.175:6443
token-file: /etc/rancher/k3s/agent-token
node-ip: 10.0.0.112
flannel-iface: enp201s0
```

Use the new worker's own LAN IP/interface when provisioning another host. Then:

```bash
curl -sfL https://get.k3s.io | sudo env INSTALL_K3S_VERSION='v1.36.4+k3s1' sh -s - agent
sudo systemctl is-active k3s-agent
sudo systemctl is-enabled k3s-agent
```

If the host already has another cluster, inspect its Jobs/PVCs and retain its
state before retiring it; the fresh-worker recipe does not migrate cluster data.
QuietBox's original test-server snapshot is root-only at
`/root/k3s-server-prejoin-2026-10-03.tar.gz`. It is historical, not a backup of the
current department platform.

## Cluster and storage traffic

| Destination/port | Purpose | Current network |
|---|---|---|
| Main TCP 6443 | Kubernetes API/agent registration | LAN, infrastructure access |
| Node UDP 8472 | Default Flannel VXLAN | LAN between nodes |
| Node TCP 10250 | kubelet access | LAN between nodes |
| QuietBox TCP 2049 | NFSv4 dataset/model mounts | LAN, main and QuietBox sources |
| Main TCP 8088 | Private portal | Tailscale and localhost listeners |
| Host TCP 22 | Ubuntu SSH | Authorized administrator access |

Pods use `10.42.0.0/16`; Services use `10.43.0.0/16`. QuietBox's UFW allows
8472/UDP and 10250/TCP from main on `enp201s0`, NFS from both LAN host IPs and
forwarding between the node pod subnets. Main's installed host firewall
restricts ordinary Tailscale users to the portal. Keep VXLAN/kubelet/NFS access
scoped to trusted infrastructure sources. Wi-Fi works for the current route;
large transfers depend on LAN throughput. No direct host-to-host cable is required.

## NVIDIA integration

The operator's device plugin advertises `nvidia.com/gpu: 2`. CDI injects assigned
GPUs into workload containers. The tracked values retain the host driver and
enable the operator's NRI/CDI configuration.

From the repository root on main, installation/upgrade for a prepared cluster:

```bash
export KUBECONFIG=/home/utmist/.kube/config
helm upgrade --install gpu-operator oci://nvcr.io/nvidia/cloud-native-charts/gpu-operator \
  --version v26.7.1 --namespace gpu-operator --create-namespace \
  --values deploy/k3s/nvidia-gpu-operator-values.yaml --wait
k3s kubectl -n gpu-operator get pods
k3s kubectl get node utmist-z1opa08 -o jsonpath='{.status.allocatable.nvidia\.com/gpu}{"\n"}'
```

Expect 2 GPUs advertised; that is total schedulable capacity, not currently free
capacity. Each request reserves 1 or 2 whole GPUs. GPU partitioning/time slicing
is not configured. Inspect host `nvidia-smi -L` locally if devices disappear.

## Tenstorrent integration

The operator uses existing NFD and host KMD/firmware. It advertises four `n300`
DRA devices, each a two-chip board. TT capacity is in ResourceSlices, not the
NVIDIA extended resource field.

```bash
export KUBECONFIG=/home/utmist/.kube/config
k3s kubectl label node utmist-tt tenstorrent.com/has-tt=true --overwrite
helm upgrade --install tt-operator oci://ghcr.io/tenstorrent/helm/tt-operator \
  --version 0.3.0 --namespace tt-operator-system --create-namespace \
  --values deploy/k3s/tenstorrent-operator-values.yaml
k3s kubectl -n tt-operator-system get pods
k3s kubectl get deviceclasses,resourceslices
```

Mist creates board-count ResourceClaimTemplates in managed team namespaces.
DRA allocates claims and CDI exposes only those boards. A one-board request can
use its two chips; other jobs can reserve remaining boards. Multi-board allocation
does not guarantee fabric topology or distributed model training.

The default TT profile mounts the tested host checkout/environment read-only;
a compatible custom image can use `tt_runtime: container`. TT smoke checks use
TT-NN forward/analytic-gradient/update operations, not a proof of arbitrary
PyTorch autograd/model compatibility.

## Test a workload

Use **Jobs → New job → NVIDIA/Tenstorrent → Training check** after signing in and
selecting your team. This exercises authentication, quotas, queue, allocator,
training and persistent output. Use the [CLI](../cli/docs/setup.md) from either
host to submit a real script through the same API.

The direct manifest below is an administrator infrastructure diagnostic in the
legacy `mist` namespace. It bypasses the portal's team admission and does not
establish team-policy enforcement. Run it only when the GPU is available:

```bash
export KUBECONFIG=/home/utmist/.kube/config
k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
k3s kubectl -n mist wait --for=condition=complete job/gpu-smoke --timeout=180s
k3s kubectl -n mist logs job/gpu-smoke
```

Expected CUDA marker: `Test PASSED`. Applying a completed Job does not rerun it;
retain needed evidence before deleting only that completed diagnostic Job.

## Troubleshooting

- **Node missing/NotReady:** inspect `systemctl status k3s` on main or
  `systemctl status k3s-agent` on QuietBox, then the matching journal and LAN route.
- **Main API down:** confirm the duplicate main agent stays inactive; inspect
  port 6444 conflicts before restarting infrastructure.
- **Missing NVIDIA resource:** check host driver and GPU operator/plugin pods.
- **Missing TT devices:** check `/dev/tenstorrent`, the KMD and TT operator pods,
  then ResourceSlices/ResourceClaims.
- **Job waiting:** inspect portal message, team limits and actual device capacity;
  cluster admins can inspect pod/claim events in its team namespace.
- **Image pull fails:** image must be accessible and contain compatible code.
  Locally imported Docker images must also be imported into k3s containerd.
- **Storage unavailable:** verify QuietBox's NFS/team-volume services and both pool
  mounts. Do not delete PVCs or overwrite mounted backing files.

Current live evidence is in [the verification record](../deploy/k3s/evidence/department-2026-10-09/README.md).
No new reboot, operator upgrade or rejoin was performed for this documentation audit.

References: [K3s quick start](https://docs.k3s.io/quick-start),
[network requirements](https://docs.k3s.io/installation/requirements),
[GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/getting-started.html),
[TT-Operator](https://docs.tenstorrent.com/tt-operator/latest/).
