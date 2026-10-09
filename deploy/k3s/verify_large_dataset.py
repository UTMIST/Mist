"""Verify a streamed dataset exceeds 2 GiB and can be mounted by a real Job."""
import hashlib
import http.cookiejar
import json
import pathlib
import tempfile
import time
import urllib.request

OUT = pathlib.Path('/home/utmist/mist-department-results-2026-10-09')
BASE = 'http://100.73.139.66:8088'
credentials = json.loads(pathlib.Path('/home/utmist/.config/mist/admin-bootstrap.json').read_text())
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
report = {'checks': {}}
team = ''

def call(path, body=None):
    request = urllib.request.Request(BASE + path, data=json.dumps(body).encode() if body is not None else None,
        headers={'Origin': BASE, 'Content-Type': 'application/json', **({'X-Mist-Team': team} if team else {})})
    with client.open(request, timeout=30) as response:
        return json.load(response)

try:
    call('/auth/sign-in/email', {'email': credentials['email'], 'password': credentials['password']})
    admin = call('/api/session')['user']
    teams = call('/api/teams')['teams']
    existing = next((t for t in teams if t['name'] == 'Large dataset verification'), None)
    if existing is None:
        existing = call('/api/teams', {'name': 'Large dataset verification', 'storage_gib': 4})
    team = existing['id']
    request = urllib.request.Request(BASE + '/api/teams/' + team, method='PATCH', data=b'{"disabled":false}', headers={'Origin': BASE, 'Content-Type': 'application/json'})
    client.open(request, timeout=30).close()
    call('/api/teams/' + team + '/members', {'user_id': admin['id'], 'common_writer': True})
    for _ in range(60):
        try:
            storage = call('/api/storage')
            if storage['upload_limit_bytes'] > 2**31: break
        except Exception: pass
        time.sleep(1)
    else: raise RuntimeError('Large team filesystem not ready')
    # Sparse local source avoids allocating a second local multi-GiB copy;
    # all bytes are nevertheless streamed over the actual portal/NFS path.
    size = 2**31 + 4 * 1024**2
    with tempfile.TemporaryFile() as source:
        source.write(b'LARGE_DATASET_START')
        source.truncate(size)
        source.seek(size - len(b'LARGE_DATASET_END!')); source.write(b'LARGE_DATASET_END!')
        source.seek(0)
        digest = hashlib.sha256()
        while chunk := source.read(1024 * 1024): digest.update(chunk)
        checksum = digest.hexdigest()
        source.seek(0)
        request = urllib.request.Request(BASE + '/api/datasets?filename=large.bin&name=Large%20dataset%20probe', data=source,
            headers={'Origin': BASE, 'Content-Type': 'application/octet-stream', 'Content-Length': str(size), 'X-Mist-Team': team})
        started = time.monotonic()
        with client.open(request, timeout=14400) as response:
            dataset = json.load(response)
        assert dataset['size'] == size and dataset['sha256'] == checksum, dataset
    report.update(team_id=team, dataset_id=dataset['id'], bytes=size, upload_seconds=round(time.monotonic()-started, 1))
    report['checks']['actual_upload_over_2_gib_checksum_verified'] = True
    print('PASS STREAMED_UPLOAD_OVER_2_GIB_CHECKSUM', flush=True)
    script = f"from pathlib import Path\np=Path('/inputs/large.bin')\nassert p.stat().st_size=={size}\nwith p.open('rb') as f:\n assert f.read(19)==b'LARGE_DATASET_START'\n f.seek(-len(b'LARGE_DATASET_END!'),2)\n assert f.read()==b'LARGE_DATASET_END!'\nprint('LARGE_DATASET_JOB_PASSED',flush=True)\n"
    job = call('/api/jobs', {'name':'Large dataset input verification', 'type':'command', 'accelerator':'cpu', 'dataset_id': dataset['id'], 'script':script, 'script_name':'verify.py', 'timeout_seconds':120})['job_id']
    report['job_id'] = job
    for _ in range(90):
        result = call('/api/jobs/' + job)
        if result['job_state'] in ['Success','Failure','Cancelled']: break
        time.sleep(2)
    assert result['job_state'] == 'Success', result
    logs = call('/api/jobs/' + job + '/logs')['logs']
    assert 'LARGE_DATASET_JOB_PASSED' in logs
    (OUT/'large-dataset-job.log').write_text(logs)
    report['checks']['large_file_mounted_readonly_real_job'] = True
    # Remove only this verifier's large dataset after its sole Job has finished.
    request = urllib.request.Request(BASE + '/api/datasets/' + dataset['id'], method='DELETE', headers={'X-Mist-Team':team})
    client.open(request, timeout=30).close()
    report['checks']['own_probe_removed_after_job_no_user_files_deleted'] = True
    request = urllib.request.Request(BASE + '/api/teams/' + team, method='PATCH', data=b'{"disabled":true}', headers={'Origin': BASE, 'Content-Type': 'application/json'})
    client.open(request, timeout=30).close()
    print('PASS LARGE_FILE_MOUNT_AND_OWN_PROBE_CLEANUP', flush=True)
finally:
    call('/auth/sign-out', {})
    (OUT/'large-dataset-results.json').write_text(json.dumps(report, indent=2)+'\n')
