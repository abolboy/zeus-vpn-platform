#!/usr/bin/env bash
# Read-only WireGuard egress diagnostics. This script never changes sysctl,
# WireGuard, firewall, routing, or cloud security-group settings.
set -uo pipefail

WG_IFACE="${WG_IFACE:-wg0}"
WARNINGS=0

ok()   { printf '[OK]   %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*"; WARNINGS=$((WARNINGS + 1)); }
info() { printf '[INFO] %s\n' "$*"; }

printf 'ZEUS WireGuard node diagnostics\n'
printf 'Interface: %s\n' "$WG_IFACE"
printf 'Mode: READ ONLY (no settings will be changed)\n\n'

if [[ $EUID -ne 0 ]]; then
  warn "Run with sudo so interface, firewall, and sysctl state can be read completely."
fi

if ! command -v wg >/dev/null 2>&1; then
  warn "wireguard-tools (wg) is not installed or not in PATH."
else
  if wg show "$WG_IFACE" >/dev/null 2>&1; then
    ok "WireGuard interface $WG_IFACE exists."
    info "Listen port: $(wg show "$WG_IFACE" listen-port 2>/dev/null || printf 'unknown')"
    PEER_COUNT="$(wg show "$WG_IFACE" peers 2>/dev/null | wc -l | tr -d ' ')"
    info "Configured peers: ${PEER_COUNT:-0}"
    NOW="$(date +%s)"
    printf '\nPeer handshake age and transfer counters (keys are redacted):\n'
    wg show "$WG_IFACE" latest-handshakes 2>/dev/null | while read -r key stamp; do
      [[ -n "${key:-}" ]] || continue
      if [[ "${stamp:-0}" =~ ^[0-9]+$ ]] && (( stamp > 0 )); then
        AGE=$((NOW - stamp))
        printf '  peer %s… handshake %ss ago\n' "${key:0:7}" "$AGE"
      else
        printf '  peer %s… no successful handshake recorded\n' "${key:0:7}"
      fi
    done
    wg show "$WG_IFACE" transfer 2>/dev/null | while read -r key rx tx; do
      [[ -n "${key:-}" ]] || continue
      printf '  peer %s… received=%s bytes sent=%s bytes\n' "${key:0:7}" "${rx:-0}" "${tx:-0}"
    done
  else
    warn "Cannot read interface $WG_IFACE. Check the interface name and wg-quick/service state."
  fi
fi

printf '\nIPv4 forwarding:\n'
IPV4_FORWARD="$(sysctl -n net.ipv4.ip_forward 2>/dev/null || true)"
if [[ "$IPV4_FORWARD" == "1" ]]; then
  ok "net.ipv4.ip_forward=1"
else
  warn "net.ipv4.ip_forward is not 1; routed client IPv4 traffic cannot be forwarded."
fi

printf '\nIPv4 uplink route:\n'
if command -v ip >/dev/null 2>&1; then
  ROUTE="$(ip -4 route get 1.1.1.1 2>/dev/null | head -n 1)"
  OUT_IF="$(awk '{for (i=1; i<=NF; i++) if ($i == "dev") {print $(i+1); exit}}' <<<"$ROUTE")"
  if [[ -n "$OUT_IF" ]]; then
    ok "Default path to 1.1.1.1 uses interface $OUT_IF."
    info "$ROUTE"
  else
    warn "No usable IPv4 route to 1.1.1.1 was found."
  fi
else
  OUT_IF=""
  warn "iproute2 (ip) is unavailable."
fi

printf '\nNAT and forwarding rules:\n'
if command -v iptables >/dev/null 2>&1; then
  NAT_RULES="$(iptables -t nat -S POSTROUTING 2>/dev/null | grep -E -- '-j (MASQUERADE|SNAT)' || true)"
  if [[ -n "$NAT_RULES" ]]; then
    ok "One or more IPv4 SNAT/MASQUERADE rules exist:"
    printf '%s\n' "$NAT_RULES" | sed 's/^/  /'
    if [[ -n "$OUT_IF" ]] && ! grep -F -- "-o $OUT_IF " <<<"$NAT_RULES" >/dev/null; then
      warn "No shown NAT rule explicitly targets the detected uplink $OUT_IF; verify the client subnet and uplink match."
    fi
  else
    warn "No IPv4 SNAT/MASQUERADE rule was found in POSTROUTING."
  fi
  FORWARD_RULES="$(iptables -S FORWARD 2>/dev/null | grep -E -- '(^-P FORWARD|wg|ACCEPT|DROP)' || true)"
  if [[ -n "$FORWARD_RULES" ]]; then
    printf '%s\n' "$FORWARD_RULES" | sed 's/^/  /'
  else
    warn "Could not read IPv4 FORWARD rules."
  fi
else
  warn "iptables is unavailable; inspect the nftables ruleset manually."
fi

if command -v nft >/dev/null 2>&1; then
  NFT_MATCHES="$(nft list ruleset 2>/dev/null | grep -Ei 'hook (forward|postrouting)|masquerade|snat' | head -n 30 || true)"
  if [[ -n "$NFT_MATCHES" ]]; then
    info "Relevant nftables rules:"
    printf '%s\n' "$NFT_MATCHES" | sed 's/^/  /'
  fi
fi

printf '\nIPv6 forwarding and route:\n'
IPV6_FORWARD="$(sysctl -n net.ipv6.conf.all.forwarding 2>/dev/null || true)"
if [[ "$IPV6_FORWARD" == "1" ]]; then
  ok "net.ipv6.conf.all.forwarding=1"
else
  warn "IPv6 forwarding is not enabled. If client AllowedIPs contains ::/0, IPv6 traffic may fail unless the server has a separately routed IPv6 design."
fi
IPV6_ROUTE="$(ip -6 route show default 2>/dev/null | head -n 1 || true)"
if [[ -n "$IPV6_ROUTE" ]]; then
  info "IPv6 default route: $IPV6_ROUTE"
else
  warn "No IPv6 default route is configured on this server."
fi

printf '\nServer HTTPS egress test:\n'
if command -v curl >/dev/null 2>&1; then
  HTTP_CODE="$(curl -4sS -o /dev/null -w '%{http_code}' --connect-timeout 4 --max-time 8 https://connectivitycheck.gstatic.com/generate_204 2>/dev/null || true)"
  if [[ "$HTTP_CODE" == "204" ]]; then
    ok "Server can reach the HTTPS connectivity test (HTTP 204)."
  else
    warn "Server HTTPS test did not return 204 (result: ${HTTP_CODE:-no response})."
  fi
else
  warn "curl is unavailable; server HTTPS egress was not tested."
fi

printf '\nCloud controls not visible from this guest OS:\n'
info "For AWS EC2, separately verify the instance Source/Destination Check is disabled."
info "Verify the Security Group allows the WireGuard UDP listen port and suitable egress."
info "Verify Network ACL return traffic and the route table/NAT path."

printf '\nSummary: %s warning(s). This is a diagnostic report, not a repair operation.\n' "$WARNINGS"
exit 0
