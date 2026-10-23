#!/usr/bin/env python3
"""Small live checks: regular-member uploads, isolation, and NVIDIA/TT training.

Creates two small verification workspaces and one regular account. No large files,
quota exhaustion, migration or old-data deletion. Credentials stay outside Git.
"""
import argparse
import hashlib
import http.cookiejar
import io
import json
import os
from pathlib import Path
import random
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile


class Client:
    def __init__(self, base):
        self.base = base.rstrip('/')
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, path, body=None, team=None, method=None, expected=200, raw=False):
        payload = body if isinstance(body, bytes) else json.dumps(body).encode() if body is not None else None
        headers = {'Origin': self.base, 'Content-Type': 'application/octet-stream' if isinstance(body, bytes) else 'application/json'}
        if team:
            headers['X-Mist-Team'] = team
        request = urllib.request.Request(self.base + path, data=payload, headers=headers, method=method)
        try:
            response = self.opener.open(request, timeout=30)
        except urllib.error.HTTPError as error:
            if error.code != expected:
                raise RuntimeError(f'{path}: unexpected HTTP {error.code}') from None
            response = error
        with response:
            assert response.code == expected, (path, response.code)
            return response.read() if raw else json.load(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default='http://100.73.139.66:8088')
    parser.add_argument('--credentials', type=Path, default=Path('/home/utmist/.config/mist/admin-bootstrap.json'))
    parser.add_argument('--out', required=True, type=Path)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(args.out, 0o700)
    report = {'checks': [], 'jobs': [], 'bulk_testing': False}
    admin, member = Client(args.base), Client(args.base)
    jobs = []
    stamp = secrets.token_hex(4)

    def passed(name):
        report['checks'].append(name)
        print('PASS ' + name, flush=True)

    try:
        creds = json.loads(args.credentials.read_text())
        identity = admin.call('/auth/sign-in/email', {'email': creds['email'], 'password': creds['password']})['user']
        teams = [admin.call('/api/teams', {'name': 'Storage verification ' + stamp + suffix,
                 'storage_gib': 1, 'model_storage_gib': 1}, expected=201) for suffix in (' A', ' B')]
        for team in teams:
            admin.call('/api/teams/' + team['id'] + '/members', {'user_id': identity['id'], 'common_writer': True})
        email, password = 'storage-check-' + stamp + '@mist.local', secrets.token_urlsafe(24)
        user = admin.call('/auth/admin/create-user', {'name': 'Storage check ' + stamp, 'email': email,
                          'password': password, 'role': 'user'})['user']
        admin.call('/api/teams/' + teams[0]['id'] + '/members', {'user_id': user['id'], 'common_writer': False})
        member.call('/auth/sign-in/email', {'email': email, 'password': password})
        team = teams[0]['id']
        state = args.out / 'member-private.json'
        state.write_text(json.dumps({'email': email, 'password': password, 'team_id': team}))
        state.chmod(0o600)
        for _ in range(60):
            try:
                storage = member.call('/api/storage', team=team)
                break
            except RuntimeError:
                time.sleep(1)
        else:
            raise AssertionError('team folders did not become ready')
        assert storage['storage_mode'] == 'shared-folders' and storage['storage_limits_enforced'] is False
        assert storage['upload_limit_bytes'] > 0
        passed('regular_member_login_and_shared_folder_readiness')
        member.call('/api/jobs', team=teams[1]['id'], expected=403)
        member.call('/api/datasets?filename=forbidden.txt&scope=common', b'no', team=team, expected=403)
        passed('membership_and_common_write_permissions_enforced')

        archive = io.BytesIO()
        with zipfile.ZipFile(archive, 'w') as z:
            z.writestr('../escape.txt', 'no')
        member.call('/api/datasets?filename=unsafe.zip&format=zip', archive.getvalue(), team=team, expected=400)
        passed('unsafe_archive_rejected')

        rng = random.Random(42)
        weights = [[rng.gauss(0, .1) for _ in range(32)] for _ in range(32)]
        x = [[rng.gauss(0, 1) for _ in range(32)] for _ in range(128)]
        vx = [[rng.gauss(0, 1) for _ in range(32)] for _ in range(64)]
        def targets(rows):
            return [[sum(row[k] * weights[k][j] for k in range(32)) for j in range(32)] for row in rows]
        payload = json.dumps({'x': x, 'y': targets(x), 'validation_x': vx, 'validation_y': targets(vx)}).encode()
        assert len(payload) < 400000
        dataset = member.call('/api/datasets?filename=regression.json&name=Ethernet%20storage%20check', payload, team=team, expected=201)
        report['dataset_bytes'] = len(payload)
        assert dataset['sha256'] == hashlib.sha256(payload).hexdigest()
        download = '/api/datasets/' + dataset['id'] + '/files/download?path=regression.json'
        assert member.call(download, team=team, raw=True) == payload
        admin.call('/api/datasets/' + dataset['id'], team=teams[1]['id'], expected=404)
        admin.call('/api/storage/files?' + urllib.parse.urlencode({'source_team': team, 'scope': user['id']}), team=teams[1]['id'], expected=404)
        passed('small_upload_checksum_download_and_cross_team_isolation')

        source = (Path(__file__).parent / 'examples/train_shared.py').read_text()
        for backend, expected in [('nvidia', 1), ('tenstorrent', 2)]:
            script = source.replace('args = parser.parse_args()',
                     f"args = parser.parse_args(['{backend}','--expected-devices','{expected}','--output','/outputs'])")
            job = member.call('/api/jobs', {'name': 'Ethernet folder training ' + backend, 'type': 'command',
                   'accelerator': backend, 'device_count': 1, 'dataset_id': dataset['id'], 'script': script,
                   'script_name': 'train.py', 'timeout_seconds': 600}, team=team, expected=201)['job_id']
            jobs.append(job)
            report['jobs'].append({'id': job, 'backend': backend})
        member.call('/api/datasets/' + dataset['id'], team=team, method='DELETE', expected=409)
        passed('active_dataset_deletion_blocked')

        deadline = time.monotonic() + 660
        for job in report['jobs']:
            while time.monotonic() < deadline:
                status = member.call('/api/jobs/' + job['id'], team=team)
                if status['job_state'] in ('Success', 'Failure', 'Cancelled'):
                    break
                time.sleep(2)
            log = member.call('/api/jobs/' + job['id'] + '/logs', team=team)['logs']
            (args.out / (job['backend'] + '.log')).write_text(log)
            assert status['job_state'] == 'Success', (job['backend'], status.get('message'))
            assert 'DATASET_LOADED file=/inputs/regression.json' in log and 'TRAINING_PASSED' in log
            files = member.call('/api/jobs/' + job['id'] + '/files', team=team)['files']
            assert any(f['path'].endswith('.pt') for f in files)
            for file in files:
                content = member.call('/api/jobs/' + job['id'] + '/files/download?' + urllib.parse.urlencode({'path': file['path']}), team=team, raw=True)
                assert len(content) == file['size']
                if file['path'] == 'results.json':
                    job['metrics'] = json.loads(content)
            job['node'], job['status'] = status['node'], status['job_state']
            passed(job['backend'] + '_real_training_and_output_downloads')
        for _ in range(45):
            storage = member.call('/api/storage', team=team)
            if storage['dataset_used_bytes'] >= len(payload) and storage['model_used_bytes'] > 0:
                break
            time.sleep(1)
        assert storage['dataset_used_bytes'] >= len(payload) and storage['model_used_bytes'] > 0
        assert storage['usage_stale'] is False
        report['storage'] = storage
        passed('host_sampled_dataset_and_model_usage')
        report['teams'] = [t['id'] for t in teams]
    except Exception:
        for job in jobs:
            try:
                member.call('/api/jobs/' + job + '/cancel', team=team, method='POST')
            except Exception:
                pass
        raise
    finally:
        for client in (member, admin):
            try:
                client.call('/auth/sign-out', {}, method='POST')
            except Exception:
                pass
        (args.out / 'results.json').write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
