#!/usr/bin/env bash
set -euo pipefail

# Compatibility entry point. The upstream 2.20.0 AAR targets API 29 and must
# not be downloaded for an app that still supports Android 7 / API 24.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$SCRIPT_DIR/build-hev-aar-api24.sh"
