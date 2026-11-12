#!/usr/bin/env bash
set -euo pipefail

for path in apps/android/src/main/kotlin/com/zeusvpn/app services/control-plane/cmd/zeus-control-plane docs/decisions scripts .github/workflows gradle/wrapper; do
  [[ -d "$path" ]] || { echo "FAIL: missing directory: $path"; exit 1; }
done
for file in README.md settings.gradle.kts build.gradle.kts gradle/wrapper/gradle-wrapper.properties gradle/wrapper/gradle-wrapper.jar gradlew apps/android/build.gradle.kts apps/android/src/main/AndroidManifest.xml services/control-plane/go.mod services/control-plane/cmd/zeus-control-plane/main.go services/control-plane/cmd/zeus-control-plane/server.go services/control-plane/cmd/zeus-control-plane/server_test.go docs/offline-and-network-resilience.md docs/phase-1-plan.md; do
  [[ -s "$file" ]] || { echo "FAIL: missing or empty file: $file"; exit 1; }
done
[[ -x gradlew ]] || { echo "FAIL: gradlew is not executable"; exit 1; }
bash -n gradlew

if find . -type f \( -name "*.pem" -o -name "*.key" -o -name "*.p12" -o -name "*.pfx" -o -name "*.keystore" -o -name "*.jks" \) -not -path "./.git/*" | grep -q .; then
  echo "FAIL: possible private key or signing keystore found"
  exit 1
fi
if find . -type f -name ".env" -not -path "./.git/*" | grep -q .; then
  echo "FAIL: .env file found"
  exit 1
fi

echo "PASS: ZEUS Phase 1 repository structure is present"
echo "NOTE: this check is not an Android build or VPN connectivity test"
