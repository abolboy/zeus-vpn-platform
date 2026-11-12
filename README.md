# ZEUS VPN Platform

Android-first hybrid VPN platform: Android management app, Linux VPN nodes, and a secure control plane.

Current stage: Phase 0 scaffold on main; Phase 1 foundation is being developed on phase1-foundation. This is not yet a functioning VPN service.

## Layout
- apps/android — Android management app (Kotlin, Jetpack Compose).
- services/control-plane — Go control API foundation.
- services/vpn-node — planned Linux VPN node agent and engine integration.
- packages/protocols — planned versioned API and event contracts.
- infra — planned deployment templates.
- docs — architecture, security baseline, decisions, and delivery plans.

## Offline-first requirements
The app shell and locally saved non-secret preferences remain accessible without internet. The control plane must not be a single point of failure for an established data-plane tunnel. Private profile material will be protected with Android Keystore-backed storage before profile caching is enabled.

A VPN tunnel still needs a usable network route to a reachable endpoint. No app can guarantee connectivity when all paths are unavailable or blocked; constrained-network support will be measured against a defined test matrix.

## Build and test
    ./gradlew :apps:android:assembleDebug :apps:android:testDebugUnitTest
    cd services/control-plane && go test ./... && go vet ./...
    bash scripts/verify-repository.sh

See docs/phase-1-plan.md and docs/offline-and-network-resilience.md.
