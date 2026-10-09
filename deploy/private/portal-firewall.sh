#!/bin/bash
# Root, actual host network namespace. Replace only Mist's chains atomically;
# preserve all other firewall tables/chains and existing LAN access.
set -euo pipefail
portal_port=${MIST_PORTAL_PORT:-8088}
for family in iptables ip6tables; do
  restore="${family}-restore"
  "$family" -w -N MIST-TAIL-INPUT 2>/dev/null || true
  "$family" -w -N MIST-TAIL-FORWARD 2>/dev/null || true
  {
    printf '*filter\n-F MIST-TAIL-INPUT\n-F MIST-TAIL-FORWARD\n'
    if [ "$family" = iptables ]; then
      for trusted in 100.73.139.66 100.95.175.37 100.99.194.52; do
        printf -- '-A MIST-TAIL-INPUT -s %s/32 -j RETURN\n' "$trusted"
        printf -- '-A MIST-TAIL-FORWARD -s %s/32 -j RETURN\n' "$trusted"
      done
      if [ "$portal_port" != 0 ]; then
        printf -- '-A MIST-TAIL-INPUT -p tcp --dport %s -j RETURN\n' "$portal_port"
      fi
    else
      for trusted in fd7a:115c:a1e0::d401:8bda fd7a:115c:a1e0::f72a:af26 fd7a:115c:a1e0::2901:c2d7; do
        printf -- '-A MIST-TAIL-INPUT -s %s/128 -j RETURN\n' "$trusted"
        printf -- '-A MIST-TAIL-FORWARD -s %s/128 -j RETURN\n' "$trusted"
      done
    fi
    # No IPv6 portal listener is enabled; do not permit an alternate bypass.
    printf -- '-A MIST-TAIL-INPUT -j DROP\n-A MIST-TAIL-FORWARD -j DROP\nCOMMIT\n'
  } | "$restore" --wait 10 --noflush
  "$family" -w -C INPUT -i tailscale0 -j MIST-TAIL-INPUT 2>/dev/null || "$family" -w -I INPUT 1 -i tailscale0 -j MIST-TAIL-INPUT
  "$family" -w -C FORWARD -i tailscale0 -j MIST-TAIL-FORWARD 2>/dev/null || "$family" -w -I FORWARD 1 -i tailscale0 -j MIST-TAIL-FORWARD
done
