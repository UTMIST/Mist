"""Verify migrated/split storage with small data and real accelerator training.

No quota exhaustion or bulk upload. Credentials stay in the local protected file.
"""
import hashlib
import http.cookiejar
import json
import math
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = 'http://100.73.139.66:8088'
TEAM = 'team-db3933031a6c2437'
OUT = Path('/home/utmist/mist-department-results-2026-10-09')
REPO = Path(__file__).resolve().parents[2]
credentials = json.loads(Path('/home/utmist/.config/mist/admin-bootstrap.json').read_text())
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
report = {'checks': {}, 'jobs': [], 'bulk_testing': False}

def api(path, data=None, method=None, expected=200, raw=False):
    body = data if isinstance(data, bytes) else json.dumps(data).encode() if data is not None else None
    r = urllib.request.Request(BASE + path, data=body, method=method,
        headers={'Origin': BASE, 'Content-Type': 'application/octet-stream' if isinstance(data, bytes) else 'application/json', 'X-Mist-Team': TEAM})
    try:
        response = client.open(r, timeout=30)
    except urllib.error.HTTPError as e:
        if e.code != expected: raise RuntimeError(f'{path}: HTTP {e.code}: {e.read().decode()}') from None
        return json.load(e)
    with response:
        assert response.status == expected, (path, response.status)
        return response.read() if raw else json.load(response)

def passed(name):
    report['checks'][name] = True
    print('PASS ' + name, flush=True)

def download(job, path):
    return api(f'/api/jobs/{job}/files/download?path=' + urllib.parse.quote(path, safe=''), raw=True)

try:
    api('/auth/sign-in/email', {'email': credentials['email'], 'password': credentials['password']})
    teams = api('/api/teams')
    team = next(t for t in teams['teams'] if t['id'] == TEAM)
    storage = api('/api/storage')
    assert storage['model_allocation_gib'] == team['policy']['model_storage_gib']
    assert storage['dataset_allocation_gib'] == team['policy']['storage_gib']
    assert storage['model_capacity_bytes'] > 0 and storage['upload_limit_bytes'] > 0
    report['storage'] = storage
    passed('both_pools_ready_with_independent_capacities')
    for field in ['storage_gib', 'model_storage_gib']:
        policy = dict(team['policy']); policy[field] -= 1
        api('/api/teams/' + TEAM, {'policy': policy}, method='PATCH', expected=400)
    policy = dict(team['policy']); policy['storage_gib'] = teams['storage_budget_gib']
    api('/api/teams/' + TEAM, {'policy': policy}, method='PATCH', expected=400)
    after = next(t for t in api('/api/teams')['teams'] if t['id'] == TEAM)
    assert after['policy'] == team['policy']
    passed('both_shrinks_and_combined_overcommit_rejected_without_mutation')
    # Local evidence retains the exact small checkpoint bytes downloaded before migration.
    evidence = OUT
    old_jobs = {'nvidia':'mist-8644eccafaf0b42c3d4add05', 'tenstorrent':'mist-e5c04ea2cc126a786d6fe783'}
    count = 0
    for backend, job in old_jobs.items():
        files = api('/api/jobs/' + job + '/files')['files']
        for f in files:
            saved = evidence / (backend + '-' + f['path'].replace('/', '_'))
            if saved.exists():
                assert hashlib.sha256(saved.read_bytes()).digest() == hashlib.sha256(download(job, f['path'])).digest()
                count += 1
        assert any(f['path'].endswith('.pt') for f in files)
    assert count >= 4, count
    report['preserved_checkpoint_files'] = count
    passed('old_accelerator_checkpoints_preserved_byte_for_byte')
    matrix = lambda n,m,p: [[math.sin((i+1)*(j+2)+p) for j in range(m)] for i in range(n)]
    x, vx = matrix(128,32,.1), matrix(64,32,1.2)
    w = [[v*.1 for v in row] for row in matrix(32,32,2.3)]
    multiply = lambda a: [[sum(row[j]*w[j][k] for j in range(32)) for k in range(32)] for row in a]
    payload = json.dumps({'x':x,'y':multiply(x),'validation_x':vx,'validation_y':multiply(vx)}).encode()
    assert len(payload) < 300000
    ds = api('/api/datasets?filename=regression.json&name=Small%20split%20storage%20check&scope=common', payload, expected=201)
    assert ds['size'] == len(payload) and ds['sha256'] == hashlib.sha256(payload).hexdigest()
    report['dataset_bytes'] = len(payload)
    passed('small_dataset_uploaded_with_verified_checksum')
    for backend in ['nvidia', 'tenstorrent']:
        training = (REPO / 'deploy/k3s/examples/train_shared.py').read_text().replace('args = parser.parse_args()',
            f"args = parser.parse_args(['{backend}','--expected-devices','2','--output','/outputs'])")
        job = api('/api/jobs', {'name':'Split storage ' + backend, 'type':'command', 'accelerator':backend,
            'device_count':2 if backend == 'nvidia' else 1, 'script':training, 'script_name':'train.py',
            'dataset_id':ds['id'], 'storage_scope':'common', 'timeout_seconds':600}, expected=201)['job_id']
        report['jobs'].append({'id':job,'backend':backend})
        for _ in range(240):
            result = api('/api/jobs/' + job)
            if result['job_state'] in ['Success','Failure','Cancelled']: break
            time.sleep(2)
        assert result['job_state'] == 'Success', result
        logs = api('/api/jobs/' + job + '/logs')['logs']
        assert 'DATASET_LOADED file=/inputs/regression.json' in logs and 'TRAINING_PASSED' in logs
        files = api('/api/jobs/' + job + '/files')['files']
        assert any(f['path'].endswith('.pt') for f in files)
        for f in files: assert len(download(job, f['path'])) == f['size']
        (OUT / ('split-' + backend + '.log')).write_text(logs)
        report['jobs'][-1]['node'] = result['node']
        # Inspect actual Kubernetes volumes, rather than inferring them from status.
        detail = json.loads(subprocess.check_output(['k3s','kubectl','-n','mist-' + TEAM,'get','job',job,'-o','json'],
            env={**os.environ,'KUBECONFIG':'/home/utmist/.kube/config'}))
        claims = {v['name']:v['persistentVolumeClaim']['claimName'] for v in detail['spec']['template']['spec']['volumes'] if 'persistentVolumeClaim' in v}
        assert claims['input'] == 'team-storage' and claims['checkpoints'] == 'team-models', claims
        pod = detail['spec']['template']['spec']
        assert next(v for v in pod['volumes'] if v['name'] == 'input')['persistentVolumeClaim']['readOnly']
        mounts = pod['containers'][0]['volumeMounts']
        assert next(m for m in mounts if m['mountPath'] == '/inputs')['readOnly']
        assert next(m for m in mounts if m['mountPath'] == '/outputs')['subPath'].endswith('/jobs/' + job + '/outputs')
        passed(backend + '_uploaded_input_readonly_and_models_saved_in_separate_pvc')
    # Preserve job outputs, remove only this small verifier dataset after its jobs finish.
    api('/api/datasets/' + ds['id'], method='DELETE')
    passed('own_small_probe_removed_after_terminal_jobs')
finally:
    api('/auth/sign-out', {})
    (OUT / 'split-storage-results.json').write_text(json.dumps(report, indent=2) + '\n')
