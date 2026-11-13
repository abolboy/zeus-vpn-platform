# ZEUS VPN Platform

Android-first hybrid VPN platform: Android management app, Linux VPN nodes, and a secure control plane.

Current stage: Phase 0 scaffold on main; Phase 1 development is isolated on phase1-foundation. The Android client now has an embedded WireGuard backend and local encrypted profile flow. A reachable server profile, handshake verification, production server provisioning, authentication, and operations API are still required for a complete service.

## Layout
- apps/android — Android management app (Kotlin, Jetpack Compose).
- services/control-plane — Go control API foundation.
- services/vpn-node — isolated WireGuard peer executor foundation (dry-run by default); server-side node mTLS and command queue endpoints are implemented, while the standalone node poller remains in development.
- packages/protocols — planned versioned API and event contracts.
- infra — planned deployment templates.
- docs — architecture, security baseline, decisions, and delivery plans.

## Offline-first requirements
The app shell and locally saved preferences remain accessible without internet. The Android app embeds the official WireGuard userspace tunnel library. User-supplied WireGuard profiles are parsed before use and stored with AES-GCM using a key held in Android Keystore, under noBackupFilesDir. Private keys must never be committed to GitHub or printed in logs. The control plane must not be a single point of failure for an established data-plane tunnel.

A VPN tunnel still needs a usable network route to a reachable endpoint. No app can guarantee connectivity when all paths are unavailable or blocked; constrained-network support will be measured against a defined test matrix.

## Build and test
    ./gradlew :apps:android:assembleDebug :apps:android:testDebugUnitTest
    cd services/control-plane && go test -race ./... && go vet ./...
    cd ../vpn-node && go test -race ./... && go vet ./...
    bash scripts/verify-repository.sh

See docs/phase-1-plan.md, docs/offline-and-network-resilience.md, and docs/node-agent-protocol.md.
