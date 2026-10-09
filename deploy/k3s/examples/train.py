"""Packaged regression example: CPU stdlib, or PyTorch CUDA on each GPU.

The tiny dataset is baked into the image until Mist's dataset service exists.
The default entrypoint and /app WORKDIR deliberately test image semantics.
"""
import argparse
import csv
import json
import os
from pathlib import Path


def cpu(rows, output):
    weight, bias = 0.0, 0.0
    initial = sum(y * y for x, y in rows) / len(rows)
    for _ in range(200):
        errors = [(weight * x + bias - y, x) for x, y in rows]
        weight -= 0.05 * 2 * sum(error * x for error, x in errors) / len(rows)
        bias -= 0.05 * 2 * sum(error for error, x in errors) / len(rows)
    loss = sum((weight * x + bias - y) ** 2 for x, y in rows) / len(rows)
    checkpoint = output / 'model.json'
    checkpoint.write_text(json.dumps({'weight': weight, 'bias': bias}) + '\n')
    restored = json.loads(checkpoint.read_text())
    assert restored['weight'] == weight and restored['bias'] == bias
    return [{'initial_loss': initial, 'final_loss': loss, 'checkpoint': checkpoint.name}]


def nvidia(rows, output, expected):
    import torch
    assert torch.cuda.is_available(), 'CUDA is unavailable; refusing CPU fallback'
    assert torch.cuda.device_count() == expected, 'Allocated GPU count differs'
    results = []
    for index in range(expected):
        device = torch.device(f'cuda:{index}')
        inputs = torch.tensor([[x] for x, y in rows], device=device)
        targets = torch.tensor([[y] for x, y in rows], device=device)
        model = torch.nn.Linear(1, 1, device=device)
        optimizer = torch.optim.SGD(model.parameters(), lr=0.05)
        initial = torch.nn.functional.mse_loss(model(inputs), targets).item()
        for _ in range(200):
            optimizer.zero_grad()
            loss = torch.nn.functional.mse_loss(model(inputs), targets)
            loss.backward()
            optimizer.step()
        final = torch.nn.functional.mse_loss(model(inputs), targets).item()
        checkpoint = output / f'nvidia-{index}.pt'
        torch.save(model.state_dict(), checkpoint)
        restored = torch.nn.Linear(1, 1, device=device)
        restored.load_state_dict(torch.load(checkpoint, weights_only=True, map_location=device))
        assert torch.allclose(model(inputs), restored(inputs))
        results.append({'device': torch.cuda.get_device_name(index), 'initial_loss': initial, 'final_loss': final, 'checkpoint': checkpoint.name})
    return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--backend', choices=['cpu', 'nvidia'], default='cpu')
    parser.add_argument('--devices', type=int, default=1)
    args = parser.parse_args()
    assert Path.cwd() == Path('/app'), 'Image WORKDIR was overridden'
    with Path('dataset.csv').open() as dataset:
        rows = [(float(row['x']), float(row['y'])) for row in csv.DictReader(dataset)]
    output = Path(os.environ.get('MIST_OUTPUT_DIR', '/outputs'))
    output.mkdir(parents=True, exist_ok=True)
    results = cpu(rows, output) if args.backend == 'cpu' else nvidia(rows, output, args.devices)
    assert all(result['final_loss'] < 0.001 for result in results), 'Training did not converge'
    evidence = {'backend': args.backend, 'cwd': str(Path.cwd()), 'results': results, 'greeting': os.environ.get('GREETING', '')}
    (output / 'results.json').write_text(json.dumps(evidence, indent=2) + '\n')
    print(json.dumps(evidence), flush=True)
    print(f'PACKAGED_TRAINING_PASSED backend={args.backend}', flush=True)


if __name__ == '__main__':
    main()
