#!/usr/bin/env bash
set -euo pipefail
required=("apps/android" "services/control-plane" "services/vpn-node" "packages/protocols" "infra" "docs" "docs/decisions" "scripts" ".github/workflows")
for path in "${required[@]}"; do
  if [[ ! -d "$path" ]]; then echo "FAIL: missing directory: $path"; exit 1; fi
done
for file in "README.md" "docs/architecture.md" "docs/phase-0-checklist.md" "docs/security-baseline.md" "docs/decisions/ADR-0001-platform-shape.md"; do
  if [[ ! -s "$file" ]]; then echo "FAIL: missing or empty file: $file"; exit 1; fi
done
if find . -type f \( -name "*.pem" -o -name "*.key" -o -name "*.p12" -o -name "*.pfx" -o -name "*.keystore" -o -name "*.jks" \) -not -path "./.git/*" | grep -q .; then
  echo "FAIL: possible private key or signing keystore found in repository tree"; exit 1
fi
if find . -type f -name ".env" -not -path "./.git/*" | grep -q .; then
  echo "FAIL: .env file found; use a non-secret .env.example template instead"; exit 1
fi
echo "PASS: required repository structure and documentation are present"
echo "NOTE: this check does not build Android/Go code or test VPN connectivity"
