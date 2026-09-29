# Single-node k3s and NVIDIA GPU pilot: setup and handoff

This runbook records the configuration observed on **2026-09-28**, a repeatable
setup and validation procedure, and the failure encountered during setup. Run
commands from the Mist repository root unless noted. The
[Kubernetes execution pilot](../../docs/kubernetes-pilot.md) describes the
separate work needed to connect Mist's API to Kubernetes Jobs.

## What the pieces do

The machine already had two NVIDIA GPUs and a working host driver. A **single
k3s server** was installed on it. A server includes a local kubelet and
container runtime and can run Jobs without a separate `k3s-agent` service.
The NVIDIA GPU Operator added the Kubernetes integration:

1. Its **device plugin** registers `nvidia.com/gpu` with the kubelet, allowing
   the scheduler to reserve a GPU for a Pod that requests one.
2. Its **container toolkit** prepares CDI device information that containerd
   uses to expose the assigned GPU and host driver libraries to a workload.
   The enabled NRI plugin handles GPU management containers and avoids
   rewriting k3s's containerd runtime configuration.
3. A CUDA sample Job requests `nvidia.com/gpu: 1` and exercises the device.

The operator was configured **not** to replace the existing host driver. Those
values are in
[`nvidia-gpu-operator-values.yaml`](nvidia-gpu-operator-values.yaml). NVIDIA
states that its NRI plugin is not yet generally available; this is a pilot
configuration. See the [GPU Operator installation guide](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/getting-started.html)
and [CDI/NRI guide](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/cdi.html).

## Observed state on 2026-09-28

| Item | Observed value |
| --- | --- |
| Node | `utmist-z1opa08`, one control-plane and worker node |
| OS | Ubuntu 22.04.5 LTS |
| k3s | `v1.36.4+k3s1`; `k3s.service` enabled and active |
| Container runtime | k3s containerd `2.3.4-k3s1.36` |
| GPUs | Two NVIDIA RTX A4000 cards |
| Host driver | NVIDIA `590.48.01`, already present before GPU Operator deployment |
| GPU Operator | Helm release `gpu-operator`, chart `v26.7.1`, namespace `gpu-operator` |
| Test namespace | `mist` |
| Node resource | Capacity and allocatable: `nvidia.com/gpu: 2` |

The node registered at 19:23 EDT. GPU Operator resources appeared around 19:39
EDT; Helm release revision 2 was deployed at 19:45 EDT. The `gpu-smoke` Job
completed at about 19:44 EDT. After the service fix, a new `gpu-confirm` Job
completed at 19:54 EDT. Both sample logs contained `Test PASSED`. Each Job
requested **one** GPU. The node advertised two, but these tests did not
independently identify or exercise each physical card.

These times and versions come from systemd, Kubernetes object timestamps,
`helm list`, `nvidia-smi`, and the completed Job logs. The exact historical
k3s installation command was not retained, so the commands below are a
reproduction recipe rather than a transcript.

## Reproduce or verify this pilot

Check versions against the target host before rebuilding or upgrading. Have a
working NVIDIA driver and `helm` available. Never commit the kubeconfig,
cluster token, or private keys to this repository.

### 1. Confirm the GPUs work on the host

```bash
nvidia-smi -L
nvidia-smi --query-gpu=name,driver_version --format=csv,noheader
```

On this node the first command lists two RTX A4000 cards. Resolve host driver
or hardware failures before diagnosing Kubernetes GPU allocation.

### 2. Run one k3s server

For a fresh **single-node** install, pin the version used in this pilot:

```bash
curl -sfL https://get.k3s.io | sudo env INSTALL_K3S_VERSION='v1.36.4+k3s1' sh -s - server
sudo systemctl is-active k3s
sudo k3s kubectl get nodes -o wide
```

