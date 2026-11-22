# ADR: Tor as the sole Android VPN engine

**Status:** Product decisions confirmed; implementation in progress and not yet complete.  
**Date:** 2026-11-22  
**Repository:** `abolboy/zeus-vpn-platform`  
**Branch:** `feat/android-tor-transport-pr27`  
**Related:** PR #28

## User decision

ZEUS will use Tor as the primary and only Android VPN transport. The Android WireGuard backend is to be removed from the final client; it is not a fallback, selectable mode, or parallel engine.

This authorization applies to the **Android client engine and its packaged dependencies only**. It does not authorize deleting server-side WireGuard node support, changing VPS configuration, deleting peers, changing AWS resources, or altering production services.

## Account and enrollment decision

The Android Tor client is account-bound. Retain verified OIDC identity, account status, shared device limits, administrator-issued Tor invitations, and device revocation. Tor device registration is a transport-specific resource and must not create a WireGuard peer or require WireGuard key material. The API contract is defined in [tor-account-bound-api-v1.md](tor-account-bound-api-v1.md). Existing WireGuard enrollment routes remain intact for compatibility.

## Architecture decision

Prefer a ZEUS-owned Android `VpnService` over a controlled fork of WireGuard's `GoBackend` if the product is Tor-only.

The service owns Android VPN consent, foreground-service lifecycle, Always-on startup, the TUN interface, route policy, and teardown. The embedded Guardian Project Tor service remains a separate daemon that exposes a loopback-only SOCKS endpoint. Hev owns the TUN-to-SOCKS forwarding worker and receives the TUN file descriptor from the ZEUS service.

Expected packet path:

```
Other app TCP traffic
  -> Android ZEUS VpnService TUN
  -> Hev TUN-to-SOCKS
  -> 127.0.0.1:<Tor SOCKS port>
  -> embedded Tor daemon
  -> Tor circuit
  -> destination

ZEUS app UID / Tor daemon sockets
  -> excluded from the TUN
  -> physical network (bootstrap and Tor transport)
```

The ZEUS app package must be excluded from the Tor TUN to prevent a routing loop because the current embedded Tor integration has no `VpnService.protect()` socket hook. This means ZEUS app/control-plane traffic is not routed through Tor; the UI must disclose this limitation.

## Why this is preferable for a Tor-only client

- It removes the need to preserve the WireGuard backend's private static service future, hard-coded service component, and internal TUN handoff.
- It avoids maintaining a fork of `GoBackend` solely to expose a VPN service that the product no longer needs.
- It gives ZEUS direct ownership of the one VPN service and TUN lifecycle.
- It reduces the Android dependency graph and prevents the WireGuard AAR from contributing an unwanted VPN service declaration.

This is not a shortcut around VPN engineering. ZEUS must still correctly implement consent, foreground service behavior, Always-on startup, lockdown handling, network changes, process recreation, notification actions, TUN routes, DNS, and deterministic resource shutdown.

## Migration constraints

1. Keep `minSdk = 24` (Android 7). Do not raise it to API 29 and do not use `tools:overrideLibrary`.
2. Keep the existing WireGuard implementation and dependency intact until the Tor-only service has passed the required build, unit, emulator, and physical-device gates. Then remove only Android client references and dependencies that are proven unused.
3. Before deleting the WireGuard AAR, search every Android source, test, manifest, Gradle file, CI workflow, and packaging rule for imports, JNI/native assets, service declarations, profile formats, and UI assumptions. Record the exact removal set.
4. The merged debug and release manifests must contain exactly one ZEUS-owned `android.net.VpnService` declaration and no WireGuard `GoBackend.VpnService` declaration.
5. Tor mode must fail closed. There is no direct-network fallback after Tor is selected.
6. First milestone is TCP-only. Tor does not provide SOCKS5 UDP ASSOCIATE; UDP must be dropped with no physical-network egress. The TUN plan is dual-stack (`198.18.0.1` / `fc00::1`) and must route both IPv4 and IPv6 defaults into TUN. Tor IPv6 SOCKS exit support remains unverified; unsupported IPv6 must fail inside the tunnel, never fall back to the physical network.
7. DNS must use the reviewed fake-DNS/MapDNS mapping and Tor-side hostname resolution. No physical resolver fallback is permitted.
8. The Tor daemon's own sockets must bypass the TUN to avoid loops. The ZEUS app UID is therefore excluded and is outside Tor protection.
9. Keep server-side WireGuard node, control-plane, peer, database, and VPS code unchanged unless a separate approved decision requires otherwise.

## Implementation increment recorded on 2026-11-22

- Hev config now explicitly declares the matching IPv4 `198.18.0.1` and IPv6 `fc00::1` tunnel addresses.
- `TorVpnTunnelPolicy` centralizes the future ZEUS `VpnService.Builder` plan: dual-stack interface addresses/default routes, fake-DNS server, MTU 1500, and exclusion of the ZEUS package to prevent Tor socket loops.
- Unit tests cover the policy values. The service has not yet consumed this policy; this does not route live device traffic or prove IPv6 SOCKS exit behavior.

## Implementation sequence and specialist gates

