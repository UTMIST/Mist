"""Live allocation/output checks for the local Mist foundation.

Run after verify_foundation_browser.mjs. Requests use the Mist API; admin
kubectl only reads cluster state and creates temporary read-only output probes.
Jobs/results are retained. Holders are cancelled even if a check fails.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:3000')
    parser.add_argument('--artifacts', type=Path, required=True)
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    evidence = {'started_at': datetime.now(timezone.utc).isoformat(), 'jobs': {}, 'checks': {}}
    run = datetime.now(timezone.utc).strftime('foundation-%m%d-%H%M%S')
    holders = set()

    def api(method, path, body=None, expected=200):
        request = urllib.request.Request(args.url + path, method=method,
            data=json.dumps(body).encode() if body is not None else None,
            headers={'Content-Type': 'application/json'})
        try:
            response = urllib.request.urlopen(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            value = json.load(response)
            assert response.code == expected, (path, response.code, value)
            return value

    def kubectl(*options, body=None):
        return subprocess.check_output(['k3s', 'kubectl', *options],
            input=json.dumps(body) if body else None, text=True, timeout=45)

    def wait(check, description, timeout=180):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = check()
            if value:
                return value
            time.sleep(1)
        raise AssertionError('Timed out: ' + description)

    def submit(label, **request):
        value = api('POST', '/jobs', {'name': run + '-' + label, **request}, expected=201)
        evidence['jobs'][label] = value['job_id']
        print('Submitted', label, value['job_id'], flush=True)
        return value['job_id']

    def status(job):
        return api('GET', '/jobs/' + job)

    def logs(job):
        return api('GET', '/jobs/' + job + '/logs')['logs']

    def finished(job, desired='Success'):
        def check():
            value = status(job)
            if value['job_state'] in ('Success', 'Failure', 'Cancelled'):
                assert value['job_state'] == desired, value
                return value
        value = wait(check, job + ' finished')
        text = logs(job)
        (args.artifacts / (job + '.log')).write_text(text)
        evidence['checks'][job] = {'status': value, 'logs': text}
        return value, text

    def pool(accelerator):
        return next(p for p in api('GET', '/hardware')['pools'] if p['accelerator'] == accelerator)

    def inspect_outputs(label, job, pvc, filenames):
        """Read files from a fresh container after the training container exits."""
        state = status(job)
        assert state['job_state'] == 'Success', state
        name = run + '-read-' + label
        commands = [f'test -s /results/{job}/{file}' for file in filenames]
        commands += [f'find /results/{job} -type f -exec sha256sum {{}} \\;', f'cat /results/{job}/results.json']
        pod = {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': name, 'namespace': 'mist'},
            'spec': {'restartPolicy': 'Never', 'automountServiceAccountToken': False,
                'nodeSelector': {'kubernetes.io/hostname': state['node']},
                'containers': [{'name': 'inspect', 'image': 'busybox:1.36', 'imagePullPolicy': 'IfNotPresent',
                    'command': ['sh', '-c', ' && '.join(commands)],
                    'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                        'capabilities': {'drop': ['ALL']}},
                    'resources': {'requests': {'cpu': '10m', 'memory': '16Mi'}, 'limits': {'cpu': '100m', 'memory': '32Mi'}},
                    'volumeMounts': [{'name': 'results', 'mountPath': '/results', 'readOnly': True}]}],
                'volumes': [{'name': 'results', 'persistentVolumeClaim': {'claimName': pvc, 'readOnly': True}}]}}
        kubectl('create', '-f', '-', body=pod)
        try:
            def done():
                value = json.loads(kubectl('-n', 'mist', 'get', 'pod', name, '-o', 'json'))
                phase = value['status'].get('phase')
                if phase in ('Succeeded', 'Failed'):
                    assert phase == 'Succeeded', value['status']
                    return value
            wait(done, 'saved outputs readable on ' + state['node'], timeout=60)
            text = kubectl('-n', 'mist', 'logs', name)
            lines = text.splitlines()
            start = next(i for i, line in enumerate(lines) if line.startswith(('{', '[')))
            results = json.loads('\n'.join(lines[start:]))
            if label == 'nvidia':
                assert len(results['results']) == 2
                assert all(result['final_loss'] < 0.001 for result in results['results'])
            if label == 'tenstorrent':
                assert len(results) == 4 and all(result['validation_mse'] < 5e-4 for result in results)
                assert sum('/tenstorrent-' in line and '.pt' in line for line in lines[:start]) == 4
            (args.artifacts / (label + '-persisted-files.txt')).write_text(text)
            evidence['checks']['persisted-' + label] = {'job_id': job, 'pvc': pvc, 'node': state['node'], 'files': filenames, 'sha256_and_results': text}
        finally:
            kubectl('-n', 'mist', 'delete', 'pod', name, '--wait=false')

    try:
        before = api('GET', '/jobs')['count']
        for request in [
            {'image': 'unapproved/image:latest'},
            {'command': ['true'], 'accelerator': 'nvidia', 'device_count': 3},
            {'command': ['true'], 'accelerator': 'tenstorrent', 'device_count': 5},
            {'command': ['true'], 'privileged': True},
            {'command': ['true'], 'working_directory': 'relative'},
            {'command': ['true'], 'env': {'MIST_OUTPUT_DIR': '/override'}},
        ]:
            api('POST', '/jobs', request, expected=400)
        assert api('GET', '/jobs')['count'] == before
        evidence['checks']['invalid_requests_rejected_without_jobs'] = True

        command = "import json,os,sys; from pathlib import Path; assert os.getcwd()=='/tmp'; assert sys.argv[1:]==['one argument with spaces','literal;argument']; p=Path(os.environ['MIST_OUTPUT_DIR']); (p/'results.json').write_text(json.dumps({'argv':sys.argv[1:],'cwd':os.getcwd()})); assert (Path(os.environ['MIST_CHECKPOINT_DIR'])/'results.json').read_text()==(p/'results.json').read_text(); print('COMMAND_ARGUMENTS_OUTPUT_ALIAS_PASSED',flush=True)"
        literal = submit('literal-arguments', image='mist-training:cpu-v1', command=['python', '-c', command],
            args=['one argument with spaces', 'literal;argument'], working_directory='/tmp')
        _, text = finished(literal)
        assert 'COMMAND_ARGUMENTS_OUTPUT_ALIAS_PASSED' in text
        inspect_outputs('arguments', literal, 'mist-cpu-checkpoints', ['results.json'])

        for accelerator, total in [('nvidia', 2), ('tenstorrent', 4)]:
            wait(lambda: pool(accelerator)['available'] == total, accelerator + ' idle')
            script = "import time\nfrom pathlib import Path\n"
            if accelerator == 'nvidia':
                script += "import torch\nassert torch.cuda.is_available() and torch.cuda.device_count()==2\n"
            else:
                script += "assert len(list(Path('/dev/tenstorrent').glob('[0-9]*')))==4\n"
            script += "print('DEVICE_HOLDER_READY',flush=True)\ntime.sleep(180)\n"
            holder = submit(accelerator + '-holder', accelerator=accelerator, device_count=total, script=script, timeout_seconds=300)
            holders.add(holder)
            wait(lambda: 'DEVICE_HOLDER_READY' in logs(holder), accelerator + ' whole-machine reservation started')
            snapshot = pool(accelerator)
            assert snapshot['allocated'] == total and snapshot['available'] == 0, snapshot
            if accelerator == 'tenstorrent':
                assert len(status(holder).get('devices', [])) == 4
            follower_request = {'accelerator': accelerator, 'device_count': 1}
            if accelerator == 'nvidia':
                follower_request.update(image='mist-training:nvidia-v1', args=['--backend', 'nvidia', '--devices', '1'])
            else:
                follower_request.update(type='training-smoke')
            follower = submit(accelerator + '-queued', **follower_request)
            def queued():
                value = status(follower)
                if value['job_state'] == 'Scheduled' and value.get('message'):
                    reason = 'cannot allocate' if accelerator == 'tenstorrent' else 'Insufficient nvidia.com/gpu'
                    assert reason in value['message'] and not value.get('node'), value
                    return value
            queue = wait(queued, accelerator + ' queued behind holder', timeout=60)
            assert api('POST', '/jobs/' + holder + '/cancel')['job_state'] == 'Cancelled'
            assert api('POST', '/jobs/' + holder + '/cancel')['job_state'] == 'Cancelled'
            holders.discard(holder)
            _, text = finished(holder, 'Cancelled')
            assert 'DEVICE_HOLDER_READY' in text
            _, text = finished(follower)
            marker = 'TRAINING_PASSED backend=tenstorrent devices=2' if accelerator == 'tenstorrent' else 'PACKAGED_TRAINING_PASSED backend=nvidia'
            assert marker in text
            wait(lambda: pool(accelerator)['available'] == total, accelerator + ' devices released', timeout=45)
            evidence['checks'][accelerator + '-queue-cancel-reuse'] = {'occupied': snapshot, 'queued': queue, 'idle': pool(accelerator)}
            print(accelerator + ': occupied/free counts, queue, cancellation, retained logs and automatic reuse passed', flush=True)

        browser = json.loads((args.artifacts / 'browser.json').read_text())
        assert 'error' not in browser and 'completed_at' in browser, 'Browser checks must finish first'
        inspect_outputs('cpu', browser['jobs']['packaged-cpu']['id'], 'mist-cpu-checkpoints', ['model.json', 'results.json'])
        inspect_outputs('nvidia', browser['jobs']['packaged-two-gpus']['id'], 'training-nvidia-checkpoints', ['nvidia-0.pt', 'nvidia-1.pt', 'results.json'])
        tt_job = browser['jobs']['tt-two-boards']['id']
        inspect_outputs('tenstorrent', tt_job, 'training-tenstorrent-checkpoints', ['allocation.json', 'results.json'])
        evidence['hardware_after'] = api('GET', '/hardware')
        evidence['completed_at'] = datetime.now(timezone.utc).isoformat()
        print('FOUNDATION_ALLOCATION_OUTPUTS_PASSED', flush=True)
    except Exception as error:
        evidence['error'] = repr(error)
        raise
    finally:
        for holder in holders:
            try:
                api('POST', '/jobs/' + holder + '/cancel')
            except Exception as error:
                evidence['cleanup_error'] = repr(error)
        (args.artifacts / 'foundation-acceptance.json').write_text(json.dumps(evidence, indent=2) + '\n')


if __name__ == '__main__':
    main()
