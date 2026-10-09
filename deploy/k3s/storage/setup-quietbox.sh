#!/bin/bash
# Run as root in QuietBox's host mount namespace. Never formats a physical disk.
set -euo pipefail
image=/srv/mist-storage-volume.img
root=/srv/mist-storage
mkdir -p "$root"
if mountpoint -q "$root"; then
  [ -f "$image" ] && [ "$(blkid -s LABEL -o value "$image")" = mist-shared ]
  source=$(findmnt -n -o SOURCE --target "$root")
  [ "$(losetup -n -O BACK-FILE "$source")" = "$image" ]
else
  if [ -n "$(ls -A "$root")" ]; then echo 'Refusing to cover nonempty storage directory' >&2; exit 1; fi
  if [ ! -e "$image" ]; then
    (set -o noclobber; : > "$image")
    chmod 0600 "$image"
    truncate -s 100G "$image"
    mkfs.ext4 -m 1 -L mist-shared "$image"
  fi
  # Check this is our ext4 image before mounting an existing file.
  [ "$(blkid -s LABEL -o value "$image")" = mist-shared ]
  mount -o loop,nodev,nosuid "$image" "$root"
fi
if ! grep -q '^/srv/mist-storage-volume.img ' /etc/fstab; then
  echo '/srv/mist-storage-volume.img /srv/mist-storage ext4 loop,nodev,nosuid 0 0' >> /etc/fstab
fi
chown 65532:65532 "$root"
chmod 0750 "$root"
install -d -o 65532 -g 65532 -m 0700 "$root/metadata" "$root/metadata/datasets" "$root/.uploads"
install -d -o 65532 -g 65532 -m 0755 "$root/datasets" "$root/jobs" "$root/legacy"
mkdir -p /etc/exports.d /etc/nfs.conf.d
cat > /etc/exports.d/mist.exports <<'EXPORT'
/srv/mist-storage 10.0.0.175(rw,sync,crossmnt,no_subtree_check,root_squash,anonuid=65532,anongid=65532) 10.0.0.112(rw,sync,crossmnt,no_subtree_check,root_squash,anonuid=65532,anongid=65532)
EXPORT
cat > /etc/nfs.conf.d/mist.conf <<'CONF'
[nfsd]
vers3=n
vers4=y
CONF
mkdir -p /etc/systemd/system/nfs-server.service.d
printf '[Unit]\nRequiresMountsFor=/srv/mist-storage\n' > /etc/systemd/system/nfs-server.service.d/mist-storage.conf
systemctl daemon-reload
systemctl enable --now nfs-server
exportfs -ra
if ufw status | grep -q "Status: active"; then
  ufw allow from 10.0.0.175 to any port 2049 proto tcp comment mist-nfs
  ufw allow from 10.0.0.112 to any port 2049 proto tcp comment mist-nfs
fi
mountpoint "$root"
df -h "$root"
exportfs -v
