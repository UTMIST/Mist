#!/usr/bin/env python3
"""Prepare ordinary team directories and publish usage; never format or migrate.

Only bounded API-owned requests outside workload mounts are accepted. Allowances
are advisory. The filesystem's real free space is shared by all teams.
"""
import json
import os
from pathlib import Path
import re
import stat
import tempfile
import time

LAYOUT = 'shared-folders-v1'
TEAM_ID = re.compile(r'^team-[a-f0-9]{16}$')


class FolderStore:
    def __init__(self, root, uid=65532, gid=65532):
        self.root = Path(root)
        self.uid, self.gid = uid, gid
        marker = self.root / '.mist-storage-layout.json'
        if self.root.is_symlink() or marker.is_symlink():
            raise ValueError('storage root and layout marker must not be symlinks')
        if json.loads(marker.read_text()).get('layout') != LAYOUT:
            raise ValueError('shared-folder storage has not been initialized')
        self.device = self.root.stat().st_dev
        self.requests = self.directory('metadata/provision-requests', 0o700)
        self.status = self.directory('metadata/provision-status', 0o700)
        self.directory('teams', 0o755)

    def directory(self, relative, mode):
        path = self.root
        parts = Path(relative).parts
        if not parts or any(part in ('.', '..', '/') for part in parts):
            raise ValueError('invalid storage directory')
        for part in parts:
            path = path / part
            if not path.exists() and not path.is_symlink():
                path.mkdir(mode=0o750)
                os.chown(path, self.uid, self.gid)
            entry = path.lstat()
            if not stat.S_ISDIR(entry.st_mode) or entry.st_dev != self.device:
                raise ValueError('storage directories must be ordinary directories on the shared disk')
        os.chmod(path, mode)
        os.chown(path, self.uid, self.gid)
        return path

    def request(self, path):
        entry = path.lstat()
        if (not TEAM_ID.fullmatch(path.stem) or not stat.S_ISREG(entry.st_mode)
                or entry.st_uid != self.uid or entry.st_size > 1024):
            raise ValueError('request must be a bounded API-owned regular file')
        body = json.loads(path.read_text())
        if set(body) != {'id', 'storage_gib', 'model_storage_gib'} or body['id'] != path.stem:
            raise ValueError('invalid team request schema')
        for key in ('storage_gib', 'model_storage_gib'):
            if type(body[key]) is not int or not 1 <= body[key] <= 2**20:
                raise ValueError('invalid advisory allowance')
        return body

    @staticmethod
    def usage(path):
        """Logical regular-file bytes. Never follow workload-created symlinks."""
        total = 0
        device = path.stat().st_dev
        seen = set()
        def failed(error):
            if not isinstance(error, FileNotFoundError):
                raise error
        for _, _, files, descriptor in os.fwalk(path, follow_symlinks=False, onerror=failed):
            if os.fstat(descriptor).st_dev != device:
                raise ValueError('unexpected nested filesystem in team storage')
            for name in files:
                try:
                    info = os.stat(name, dir_fd=descriptor, follow_symlinks=False)
                except FileNotFoundError:
                    continue  # A live job may rename/delete files during sampling.
                if stat.S_ISREG(info.st_mode):
                    identity = (info.st_dev, info.st_ino)
                    if identity not in seen:
                        total += info.st_size
                        seen.add(identity)
        return total

    def provision(self, body, previous=None):
        team = body['id']
        if not TEAM_ID.fullmatch(team):
            raise ValueError('invalid team ID')
        datasets = self.directory(f'teams/{team}/datasets', 0o750)
        results = self.directory(f'teams/{team}/results', 0o750)
        for name in ('metadata', 'metadata/datasets', '.uploads'):
            self.directory(f'teams/{team}/datasets/{name}', 0o700)
        for pool in ('datasets', 'results'):
            for scope in ('common', 'members'):
                self.directory(f'teams/{team}/{pool}/{scope}', 0o755)
        usage = previous or {}
        if time.time() - usage.get('usage_observed_unix', 0) >= 30:
            usage = {'dataset_used_bytes': self.usage(datasets),
                     'model_used_bytes': self.usage(results),
                     'usage_observed_unix': int(time.time())}
        return {**body, 'state': 'Ready', 'layout': LAYOUT,
                **{key: usage[key] for key in
                   ('dataset_used_bytes', 'model_used_bytes', 'usage_observed_unix')}}

    def publish(self, team, value):
        path = self.status / f'{team}.json'
        serialized = json.dumps(value, sort_keys=True)
        if path.exists() and not path.is_symlink() and path.read_text() == serialized:
            return
        fd, temporary = tempfile.mkstemp(prefix='.mist-', dir=self.status)
        try:
            with os.fdopen(fd, 'w') as stream:
                stream.write(serialized)
                stream.flush()
                os.fsync(stream.fileno())
            os.chown(temporary, self.uid, self.gid)
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    def reconcile(self, cache):
        for path in sorted(self.requests.glob('team-*.json')):
            try:
                body = self.request(path)
            except Exception as error:
                print(f'Request rejected: {path.name}: {error}', flush=True)
                continue
            try:
                value = self.provision(body, cache.get(body['id']))
                cache[body['id']] = value
            except Exception as error:
                value = {'state': 'Error', 'layout': LAYOUT, 'error': str(error)}
                print(f"Provisioning failed: {body['id']}: {error}", flush=True)
            self.publish(body['id'], value)


def main():
    if os.geteuid() != 0:
        raise SystemExit('Run as root on the storage host')
    store = FolderStore(os.environ.get('MIST_STORAGE_ROOT', '/srv/mist-data'))
    cache = {}
    while True:
        store.reconcile(cache)
        time.sleep(2)


if __name__ == '__main__':
    main()
