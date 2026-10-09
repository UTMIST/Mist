"""Small accelerator training check; TT gradients are computed explicitly in TT-NN.

CPU Torch creates the data, records metrics, and serializes checkpoints. All
forward passes, gradients, and weight updates run on the selected accelerator.
This is a linear-regression smoke test, not a model compatibility benchmark.
"""

import argparse
import json
import math
import os
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import torch


def dataset():
    generator = torch.Generator().manual_seed(42)
    truth = torch.randn(32, 32, generator=generator) * 0.1
    x = torch.randn(128, 32, generator=generator)
    validation_x = torch.randn(64, 32, generator=generator)
    return x, x @ truth, validation_x, validation_x @ truth


def await_start(output, start_file, metadata):
    if not start_file:
        return
    (output / "ready.json").write_text(json.dumps({**metadata,
        "ready_at": datetime.now(timezone.utc).isoformat()}) + "\n")
    print(f"BARRIER_READY {json.dumps(metadata)}", flush=True)
    deadline = time.monotonic() + 300
    while not start_file.exists():
        assert time.monotonic() < deadline, "Training start barrier timed out"
        time.sleep(0.2)


def check_and_save(backend, device_id, weights, initial, final, validation, output):
    change = weights.abs().max().item()
    assert all(math.isfinite(v) for v in (initial, final, validation, change))
    assert final < initial * 0.02, f"Loss did not converge: {initial} -> {final}"
    assert validation < 5e-4, f"Held-out MSE too high: {validation}"
    assert change > 0.05, "Weights were not updated"
    path = output / f"{backend}-{device_id}.pt"
    torch.save({"weights": weights.cpu(), "initial_mse": initial,
                "final_mse": final, "validation_mse": validation}, path)
    loaded = torch.load(path, map_location="cpu", weights_only=True)
    torch.testing.assert_close(loaded["weights"], weights.cpu(), rtol=0, atol=0)
    result = {"backend": backend, "device_id": device_id,
              "initial_mse": initial, "final_mse": final,
              "validation_mse": validation, "max_weight_change": change,
              "checkpoint": str(path)}
    print(json.dumps(result), flush=True)
    return result, loaded["weights"]


def nvidia(output, steps, expected_devices, start_file, board_pci):
    assert torch.cuda.is_available(), "CUDA unavailable; CPU fallback is forbidden"
    count = torch.cuda.device_count()
    assert count == expected_devices, f"Expected {expected_devices} allocated GPUs; saw {count}"
    print(f"CUDA devices={count}; torch={torch.__version__}", flush=True)
    nodes = sorted(str(path) for path in Path("/dev").glob("nvidia[0-9]*"))
    assert len(nodes) == count, f"Unexpected NVIDIA device nodes: {nodes}"
    (output / "allocation.json").write_text(json.dumps({"device_nodes": nodes,
                                                       "gpu_count": count}, indent=2) + "\n")
    results = []
    for index in range(count):
        device = torch.device(f"cuda:{index}")
        print(f"Training on {device}: {torch.cuda.get_device_name(index)}", flush=True)
        x, y, validation_x, validation_y = (t.to(device) for t in dataset())
        model = torch.nn.Linear(32, 32, bias=False).to(device)
        torch.nn.init.zeros_(model.weight)
        optimizer = torch.optim.SGD(model.parameters(), lr=0.2)
        if index == 0:
            await_start(output, start_file, {"device_nodes": nodes, "gpu_count": count})
        started = datetime.now(timezone.utc).isoformat()
        initial = (model(x) - y).square().mean().item()
        for step in range(steps):
            optimizer.zero_grad(set_to_none=True)
            residual = model(x) - y
            loss = residual.square().sum() / (2 * x.shape[0])
            loss.backward()
            assert model.weight.grad.is_cuda
            optimizer.step()
            if step % 20 == 0:
                print(f"cuda:{index} step={step} mse={residual.square().mean().item():.8f}", flush=True)
        with torch.no_grad():
            final = (model(x) - y).square().mean().item()
            validation = (model(validation_x) - validation_y).square().mean().item()
            result, weights = check_and_save("nvidia", index, model.weight.T.detach(),
                                              initial, final, validation, output)
            restored_error = (validation_x @ weights.to(device) - validation_y).square().mean().item()
            assert abs(restored_error - validation) < 1e-7
            result["checkpoint_reload_mse"] = restored_error
            result["device_name"] = torch.cuda.get_device_name(index)
            result["started_at"] = started
            result["finished_at"] = datetime.now(timezone.utc).isoformat()
            result["device_nodes"] = nodes
            results.append(result)
    return results


