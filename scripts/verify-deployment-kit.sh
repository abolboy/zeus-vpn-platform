#!/usr/bin/env bash
set -euo pipefail

required_files=(
  "deploy/systemd/zeus-control-plane.service"
  "deploy/systemd/zeus-migrate.service"
  "deploy/systemd/zeus-vpn-node.service"
  "deploy/examples/control-plane.env.example"
  "deploy/examples/vpn-node.env.example"
  "docs/staging-deployment.md"
)

for file in "${required_files[@]}"; do
  [[ -s "$file" ]] || { echo "FAIL: missing or empty deployment file: $file"; exit 1; }
done

grep -q '^User=zeus$' deploy/systemd/zeus-control-plane.service
grep -q '^ProtectSystem=strict$' deploy/systemd/zeus-control-plane.service
grep -q '^Type=oneshot$' deploy/systemd/zeus-migrate.service
grep -q '^ZEUS_HTTP_ADDR=127.0.0.1:8080$' deploy/examples/control-plane.env.example
grep -q '^ZEUS_NODE_HTTP_ADDR=0.0.0.0:8443$' deploy/examples/control-plane.env.example
grep -q '^ZEUS_NODE_ALLOW_MUTATIONS=false$' deploy/examples/vpn-node.env.example
grep -q '^CapabilityBoundingSet=CAP_NET_ADMIN$' deploy/systemd/zeus-vpn-node.service
grep -q '^ReadWritePaths=/etc/wireguard$' deploy/systemd/zeus-vpn-node.service

if grep -R -E -- '-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|AKIA[0-9A-Z]{16}' deploy; then
  echo "FAIL: private key material or AWS access key-like value found in deployment kit"
  exit 1
fi

if command -v systemd-analyze >/dev/null 2>&1; then
  temp_root="$(mktemp -d)"
  trap 'rm -rf "$temp_root"' EXIT
  mkdir -p "$temp_root/etc/zeus" "$temp_root/etc/systemd/system" "$temp_root/etc/wireguard" "$temp_root/opt/zeus/bin"
  : > "$temp_root/etc/zeus/control-plane.env"
  : > "$temp_root/etc/zeus/vpn-node.env"
  : > "$temp_root/opt/zeus/bin/zeus-control-plane"
  : > "$temp_root/opt/zeus/bin/zeus-migrate"
  : > "$temp_root/opt/zeus/bin/zeus-vpn-node"
  chmod 0755 "$temp_root/opt/zeus/bin/zeus-control-plane" "$temp_root/opt/zeus/bin/zeus-migrate" "$temp_root/opt/zeus/bin/zeus-vpn-node"
  for target in sysinit.target network-online.target multi-user.target; do
    printf '[Unit]\nDescription=%s\n' "$target" > "$temp_root/etc/systemd/system/$target"
  done
  cp deploy/systemd/zeus-control-plane.service deploy/systemd/zeus-migrate.service deploy/systemd/zeus-vpn-node.service "$temp_root/etc/systemd/system/"
  systemd-analyze verify --root="$temp_root" \
    "$temp_root/etc/systemd/system/zeus-control-plane.service" \
    "$temp_root/etc/systemd/system/zeus-migrate.service" \
    "$temp_root/etc/systemd/system/zeus-vpn-node.service"
  echo "PASS: systemd-analyze accepted all three service units"
else
  echo "SKIP: systemd-analyze is unavailable; service syntax was not checked on this runner"
fi

echo "PASS: ZEUS staging deployment templates and guardrails are present"
echo "NOTE: static template checks do not validate a live host, certificate, firewall, or database"
