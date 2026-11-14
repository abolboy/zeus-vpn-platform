#!/usr/bin/env bash
# Read-only WireGuard egress diagnostics. This script never changes sysctl,
# WireGuard, firewall, routing, or cloud security-group settings.
set -uo pipefail

WG_IFACE="${WG_IFACE:-wg0}"
WG_UPLINK_IFACE="${WG_UPLINK_IFACE:-}"
WG_CLIENT_IPV4_CIDR="${WG_CLIENT_IPV4_CIDR:-}"
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

if ! command -v ip >/dev/null 2>&1; then
  warn "iproute2 (ip) is unavailable; route and uplink checks are limited."
fi

if command -v wg >/dev/null 2>&1; then
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
else
  warn "wireguard-tools (wg) is not installed or not in PATH."
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
  DETECTED_OUT_IF="$(awk '{for (i=1; i<=NF; i++) if ($i == "dev") {print $(i+1); exit}}' <<<"$ROUTE")"
  if [[ -n "$WG_UPLINK_IFACE" ]]; then
    OUT_IF="$WG_UPLINK_IFACE"
    if ip link show dev "$OUT_IF" >/dev/null 2>&1; then
      info "Using explicitly supplied uplink: $OUT_IF"
    else
      warn "Configured uplink interface $OUT_IF does not exist."
    fi
  else
    OUT_IF="$DETECTED_OUT_IF"
  fi
  if [[ -n "${OUT_IF:-}" ]]; then
    ok "IPv4 uplink interface: $OUT_IF"
    info "$ROUTE"
  else
    OUT_IF=""
    warn "No usable IPv4 route to 1.1.1.1 or uplink interface was found."
  fi
else
  OUT_IF=""
fi

if [[ -z "$WG_CLIENT_IPV4_CIDR" ]] && command -v ip >/dev/null 2>&1; then
  WG_CLIENT_IPV4_CIDR="$(ip -4 route show dev "$WG_IFACE" scope link 2>/dev/null | awk '$1 ~ /^[0-9.]+\/[0-9]+$/ {print $1; exit}')"
  if [[ -n "$WG_CLIENT_IPV4_CIDR" ]]; then
    info "Inferred client subnet from the WireGuard interface route: $WG_CLIENT_IPV4_CIDR"
    info "For an exact check, set WG_CLIENT_IPV4_CIDR to the actual allocated client pool."
  fi
fi

if [[ -n "$WG_CLIENT_IPV4_CIDR" ]]; then
  info "Client IPv4 subnet used for NAT validation: $WG_CLIENT_IPV4_CIDR"
else
  warn "Client IPv4 subnet is unknown; exact source-subnet NAT validation cannot be completed. Set WG_CLIENT_IPV4_CIDR."
fi

printf '\nNAT and forwarding rules:\n'
IPTABLES_NAT_MATCH=""
IPTABLES_FORWARD_OUT=""
IPTABLES_FORWARD_RETURN=""
FORWARD_POLICY=""
NFT_RULES=""

if command -v iptables >/dev/null 2>&1; then
  NAT_RULES="$(iptables -t nat -S POSTROUTING 2>/dev/null || true)"
  FORWARD_RULES="$(iptables -S FORWARD 2>/dev/null || true)"
  if [[ -n "$WG_CLIENT_IPV4_CIDR" && -n "$OUT_IF" ]]; then
    IPTABLES_NAT_MATCH="$(grep -F -- "-s $WG_CLIENT_IPV4_CIDR " <<<"$NAT_RULES" | grep -F -- "-o $OUT_IF " | grep -Ei -- '-j (MASQUERADE|SNAT)( |$)' || true)"
    IPTABLES_FORWARD_OUT="$(grep -F -- "-i $WG_IFACE " <<<"$FORWARD_RULES" | grep -F -- "-o $OUT_IF " | grep -E -- '-j ACCEPT( |$)' || true)"
    IPTABLES_FORWARD_RETURN="$(grep -F -- "-i $OUT_IF " <<<"$FORWARD_RULES" | grep -F -- "-o $WG_IFACE " | grep -E -- 'ESTABLISHED|RELATED' | grep -E -- '-j ACCEPT( |$)' || true)"
  fi
  FORWARD_POLICY="$(awk '$1 == "-P" && $2 == "FORWARD" {print $3; exit}' <<<"$FORWARD_RULES")"
  if [[ -n "$NAT_RULES" ]]; then
    printf '%s\n' "$NAT_RULES" | sed 's/^/  /'
  else
    info "iptables returned no POSTROUTING rules."
  fi
  if [[ -n "$FORWARD_RULES" ]]; then
    printf '%s\n' "$FORWARD_RULES" | sed 's/^/  /'
  else
    info "iptables returned no FORWARD rules."
  fi