def tenstorrent(output, steps, expected_devices, start_file, board_pci):
    nodes = sorted(str(path) for path in Path("/dev/tenstorrent").iterdir())

    def pci_address(node):
        path = Path("/sys/class/tenstorrent") / ("tenstorrent!" + Path(node).name)
        assert path.exists(), f"Missing PCI identity for {node}"
        return path.resolve().parents[1].name

    if board_pci:
        nodes = [node for node in nodes if pci_address(node) == board_pci]
        assert len(nodes) == 1, f"PCI selector does not match one allocated board: {board_pci}"
    assert len(nodes) * 2 == expected_devices, f"Unexpected n300 device nodes: {nodes}"
    if len(nodes) > 1:
        # Arbitrary n300 subsets need not form a connected fabric mesh.
        # This test trains independently, so use one runtime per board.
        results = []
        for ordinal, node in enumerate(nodes):
            bdf = pci_address(node)
            board_output = output / ("board-" + Path(node).name)
            command = [sys.executable, __file__, "tenstorrent", "--steps", str(steps),
                       "--expected-devices", "2", "--output", str(board_output), "--board-pci", bdf]
            if start_file:
                command += ["--start-file", str(start_file)]
            print(f"Training reserved board {node}, PCI {bdf}", flush=True)
            subprocess.run(command, env={**os.environ, "TT_VISIBLE_DEVICES": bdf}, check=True)
            for result in json.loads((board_output / "results.json").read_text()):
                result["board_local_device_id"] = result["device_id"]
                result["device_id"] = ordinal * 2 + result["device_id"]
                result["board_pci"] = bdf
                results.append(result)
        assert len(results) == expected_devices
        (output / "allocation.json").write_text(json.dumps({"device_nodes": nodes,
                                                           "chip_count": expected_devices}, indent=2) + "\n")
        return results

    import ttnn

    count = ttnn.get_num_devices()
    assert count == expected_devices, f"Expected {expected_devices} allocated chips; saw {count}"
    (output / "allocation.json").write_text(json.dumps({"device_nodes": nodes,
                                                       "chip_count": count}, indent=2) + "\n")
    print(f"Tenstorrent devices={count}; torch used for data/metrics={torch.__version__}", flush=True)
    results = []
    for index in range(count):
        print(f"Opening Tenstorrent device {index}", flush=True)
        device = ttnn.open_device(device_id=index)
        try:
            if index == 0 and start_file:
                await_start(output, start_file, {"device_nodes": nodes, "chip_count": count})
            started = datetime.now(timezone.utc).isoformat()
            x, y, validation_x, validation_y = dataset()
            options = dict(dtype=ttnn.bfloat16, layout=ttnn.TILE_LAYOUT,
                           device=device, memory_config=ttnn.DRAM_MEMORY_CONFIG)
            dx, dy, dvx = (ttnn.from_torch(t, **options) for t in (x, y, validation_x))
            dxt = ttnn.from_torch(x.T.contiguous(), **options)
            weights = ttnn.from_torch(torch.zeros(32, 32), **options)

            def mse(inputs, target, matrix):
                prediction = ttnn.to_torch(ttnn.matmul(inputs, matrix)).float()
                return (prediction - target).square().mean().item()

            initial = mse(dx, y, weights)
            for step in range(steps):
                prediction = ttnn.matmul(dx, weights)
                residual = ttnn.subtract(prediction, dy)
                # Gradient of ||XW-Y||^2/(2N) is X.T@(XW-Y)/N.
                gradient = ttnn.matmul(dxt, residual)
                weights = ttnn.subtract(weights, ttnn.multiply(gradient, 0.2 / x.shape[0]))
                if step % 20 == 0:
                    print(f"tt:{index} step={step} mse={mse(dx, y, weights):.8f}", flush=True)
            final = mse(dx, y, weights)
            validation = mse(dvx, validation_y, weights)
            host_weights = ttnn.to_torch(weights).float()
            result, loaded = check_and_save("tenstorrent", index, host_weights,
                                            initial, final, validation, output)
            restored = ttnn.from_torch(loaded, **options)
            restored_error = mse(dvx, validation_y, restored)
            assert abs(restored_error - validation) < 1e-7
            result["checkpoint_reload_mse"] = restored_error
            result["started_at"] = started
            result["finished_at"] = datetime.now(timezone.utc).isoformat()
            result["device_nodes"] = nodes
            results.append(result)
        finally:
            ttnn.close_device(device)
    return results


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("backend", choices=("nvidia", "tenstorrent"))
    parser.add_argument("--steps", type=int, default=100)
    parser.add_argument("--output", type=Path, default=Path("/checkpoints"))
    parser.add_argument("--expected-devices", type=int,
                        help="Expected allocated chip/GPU count; defaults to the original whole-node test")
    parser.add_argument("--start-file", type=Path,
                        help="Optional shared-file barrier for the concurrent allocation test")
    parser.add_argument("--board-pci", help=argparse.SUPPRESS)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    expected = args.expected_devices or (2 if args.backend == "nvidia" else 8)
    results = globals()[args.backend](args.output, args.steps, expected, args.start_file, args.board_pci)
    (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    print(f"TRAINING_PASSED backend={args.backend} devices={len(results)}", flush=True)