Expect `active` and a `Ready` node. The installer writes an administrative
kubeconfig at `/etc/rancher/k3s/k3s.yaml`; `sudo k3s kubectl` uses it without
copying credentials. Any copied kubeconfig must be kept private. The
[k3s quick-start guide](https://docs.k3s.io/quick-start) explains that a
single server can host workloads and that agents join additional machines.

This same host must **not** run a separate `k3s-agent` service:

```bash
systemctl is-active k3s-agent
systemctl is-enabled k3s-agent
```

Expected outputs here are `inactive` and `disabled`. These commands can
return nonzero for those expected states.

### 3. Install the GPU Operator

The tracked Helm values disable Operator driver installation and enable its
NRI plugin for GPU management containers. Keep the chart version pinned when
reproducing this setup:

```bash
sudo env KUBECONFIG=/etc/rancher/k3s/k3s.yaml helm upgrade --install gpu-operator \
  oci://nvcr.io/nvidia/cloud-native-charts/gpu-operator \
  --version v26.7.1 --namespace gpu-operator --create-namespace \
  --values deploy/k3s/nvidia-gpu-operator-values.yaml --wait

sudo k3s kubectl -n gpu-operator get pods
sudo k3s kubectl describe node utmist-z1opa08
```

The device plugin and container toolkit Pods should be Running. In the node's
`Capacity` and `Allocatable` sections, expect `nvidia.com/gpu: 2` for this
hardware. A completed validator Pod is normal. The operator may need a little
time to register devices after k3s starts; check again after its Pods are
healthy.

### 4. Execute CUDA through k3s

The tracked [`gpu-smoke-job.yaml`](gpu-smoke-job.yaml) requests and limits one
`nvidia.com/gpu` device. Create the namespace if it does not already exist:

```bash
sudo k3s kubectl create namespace mist
sudo k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
sudo k3s kubectl -n mist wait --for=condition=complete job/gpu-smoke --timeout=180s
sudo k3s kubectl -n mist describe job gpu-smoke
sudo k3s kubectl -n mist logs job/gpu-smoke
```

Expect `1 Succeeded`, `nvidia.com/gpu: 1` in the Job's requests and limits,
and `Test PASSED` in the logs. This establishes that k3s scheduled a GPU Pod,
the runtime exposed a device inside the container, and CUDA executed. Merely
seeing the host GPUs or the Kubernetes resource count is a weaker check.

A completed Job does not rerun on `kubectl apply`. To repeat this test, delete
**only the completed smoke Job**, then apply the manifest again:

```bash
sudo k3s kubectl -n mist delete job gpu-smoke
sudo k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
sudo k3s kubectl -n mist wait --for=condition=complete job/gpu-smoke --timeout=180s
sudo k3s kubectl -n mist logs job/gpu-smoke
```

Save logs first if they are needed for an incident review. This batch Job
does not need a Service or NodePort.

## Failure encountered: duplicate agent on port 6444

Both `k3s.service` and `k3s-agent.service` had been enabled on the **same
host**. The agent listened on `127.0.0.1:6444`, which the server's API process
also needed. The server repeatedly exited with:

```text
failed to listen on 127.0.0.1:6444: bind: address already in use
```

During the conflict, `kubectl` could not connect to `127.0.0.1:6443`.
`nvidia-smi` still saw the GPUs, but new Kubernetes workloads could not use
them because the control plane was unavailable. Agent logs also referenced
`https://myserver:6443`, an example server name rather than this node's
working endpoint. The fix on 2026-09-28 was:

```bash
sudo systemctl stop k3s-agent.service
sudo systemctl disable k3s-agent.service
sudo systemctl is-active k3s
sudo k3s kubectl get nodes
```

The server recovered without replacing the host driver or reinstalling the
operator. If it does not recover, inspect logs before restarting it,
especially when other workloads are running:

```bash
systemctl status k3s k3s-agent --no-pager
journalctl -u k3s -u k3s-agent -n 100 --no-pager
ss -ltn '( sport = :6443 or sport = :6444 )'
```

## Troubleshooting and limits

- **API connection refused:** Check `k3s.service` and its journal first. On
  this node, confirm the duplicate agent has not been enabled again.
- **Node Ready but no `nvidia.com/gpu`:** Check `nvidia-smi -L`, the device
  plugin and toolkit Pods, and
  `sudo k3s kubectl -n gpu-operator logs daemonset/nvidia-device-plugin-daemonset --tail=100`.
  Compare live Helm values with the tracked file if the release has changed.
- **GPU Job Pending:** Use `sudo k3s kubectl -n mist describe pod <pod-name>`
  to inspect scheduler events and requested resources. Two GPUs are the
  observed capacity, not a promise that both are free at every moment.
- **Pod starts but CUDA fails:** Inspect its logs, host `nvidia-smi`, toolkit
  Pod logs, and device plugin logs. An advertised resource alone does not
  prove CUDA works.

The smoke test proves **one requested GPU worked through k3s on the test
date**. It does not prove independent operation of both cards, GPU sharing,
performance, reboot durability, multi-node behavior, or Mist API integration.
See [the pilot plan](../../docs/kubernetes-pilot.md) for application work.
