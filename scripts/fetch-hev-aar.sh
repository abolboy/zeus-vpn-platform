#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AAR_DIR="$ROOT_DIR/apps/android/libs"
AAR_PATH="$AAR_DIR/hev-socks5-tunnel-2.20.0.aar"
AAR_URL="https://github.com/heiher/hev-socks5-tunnel/releases/download/2.20.0/hev-socks5-tunnel.aar"
EXPECTED_SHA256="ae8a2e96b80b4eccccd037805a4bb4a278090c89dbcae8bc6fcb7634e9a255b9"

mkdir -p "$AAR_DIR"

verify_sha256() {
  local file="$1"
  local actual
  actual="$(sha256sum "$file" | awk '{print $1}')"
  if [[ "$actual" != "$EXPECTED_SHA256" ]]; then
    echo "Hev AAR checksum mismatch: expected $EXPECTED_SHA256, got $actual" >&2
    return 1
  fi
}

if [[ -f "$AAR_PATH" ]]; then
  verify_sha256 "$AAR_PATH"
  echo "Verified existing Hev AAR: $AAR_PATH"
  exit 0
fi

tmp_path="$(mktemp "$AAR_DIR/.hev-socks5-tunnel.XXXXXX")"
trap 'rm -f "$tmp_path"' EXIT

curl --fail --location --retry 3 --connect-timeout 15 --max-time 180 \
  "$AAR_URL" --output "$tmp_path"

verify_sha256 "$tmp_path"
mv "$tmp_path" "$AAR_PATH"
trap - EXIT

echo "Downloaded and verified Hev AAR 2.20.0: $AAR_PATH"
