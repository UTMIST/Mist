#!/bin/bash
# Root in the host namespace. Exercise the installed enforcement chains with
# an isolated, simulated untrusted Tailnet source; no real peer is impersonated.
set -euo pipefail
namespace=mist-firewall-check
source_ip=100.127.254.253
if ip netns list | awk '{print $1}' | grep -qx "$namespace"; then
  echo 'Verification namespace already exists; refusing to replace it' >&2; exit 1
fi
if ip route show "$source_ip/32" | grep -q .; then
  echo 'Verification source already routed; refusing to replace it' >&2; exit 1
fi
iptables -S MIST-TAIL-INPUT >/dev/null
cleanup() {
  iptables -w -D INPUT -i mist-fw-host -j MIST-TAIL-INPUT 2>/dev/null || true
  iptables -w -D INPUT -i mist-fw-host -j ACCEPT 2>/dev/null || true
  iptables -w -D FORWARD -i mist-fw-host -j MIST-TAIL-FORWARD 2>/dev/null || true
  ip route del "$source_ip/32" dev mist-fw-host 2>/dev/null || true
  ip netns del "$namespace" 2>/dev/null || true
  ip link del mist-fw-host 2>/dev/null || true
}
trap cleanup EXIT
ip netns add "$namespace"
ip link add mist-fw-host type veth peer name mist-fw-guest
ip link set mist-fw-guest netns "$namespace"
ip addr add 198.18.0.1/30 dev mist-fw-host
ip link set mist-fw-host up
ip route add "$source_ip/32" dev mist-fw-host
ip netns exec "$namespace" ip addr add 198.18.0.2/30 dev mist-fw-guest
ip netns exec "$namespace" ip addr add "$source_ip/32" dev lo
ip netns exec "$namespace" ip link set lo up
ip netns exec "$namespace" ip link set mist-fw-guest up
ip netns exec "$namespace" ip route add default via 198.18.0.1
# Emulate the real tailscale0 dispatch with a test-only interface. Accept after
# RETURN avoids Tailscale's deliberate rejection of 100/10 on non-tail devices.
iptables -w -I INPUT 1 -i mist-fw-host -j ACCEPT
iptables -w -I INPUT 1 -i mist-fw-host -j MIST-TAIL-INPUT
iptables -w -I FORWARD 1 -i mist-fw-host -j MIST-TAIL-FORWARD
code=$(ip netns exec "$namespace" curl --interface "$source_ip" --max-time 4 -sS -o /dev/null -w '%{http_code}' http://100.73.139.66:8088/api/session)
[ "$code" = 200 ]
echo 'PASS untrusted source reaches portal'
for target in 100.73.139.66:22 100.73.139.66:6443 100.73.139.66:30491 10.43.1.116:3000; do
  if ! ip netns exec "$namespace" python3 -c 'import socket,sys; host,port=sys.argv[1].split(":"); s=socket.socket(); s.settimeout(2); s.bind((sys.argv[2],0))
try:
 s.connect((host,int(port)))
except OSError:
 sys.exit(0)
sys.exit(1)' "$target" "$source_ip"; then
    echo "FAIL untrusted source reached $target" >&2; exit 1
  fi
  echo "PASS untrusted source denied $target"
done
