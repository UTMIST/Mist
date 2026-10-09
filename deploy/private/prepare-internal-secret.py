#!/usr/bin/python3
"""Reuse the internal auth token on ordinary deployments. Never print its value."""
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess

env = {**os.environ, 'KUBECONFIG': os.environ.get('KUBECONFIG', '/home/utmist/.kube/config')}
command = ['k3s', 'kubectl']
token_file = Path('/home/utmist/.config/mist/internal-token')
existing = subprocess.run(command + ['get', 'secret', 'mist-internal-auth', '-n', 'mist-system', '-o', 'json'], env=env, capture_output=True, text=True)
if existing.returncode == 0:
    token = base64.b64decode(json.loads(existing.stdout)['data']['MIST_INTERNAL_TOKEN']).decode()
else:
    if 'NotFound' not in existing.stderr:
        raise SystemExit('Cannot inspect internal Secret; refusing to replace credentials')
    token = token_file.read_text() if token_file.exists() else secrets.token_urlsafe(48)
if len(token) < 32:
    raise SystemExit('Internal token is invalid')
token_file.parent.mkdir(parents=True, exist_ok=True)
token_file.write_text(token)
token_file.chmod(0o600)
body = {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': 'mist-internal-auth', 'namespace': 'mist-system'}, 'stringData': {'MIST_INTERNAL_TOKEN': token}}
result = subprocess.run(command + ['apply', '-f', '-'], input=json.dumps(body), env=env, capture_output=True, text=True)
if result.returncode:
    raise SystemExit('Cannot apply internal Secret')
print('Internal auth Secret ready; credentials preserved')
