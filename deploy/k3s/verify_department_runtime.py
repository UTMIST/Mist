"""Live execution checks for the recorded pilot; credentials remain local."""
import http.cookiejar
import json
import pathlib
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path('/home/utmist/mist-department-results-2026-10-09')
BASE = 'http://100.73.139.66:8088'
TEAM = 'team-db3933031a6c2437'
credentials = json.loads(pathlib.Path('/home/utmist/.config/mist/admin-bootstrap.json').read_text())
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
report = {'checks': {}, 'jobs': []}


def api(path, body=None):
    request = urllib.request.Request(BASE + path, data=json.dumps(body).encode() if body is not None else None,
        headers={'Origin': BASE, 'Content-Type': 'application/json', 'X-Mist-Team': TEAM})
    with client.open(request, timeout=30) as response:
        return json.load(response)


def submit(name, **data):
    job = api('/api/jobs', {'name': name, 'type': 'command', 'accelerator': 'cpu', 'timeout_seconds': 120,
        'command': ['python', '-c', "print('PASSED',flush=True)"], **data})['job_id']
    report['jobs'].append({'id': job, 'name': name})
    return job


def terminal(job, expected):
    for _ in range(120):
        result = api('/api/jobs/' + job)
        if result['job_state'] in ['Success', 'Failure', 'Cancelled']:
            assert result['job_state'] == expected, result
            log = api('/api/jobs/' + job + '/logs')['logs']
            (ROOT / (job + '.log')).write_text(log)
            return result, log
        time.sleep(2)
    raise RuntimeError('Job timed out ' + job)


try:
    api('/auth/sign-in/email', {'email': credentials['email'], 'password': credentials['password']})
    first = submit('Concurrent TT board A', type='training-smoke', accelerator='tenstorrent', device_count=1, command=None)
    second = submit('Concurrent TT board B', type='training-smoke', accelerator='tenstorrent', device_count=1, command=None)
    allocations = {}
    concurrent = False
    for _ in range(120):
        jobs = [api('/api/jobs/' + job) for job in [first, second]]
        if all(j['job_state'] == 'InProgress' for j in jobs):
            concurrent = True
        for job in jobs:
            if job.get('devices'):
                allocations[job['id']] = job['devices']
        if all(j['job_state'] in ['Success', 'Failure', 'Cancelled'] for j in jobs):
            break
        time.sleep(1)
    assert concurrent, 'TT jobs did not overlap'
    for job in [first, second]:
        result, log = terminal(job, 'Success')
        assert result['node'] == 'utmist-tt' and 'TRAINING_PASSED backend=tenstorrent devices=2' in log, log
    assert len(allocations) == 2, allocations
    left = {(d['pool'], d['device']) for d in allocations[first]}
    right = {(d['pool'], d['device']) for d in allocations[second]}
    assert len(left) == len(right) == 1 and left.isdisjoint(right), allocations
    report['allocations'] = allocations
    report['checks']['concurrent_tt_jobs_distinct_boards_real_training'] = True
    print('PASS CONCURRENT_TT_JOBS_DISTINCT_BOARDS_TRAINING')
    failure = submit('Intentional failure', command=['python', '-c', "print('EXPECTED_EXIT_7',flush=True); raise SystemExit(7)"])
    result, log = terminal(failure, 'Failure')
    assert result.get('exit_code') == 7 and 'EXPECTED_EXIT_7' in log
    report['checks']['nonzero_exit_visible_no_retry'] = True
    deadline = submit('Execution deadline', timeout_seconds=10, command=['python', '-c', "import time; print('DEADLINE_STARTED',flush=True); time.sleep(120)"])
    result, log = terminal(deadline, 'Failure')
    assert 'Deadline' in result.get('message', '') or 'deadline' in result.get('message', '').lower(), result
    report['checks']['execution_deadline_failure_visible'] = True
    print('PASS EXIT_CODE_AND_DEADLINE_FAILURES')
finally:
    api('/auth/sign-out', {})
(ROOT / 'runtime-results.json').write_text(json.dumps(report, indent=2) + '\n')
