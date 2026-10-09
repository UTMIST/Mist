"""Authenticate from QuietBox, train on NVIDIA node, then download the result.

Credentials arrive on standard input, never shell arguments or a remote file.
Run with a protected JSON credential file redirected into stdin. The caller
copies this script to QuietBox and passes only the source path to SSH.
"""
import http.cookiejar
import json
import sys
import time
import urllib.request

BASE = 'http://100.73.139.66:8088'
credentials = json.load(sys.stdin)
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

def request(path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data,
        headers={'Content-Type': 'application/json', 'Origin': BASE})
    with client.open(req, timeout=30) as response:
        return json.load(response)

request('/auth/sign-in/email', {'email': credentials['email'], 'password': credentials['password']})
ds = request('/api/datasets')['datasets'][0]
script = """import json
from pathlib import Path
import torch
assert torch.cuda.is_available()
assert torch.cuda.device_count() == 1
d=json.loads(Path('/inputs/regression.json').read_text())
x=torch.tensor(d['x'],device='cuda')
y=torch.tensor(d['y'],device='cuda')
w=torch.zeros((32,32),device='cuda',requires_grad=True)
optimizer=torch.optim.SGD([w],lr=0.2)
initial=(x@w-y).square().mean().item()
for _ in range(100):
 optimizer.zero_grad()
 loss=(x@w-y).square().sum()/(2*x.shape[0])
 loss.backward()
 assert w.grad.is_cuda
 optimizer.step()
final=(x@w-y).square().mean().item()
assert final<initial*0.02
Path('/outputs/remote-training.json').write_text(json.dumps({'initial':initial,'final':final,'cuda':True}))
print('REMOTE_NVIDIA_TRAINING_PASSED',flush=True)
"""
job = request('/api/jobs', {'name': 'Submitted from QuietBox', 'accelerator': 'nvidia',
    'device_count': 1, 'dataset_id': ds['id'], 'script': script, 'script_name': 'remote.py'})['job']
deadline = time.monotonic() + 300
while time.monotonic() < deadline:
    job = request('/api/jobs/' + job['id'])
    if job['job_state'] in ('Success', 'Failure', 'Cancelled'):
        break
    time.sleep(2)
assert job['job_state'] == 'Success', job
assert job['node'] == 'utmist-z1opa08', job
logs = request('/api/jobs/' + job['id'] + '/logs')['logs']
assert 'REMOTE_NVIDIA_TRAINING_PASSED' in logs
metrics = request('/api/jobs/' + job['id'] + '/files/download?path=remote-training.json')
request('/auth/sign-out', {})
print(json.dumps({'submitted_from': 'utmist-tt', 'job_id': job['id'], 'node': job['node'],
    'state': job['job_state'], 'downloaded_metrics': metrics}, indent=2))
