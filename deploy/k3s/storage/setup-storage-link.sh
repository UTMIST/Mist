#!/usr/bin/env bash
# Run on each owning host as root. No gateway or cluster-interface changes.
set -euo pipefail
case "${1:-}" in
  main) iface=enp6s0; address=192.168.240.1/30 ;;
  quietbox) iface=enp200s0; address=192.168.240.2/30 ;;
  *) echo 'Usage: setup-storage-link.sh main|quietbox' >&2; exit 2 ;;
esac
[ "$(id -u)" -eq 0 ] || { echo 'Run as root on the owning host' >&2; exit 1; }
[ "$(cat "/sys/class/net/$iface/carrier")" = 1 ] || { echo "$iface has no cable link" >&2; exit 1; }
[ -z "$(ip -4 route show default dev "$iface")" ] || { echo 'Refusing to replace a default-route connection' >&2; exit 1; }
profile=mist-storage-direct
current=$(nmcli -g GENERAL.CONNECTION device show "$iface")
install -d -m 0700 /var/lib/mist
if [ -n "$current" ] && [ "$current" != '--' ] && [ "$current" != "$profile" ]; then
  uuid=$(nmcli -g connection.uuid connection show "$current")
  auto=$(nmcli -g connection.autoconnect connection show "$uuid")
  record=/var/lib/mist/storage-link-previous.txt
  if [ ! -f "$record" ]; then
    printf '%s\n%s\n%s\n' "$iface" "$uuid" "$auto" > "$record"
    chmod 0600 "$record"
  fi
  nmcli connection modify "$uuid" connection.autoconnect no
fi
if nmcli connection show "$profile" >/dev/null 2>&1; then
  [ "$(nmcli -g connection.interface-name connection show "$profile")" = "$iface" ] || exit 1
else
  nmcli connection add type ethernet ifname "$iface" con-name "$profile"
fi
nmcli connection modify "$profile" \
  connection.autoconnect yes connection.autoconnect-priority 100 \
  ipv4.method manual ipv4.addresses "$address" ipv4.gateway '' \
  ipv4.dns '' ipv4.never-default yes ipv6.method disabled
nmcli --wait 15 connection up "$profile"
ip -4 address show dev "$iface"
