#!/usr/bin/python3
"""QuietBox host provisioning agent. No network listener or cluster credentials.

Only API-owned manifests outside all workload mounts request capacity. Filesystem
images enforce capacity on every writer, including jobs. Never shrink/delete or
format an existing unknown image; preserve mounts and original storage data.
"""
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
import time

ROOT = Path('/srv/mist-storage')
REQUESTS = ROOT / 'metadata/provision-requests'
STATUS = ROOT / 'metadata/provision-status'
IMAGES = ROOT / '.team-volumes'
PATTERN = re.compile(r'^team-[a-f0-9]{16}$')
GIB = 1024 ** 3


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()


def atomic(path, data, mode=0o600, owner=None):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix='.mist-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as output:
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.chmod(tmp, mode)
        if owner:
            os.chown(tmp, *owner)
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def manifest(path):
    if not PATTERN.fullmatch(path.stem) or path.is_symlink():
        raise ValueError('invalid manifest path')
    st = path.stat()
    if not stat.S_ISREG(st.st_mode) or st.st_uid != 65532 or st.st_size > 1024:
        raise ValueError('manifest must be a bounded API-owned regular file')
    body = json.loads(path.read_text())
    if set(body) != {'id', 'storage_gib'} or body['id'] != path.stem:
        raise ValueError('invalid manifest schema')
    size = body['storage_gib']
    if type(size) is not int or not 1 <= size <= 80:
        raise ValueError('storage must be 1–80GiB')
    return body


def provision(body):
    team, gib = body['id'], body['storage_gib']
    image, mount = IMAGES / (team + '.img'), ROOT / 'teams' / team
    label = 'mist-' + team[-10:]
    size = gib * GIB
    created = False
    if not image.exists():
        fd = os.open(image, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as file:
            file.truncate(size)
        run('mkfs.ext4', '-q', '-m', '1', '-L', label, str(image))
        created = True
    if image.is_symlink() or not stat.S_ISREG(image.stat().st_mode):
        raise ValueError('unknown image type')
    if run('blkid', '-s', 'LABEL', '-o', 'value', str(image)) != label:
        raise ValueError('refusing an unknown filesystem image')
    old_size = image.stat().st_size
    if old_size > size:
        raise ValueError('refusing to shrink storage')
    mount.mkdir(parents=True, exist_ok=True)
    mounted = subprocess.run(['mountpoint', '-q', str(mount)]).returncode == 0
    if mounted:
        device = run('findmnt', '-n', '-o', 'SOURCE', '--mountpoint', str(mount))
        if Path(run('losetup', '-n', '-O', 'BACK-FILE', device)) != image:
            raise ValueError('mount is backed by an unknown device')
    else:
        if any(mount.iterdir()):
            raise ValueError('refusing to cover a nonempty directory')
        run('mount', '-o', 'loop,nodev,nosuid', str(image), str(mount))
        device = run('findmnt', '-n', '-o', 'SOURCE', '--mountpoint', str(mount))
    if old_size < size:
        with image.open('r+b') as file:
            file.truncate(size)
        run('losetup', '-c', device)
    # Retry online growth even if the preceding run resized the image but
    # crashed before resizing ext4.
    filesystem_bytes = os.statvfs(mount).f_blocks * os.statvfs(mount).f_frsize
    if filesystem_bytes < size - 64 * 1024 ** 2:
        run('losetup', '-c', device)
        run('resize2fs', device)
    os.chown(mount, 65532, 65532)
    os.chmod(mount, 0o750)
    # The filesystem root and metadata stay outside selected workload subpaths.
    for name in ['metadata', 'metadata/datasets', '.uploads']:
        directory = mount / name
        directory.mkdir(parents=True, exist_ok=True)
        os.chown(directory, 65532, 65532)
        os.chmod(directory, 0o700)
    for name in ['datasets', 'jobs', 'legacy', 'common', 'members']:
        directory = mount / name
        directory.mkdir(exist_ok=True)
        os.chown(directory, 65532, 65532)
        os.chmod(directory, 0o755)
    fstab = Path('/etc/fstab')
    entry = f'{image} {mount} ext4 loop,nodev,nosuid,x-systemd.requires-mounts-for=/srv/mist-storage 0 0\n'
    contents = fstab.read_text()
    if not any(line.startswith(str(image) + ' ') for line in contents.splitlines()):
        with fstab.open('a') as file:
            file.write(entry)
    uuid = run('blkid', '-s', 'UUID', '-o', 'value', str(image))
    opts = f'rw,sync,no_subtree_check,root_squash,anonuid=65532,anongid=65532,fsid={uuid}'
    export = f'{mount} 10.0.0.175({opts}) 10.0.0.112({opts})\n'
    export_path = Path('/etc/exports.d') / (team + '.exports')
    changed = not export_path.exists() or export_path.read_text() != export
    if changed:
        atomic(export_path, export, 0o644)
    if changed or created:
        run('exportfs', '-ra')
    return {'state': 'Ready', 'storage_gib': gib, 'filesystem_uuid': uuid}


def main():
    if os.geteuid() != 0:
        raise SystemExit('Run as root in the host mount namespace')
    if subprocess.run(['mountpoint', '-q', str(ROOT)]).returncode:
        raise SystemExit('Base Mist storage is not mounted')
    for directory in [REQUESTS, STATUS]:
        directory.mkdir(parents=True, exist_ok=True)
        os.chown(directory, 65532, 65532)
        os.chmod(directory, 0o700)
    IMAGES.mkdir(exist_ok=True)
    os.chmod(IMAGES, 0o700)
    os.chown(IMAGES, 0, 0)
    (ROOT / 'teams').mkdir(exist_ok=True)
    os.chown(ROOT / 'teams', 65532, 65532)
    os.chmod(ROOT / 'teams', 0o755)
    # Ready records from a previous boot must not authorize an unmounted path.
    for path in STATUS.glob('team-*.json'):
        atomic(path, json.dumps({'state': 'Provisioning'}), 0o600, (65532, 65532))
    while True:
        desired = []
        for path in sorted(REQUESTS.glob('team-*.json')):
            try:
                desired.append(manifest(path))
            except Exception as error:
                print(f'Manifest rejected: {path.name}: {error}', flush=True)
        if sum(body['storage_gib'] for body in desired) > 80:
            print('Allocation budget exceeded; provisioning refused', flush=True)
            time.sleep(2)
            continue
        for body in desired:
            try:
                status = provision(body)
            except Exception as error:
                status = {'state': 'Error', 'storage_gib': body['storage_gib'], 'error': str(error)}
                print(f"Provisioning failed: {body['id']}: {error}", flush=True)
            path = STATUS / (body['id'] + '.json')
            serialized = json.dumps(status, sort_keys=True)
            if not path.exists() or path.read_text() != serialized:
                atomic(path, serialized, 0o600, (65532, 65532))
        time.sleep(2)


if __name__ == '__main__':
    main()