### Coding Master
- Inventory all Android WireGuard engine usages and define the minimum deletion set.
- Implement a ZEUS-owned service and explicit engine interfaces; keep transport lifecycle state separate from UI.
- Ensure only one TUN descriptor owner exists and descriptor ownership transfers are explicit.
- Remove the WireGuard Android dependency only after all references are migrated and tests pass.

### API Engineering
- Add the documented Tor-specific account/device contract alongside existing WireGuard enrollment routes; do not weaken or repurpose the peer-provisioning API.
- Enforce account-scoped authorization, shared device quotas, idempotency, invitation one-time use, and revocation atomically.
- Ensure the client reports transport identity and truthful readiness; Tor daemon readiness alone must never be reported as VPN-connected.
- Do not add server-side Tor relays or change node provisioning infrastructure.

### Android Kotlin
- Implement `VpnService` with consent, foreground notification, sticky/Always-on cold-start behavior, service recreation, and safe stop.
- Build the TUN with explicit IPv4 routes, DNS/fake-DNS policy, MTU, and app exclusion.
- Keep API 24 compatibility and verify service type/foreground requirements for the supported target SDK.
- Serialize start/stop/switch and cancellation with structured concurrency; do not close a TUN fd while native Hev may still use it.

### Cybersecurity
- Enforce loopback-only SOCKS and ControlPort listeners.
- Block UDP and IPv6 in the first Tor VPN milestone; prove no direct egress.
- Prevent DNS leaks and TUN routing loops.
- Verify Android lockdown and the ZEUS package exclusion interaction on supported API/OEM combinations.
- Threat-model Tor bootstrap, service crash, native worker crash, process death, network handover, and malicious configuration.

### Testing & QA
- Unit tests: lifecycle state machine, cancellation, descriptor ownership, route policy, DNS mapping, UDP rejection, IPv6 blocking, and failure recovery.
- Instrumentation: VPN consent, foreground service, Always-on cold start, process recreation, and manifest single-service assertion.
- Emulator/device tests: TCP forwarding through Tor, external IP, DNS path, IPv4/IPv6 leaks, UDP no-egress, Tor circuit loss, network changes, and shutdown under traffic.
- Physical Android 7/API 24 device test is required before claiming support.

### DevOps
- Keep pinned Tor/Hev source and checksums reproducible.
- CI must build API 24-compatible native artifacts and test Android API 24, 28, and current supported API levels.
- Inspect final APK/AAB contents and merged manifests for duplicate VPN services, stale WireGuard classes, unexpected native libraries, and ABI coverage.
- Do not deploy or change VPS/AWS/production resources as part of this Android-only migration.

## Acceptance gates before removing the WireGuard Android engine

- [ ] ZEUS-owned VPN service starts and stops reliably on API 24+.
- [ ] Tor bootstrap reaches a usable circuit and local SOCKS endpoint.
- [ ] Hev starts on the ZEUS-owned TUN and real TCP traffic reaches Tor exits.
- [ ] DNS is resolved through Tor with no physical DNS fallback.
- [ ] UDP and unsupported IPv6 produce no direct egress.
- [ ] Tor sockets bypass the TUN and no routing loop occurs.
- [ ] Service crash, native worker crash, cancellation, process death, and network handover fail closed.
- [ ] Always-on and lockdown behavior is verified on emulator and physical devices.
- [ ] Merged manifests contain exactly one ZEUS VPN service.
- [ ] Android debug/release builds and all relevant CI tests pass.
- [ ] Android source, tests, manifests, and dependency graph contain no unintended WireGuard backend references.
- [ ] Server-side WireGuard node/control-plane behavior remains unchanged and its CI passes.

## Current status

PR #28 currently contains the Tor daemon controller, Hev config/runtime wrapper, and regression tests. It does **not** contain a ZEUS-owned VPN service or a working TUN-to-SOCKS data plane. The latest observed Android build and unit-test job passed, as did Hev JNI compatibility jobs on API 24, 28, and 29. The Package Visibility emulator job was still running at the last check. Therefore the WireGuard Android dependency has **not** been removed, and Tor is **not** yet a functioning VPN mode.

No merge, release, deployment, or production/server-side change is authorized by this ADR.


## Addendum — server-enforced ZEUS access (2026-11-22)

The earlier packet-path section describes the **initial local-Tor prototype**, not the final authorization architecture. The accepted security decision is now a mandatory ZEUS-controlled Tor egress gateway as specified in [tor-server-enforced-access-architecture.md](tor-server-enforced-access-architecture.md).

Consequences:
- Embedded local Tor is not an acceptable security fallback for managed ZEUS access.
- Hev may remain as the Android TUN-to-SOCKS component only if its upstream can be securely connected to a gateway through an authenticated encrypted local adapter.
- The official Hev SOCKS5 configuration exposes optional username/password fields, but SOCKS5 authentication alone does not encrypt the network transport. A plaintext remote SOCKS5 connection with reusable credentials is prohibited.
- The gateway must enforce device/account status independently of APK behavior, terminate active sessions on revocation, and forward only through ZEUS-controlled Tor.
- This architecture cannot prevent independent use of public Tor or third-party proxies outside ZEUS.
- Gateway code, grant/admin APIs, Android secure transport adapter, hosting choice, and end-to-end tests remain unimplemented. No deployment is authorized.

The server-enforced gateway ADR supersedes any implication in this document that local embedded Tor alone provides remote revocation or modified-client resistance.
