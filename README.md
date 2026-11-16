# ZEUS VPN Platform

Android-first hybrid VPN platform with a Kotlin Android app, Linux WireGuard nodes, and a Go control plane.

## Layout
- apps/android — Android management app (Kotlin, Jetpack Compose).
- apps/web-admin — Persian-first operator dashboard.
- services/control-plane — Go API, PostgreSQL, operator auth, user/device lifecycle, peer command queue, and optional mTLS node command listener.
- services/vpn-node — mTLS client, command worker, and dry-run-first WireGuard peer executor.
- docs — architecture, security baseline, decisions, protocol contracts, and delivery plans.

## Offline-first requirements
The Android app shell and locally saved preferences remain usable offline. User-supplied WireGuard profiles are parsed before use and stored with AES-GCM using Android Keystore. Private keys must never be committed to GitHub or printed in logs. An established data-plane tunnel must not depend on a continuously reachable control plane.

A tunnel still requires a usable network route to a reachable endpoint. No app can guarantee connectivity when all network paths are blocked or unavailable; constrained-network support is measured against a defined test matrix.

## Build and test

    ./gradlew :apps:android:assembleDebug :apps:android:testDebugUnitTest
    cd services/control-plane && go test -race ./... && go vet ./...
    cd ../vpn-node && go test -race ./... && go vet ./...
    bash scripts/verify-repository.sh

See docs/phase-1-plan.md, docs/offline-and-network-resilience.md, docs/phase-2-plan.md, docs/node-agent-protocol.md, and docs/staging-deployment.md for the isolated staging runbook. Hardened systemd units and non-secret environment templates are under deploy/.

## Production boundary

Device registration requires an online, mTLS-registered node with explicit address pools and interface IPs. Import pre-existing WireGuard peers before enabling a node for allocation. The mTLS listener is disabled unless its dedicated certificate and CA files are configured. The node process defaults to dry-run, and its mutation flag must be explicitly enabled. No production node, AWS resource, firewall, or WireGuard peer has been changed by this branch.
