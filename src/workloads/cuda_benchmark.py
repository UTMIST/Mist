"""Small, bounded FP32 matrix multiplication comparison; no downloads required."""
import json
import sys
import time

import torch

size = int(sys.argv[1])
assert size in (1024, 2048, 4096)
assert torch.cuda.is_available(), "CUDA is not available in this container"
torch.set_num_threads(4)
torch.manual_seed(42)
torch.backends.cuda.matmul.allow_tf32 = False
a = torch.randn(size, size)
b = torch.randn(size, size)
ga, gb = a.cuda(), b.cuda()
print(f"Running {size} x {size} FP32 matrix multiplication", flush=True)
print(f"GPU: {torch.cuda.get_device_name(0)}", flush=True)

for _ in range(5):
    gpu_result = ga @ gb
torch.cuda.synchronize()
start = time.perf_counter()
for _ in range(100):
    gpu_result = ga @ gb
torch.cuda.synchronize()
gpu_ms = (time.perf_counter() - start) * 1000 / 100

cpu_result = a @ b
start = time.perf_counter()
for _ in range(5):
    cpu_result = a @ b
cpu_ms = (time.perf_counter() - start) * 1000 / 5
torch.testing.assert_close(gpu_result.cpu(), cpu_result, rtol=1e-3, atol=1e-3)

result = {
    "gpu": torch.cuda.get_device_name(0),
    "torch_version": torch.__version__,
    "cuda_version": torch.version.cuda,
    "matrix_size": size,
    "gpu_ms": round(gpu_ms, 3),
    "cpu_ms": round(cpu_ms, 3),
    "speedup": round(cpu_ms / gpu_ms, 2),
    "gpu_iterations": 100,
    "cpu_iterations": 5,
    "cpu_threads": 4,
    "peak_vram_mib": round(torch.cuda.max_memory_allocated() / 2**20, 1),
    "verified": True,
}
print("CPU and GPU results match within FP32 tolerance.", flush=True)
print("MIST_RESULT=" + json.dumps(result), flush=True)
