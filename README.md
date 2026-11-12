# ZEUS VPN Platform

Android-first hybrid VPN platform: Android management app, Linux VPN nodes, and a secure control plane.

> **Status:** Phase 0 scaffold. This repository template is not a functioning VPN server or production-ready application.

## Planned components

- `apps/android/` — Android management app (Kotlin; UI planned with Jetpack Compose).
- `services/control-plane/` — authenticated control API and orchestration (Go).
- `services/vpn-node/` — Linux node agent and VPN engine integration.
- `packages/protocols/` — versioned API and event contracts.
- `infra/` — deployment templates and infrastructure configuration.
- `docs/` — architecture, decisions, security baseline, and delivery plan.
- `scripts/` — repository checks.

## Architecture principles

1. Android is the management/control application first. Android `VpnService` is not treated as an inbound VPN server API.
2. The first production data-plane target is a Linux host running a maintained WireGuard implementation.
3. Use established cryptographic implementations; do not invent cryptography.
4. Keep control-plane credentials separate from VPN peer keys.
5. Make every deployment explicit, reviewable, and reversible.
6. Do not commit secrets, private keys, user data, or production configuration.

## Phase 0

Read [the architecture](docs/architecture.md), [the phase plan](docs/phase-0-checklist.md), and [the security baseline](docs/security-baseline.md).

Run the initial repository checks:

```bash
bash scripts/verify-repository.sh
```

## Current limitations

- No Android APK is implemented yet.
- No VPN node or control-plane service is implemented yet.
- No real network tunnel has been established or tested.
- CI currently checks repository structure only; it does not claim to build Android or Go components.
