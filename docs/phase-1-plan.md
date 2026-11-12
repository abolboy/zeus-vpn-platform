# Phase 1 — Android and Control Plane Foundation

## Implemented in phase1-foundation
- Android Gradle project scaffold and official Gradle Wrapper.
- Compose dashboard foundation with explicit messaging that the VPN engine is not implemented.
- Device network observer: online / limited / offline / unknown.
- DataStore-backed non-secret auto-reconnect preference. It is not wired to a tunnel engine yet.
- Go control-plane health, readiness, and metadata endpoints.
- Go tests and CI workflow for repository structure, Go, and Android debug build.
- Offline-first boundaries documented.

## Not implemented yet
- VPN tunnel engine or Android VpnService connection.
- Linux VPN-node provisioning or WireGuard integration.
- Account system, API authentication, database, payments, or subscriptions.
- Encrypted VPN profile cache or offline provisioning bundles.
- Actual automatic reconnection behavior.
- Multiple transport fallback.
- SAP integration; the requested term needs clarification before selecting a SAP product or interface.

## Exit criteria
1. GitHub Actions passes Go tests and Android debug build.
2. APK installs and launches on Android 13 / API 33 and a second supported API.
3. Network state is accurate on Wi-Fi, mobile data, captive portal, and airplane mode.
4. Local preferences survive force-stop and restart without internet access.
5. API health endpoints return expected JSON and security headers.
