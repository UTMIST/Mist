#!/bin/bash
# Run as root in the main node's host mount namespace, after npm run build.
set -euo pipefail
repo=${1:-/home/utmist/Mist}
release=/srv/mist-web/releases/$(date -u +%Y%m%dT%H%M%S)
install -d -m 0755 "$release"
cp -r "$repo/web-interface/dist/." "$release/"
chmod -R a+rX "$release"
ln -s "$release" /srv/mist-web/current.new
mv -Tf /srv/mist-web/current.new /srv/mist-web/current
install -m 0644 "$repo/deploy/private/nginx.conf" /etc/nginx/conf.d/mist.conf
install -d -m 0755 /etc/systemd/system/nginx.service.d
install -m 0644 "$repo/deploy/private/nginx-mist-startup.conf" /etc/systemd/system/nginx.service.d/mist.conf
systemctl daemon-reload
nginx -t
systemctl enable nginx
systemctl reload nginx
