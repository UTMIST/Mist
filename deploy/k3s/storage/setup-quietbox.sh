#!/usr/bin/env bash
# Initialize ordinary storage folders on QuietBox. Never formats/mounts a disk.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo 'Run as root on QuietBox' >&2; exit 1; }
root=/srv/mist-data
[ ! -L "$root" ] || { echo 'Refusing a symlink storage root' >&2; exit 1; }
if [ -d "$root" ] && [ -n "$(ls -A "$root")" ] && [ ! -f "$root/.mist-storage-layout.json" ]; then
  echo 'Refusing to adopt a nonempty unknown directory' >&2
  exit 1
fi
install -d -o 65532 -g 65532 -m 0750 "$root"
if [ -e "$root/.mist-storage-layout.json" ]; then
  python3 - "$root" <<'PY'
import json, sys
from pathlib import Path
marker = Path(sys.argv[1]) / '.mist-storage-layout.json'
assert not marker.is_symlink()
assert json.loads(marker.read_text())['layout'] == 'shared-folders-v1'
PY
else
  printf '{"layout":"shared-folders-v1"}\n' > "$root/.mist-storage-layout.json"
  chmod 0644 "$root/.mist-storage-layout.json"
fi
install -d -o 65532 -g 65532 -m 0700 "$root/metadata" "$root/metadata/provision-requests" "$root/metadata/provision-status"
install -d -o 65532 -g 65532 -m 0755 "$root/teams"
install -d /etc/exports.d /etc/nfs.conf.d
cat > /etc/exports.d/mist-folders.exports <<'EXPORT'
/srv/mist-data 192.168.240.1(rw,sync,no_subtree_check,root_squash,anonuid=65532,anongid=65532,fsid=8410) 192.168.240.2(rw,sync,no_subtree_check,root_squash,anonuid=65532,anongid=65532,fsid=8410)
EXPORT
cat > /etc/nfs.conf.d/mist.conf <<'CONF'
[nfsd]
vers3=n
vers4=y
CONF
systemctl enable --now nfs-server
exportfs -ra
if ufw status | grep -q 'Status: active'; then
  ufw allow in on enp200s0 from 192.168.240.1 to any port 2049 proto tcp comment mist-direct-nfs
fi
findmnt -T "$root"
df -h "$root"
