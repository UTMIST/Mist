# NVIDIA GPU smoke test on k3s

For the full as-built setup, recovery procedure, verification evidence, and
handoff notes, read the [single-node setup runbook](setup-runbook.md).

This pilot node runs Ubuntu 22.04, k3s 1.36, and two RTX A4000 GPUs. The host
driver is pre-installed. NVIDIA GPU Operator v26.7.1 is configured with
[`nvidia-gpu-operator-values.yaml`](nvidia-gpu-operator-values.yaml). Pin the
chart version when installing or upgrading:

```bash
sudo env KUBECONFIG=/etc/rancher/k3s/k3s.yaml helm upgrade --install gpu-operator \
  oci://nvcr.io/nvidia/cloud-native-charts/gpu-operator \
  --version v26.7.1 --namespace gpu-operator --create-namespace \
  --values deploy/k3s/nvidia-gpu-operator-values.yaml --wait
```

Run from the repository root. Check that the operator pods are healthy and the
node advertises two GPUs before submitting a workload:

```bash
sudo k3s kubectl -n gpu-operator get pods
sudo k3s kubectl describe node utmist-z1opa08
```

The `Allocatable` section must include `nvidia.com/gpu: 2`. Then run the
single-GPU CUDA smoke test:

```bash
sudo k3s kubectl create namespace mist
sudo k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
sudo k3s kubectl -n mist wait --for=condition=complete job/gpu-smoke --timeout=180s
sudo k3s kubectl -n mist logs job/gpu-smoke
```

The expected log contains `Test PASSED`. To rerun the Job, delete only the
finished smoke Job and apply the manifest again:

```bash
sudo k3s kubectl -n mist delete job gpu-smoke
sudo k3s kubectl apply -f deploy/k3s/gpu-smoke-job.yaml
```

No Service or NodePort is needed for a batch Job. This test verifies that k3s
can allocate an NVIDIA GPU and execute CUDA; it does not connect Mist's API to
Kubernetes yet. See [the execution pilot](../../docs/kubernetes-pilot.md) for
that application work.