else
  info "iptables is unavailable; checking nftables where possible."
fi

if command -v nft >/dev/null 2>&1; then
  NFT_RULES="$(nft list ruleset 2>/dev/null || true)"
  if [[ -n "$WG_CLIENT_IPV4_CIDR" && -n "$OUT_IF" ]]; then
    NFT_NAT_MATCH="$(grep -F -- "$WG_CLIENT_IPV4_CIDR" <<<"$NFT_RULES" | grep -F -- "$OUT_IF" | grep -Ei -- 'masquerade|snat' || true)"
    NFT_FORWARD_OUT="$(grep -F -- "$WG_IFACE" <<<"$NFT_RULES" | grep -F -- "$OUT_IF" | grep -Ei -- 'accept' || true)"
    NFT_FORWARD_RETURN="$(grep -F -- "$WG_IFACE" <<<"$NFT_RULES" | grep -F -- "$OUT_IF" | grep -Ei -- 'established|related' | grep -Ei -- 'accept' || true)"
  else
    NFT_NAT_MATCH=""
    NFT_FORWARD_OUT=""
    NFT_FORWARD_RETURN=""
  fi
  NFT_MATCHES="$(grep -Ei 'hook (forward|postrouting)|masquerade|snat' <<<"$NFT_RULES" | head -n 40 || true)"
  if [[ -n "$NFT_MATCHES" ]]; then
    info "Relevant nftables rules:"
    printf '%s\n' "$NFT_MATCHES" | sed 's/^/  /'
  fi
else
  NFT_NAT_MATCH=""
  NFT_FORWARD_OUT=""
  NFT_FORWARD_RETURN=""
fi

if [[ -n "$WG_CLIENT_IPV4_CIDR" && -n "$OUT_IF" ]]; then
  if [[ -n "$IPTABLES_NAT_MATCH" || -n "$NFT_NAT_MATCH" ]]; then
    ok "A NAT/SNAT rule matches both client subnet $WG_CLIENT_IPV4_CIDR and uplink $OUT_IF."
  else
    warn "No NAT/SNAT rule was found for client subnet $WG_CLIENT_IPV4_CIDR on uplink $OUT_IF. A generic MASQUERADE rule for another subnet is not sufficient."
  fi

  if [[ "$FORWARD_POLICY" == "ACCEPT" ]]; then
    ok "iptables FORWARD default policy is ACCEPT; also review nftables/UFW rules for later drops."
  elif [[ -n "$IPTABLES_FORWARD_OUT" && -n "$IPTABLES_FORWARD_RETURN" ]]; then
    ok "iptables has matching client-to-uplink and established return forwarding rules."
  elif [[ -n "$NFT_FORWARD_OUT" && -n "$NFT_FORWARD_RETURN" ]]; then
    ok "nftables shows matching forward and established return rules for the WireGuard/uplink interfaces."
  else
    warn "Could not verify both forward and established-return paths between $WG_IFACE and $OUT_IF."
  fi
else
  warn "NAT and forwarding rules cannot be checked exactly until the client subnet and uplink are known."
fi

if ! command -v iptables >/dev/null 2>&1 && ! command -v nft >/dev/null 2>&1; then
  warn "Neither iptables nor nft is available; firewall/NAT state was not inspected."
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
