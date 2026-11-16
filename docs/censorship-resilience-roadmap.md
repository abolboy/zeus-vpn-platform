# ZEUS censorship-resilience roadmap

This document turns the externally observed WarpGen / Varvara feature set into an independently implemented, testable ZEUS plan. It is not a promise that every network filter can be bypassed.

## Reference-project findings

The public `warpgen/warpgen.github.io` repository is a static mirror/landing page, not the source code for a VPN client. Its `vless.txt` contains one static VLESS URI; it must not be embedded as a ZEUS server. The page links to the separate Varvara Android client. The public `warpgen/varvara` repository currently contains only a README; its release page distributes an APK but does not publish the client implementation. ZEUS therefore cannot verify or port Varvara's implementation from source.

The advertised product concepts worth evaluating independently are:

1. Test multiple Cloudflare WARP endpoint candidates and choose a recently working endpoint.
2. Support a censorship-resistant protocol such as AmneziaWG where the backend and server both support it.
3. Route selected apps through or outside a tunnel.
4. Keep alternative distribution/update channels and diagnostic guidance available when the primary service hostname is unreachable.

## Delivery stages

### Stage 1 — app-level routing (feature branch)

- Implement Android `IncludedApplications` / `ExcludedApplications` using the existing WireGuard backend.
- Store package selections locally; do not upload the user's installed-app list.
- Bound selection count and validate package names.
- Verify both include-only and exclude-selected behavior on real Android devices.
- Do not claim this feature itself bypasses filtering.

### Stage 2 — reliable endpoint selection

- Define a provider-authorized endpoint inventory (host, UDP port, protocol, profile identity, expiry, region label).
- Build a cancellable diagnostic that tests a small candidate set with strict timeouts and low concurrency.
- Prefer a real tunnel handshake plus non-zero traffic counters and HTTPS reachability; latency alone or an open UDP port is insufficient.
- Cache the last-known-good choice with a timestamp and re-test after Wi-Fi/mobile handoff.
- Keep the scan local to the device and avoid exposing private keys or full profile contents in telemetry.
- Add per-network history only with user consent and an explicit retention policy.

### Stage 3 — optional Cloudflare WARP adapter

- Treat WARP as its own provider and account/profile lifecycle, not as an arbitrary WireGuard endpoint.
- Evaluate the MIT-licensed `wgcf` project as a protocol/API reference; its use of Cloudflare's unofficial/undocumented registration interfaces may break without notice and must be reviewed against Cloudflare's current terms.
- Generate a device-specific identity and keep its private key on-device.
- Verify WARP-specific fields (including any provider-specific reserved bytes) end-to-end. Do not assume a generic WireGuard engine can carry them.
- Make WARP an optional transport, never silently route users to a third party or hard-code a shared public profile.

### Stage 4 — AmneziaWG adapter

- Integrate the official AmneziaWG Android tunnel runtime only after a build/ABI feasibility spike against ZEUS's current Android Gradle, Kotlin, NDK, and minSdk configuration.
- Pin the root repository and every submodule to immutable commits; build native libraries reproducibly in CI for the supported ABIs.
- Review licensing before bundling: the Android repository declares Apache-2.0, while its `amneziawg-tools` submodule declares GPL-2.0. Preserve notices and meet source-distribution obligations for shipped binaries.
- Define and validate the server-side obfuscation parameters and protocol version. Client-side obfuscation without a compatible server is not useful.
- Test CPU/battery cost, handshake reliability, MTU, network handover, and leak behavior.

### Stage 5 — transport policy and fallback

- Introduce explicit transport adapters and a state machine; do not overload the current WireGuard profile model.
- Keep protocol-specific credentials and private keys isolated.
- Retry a known-good endpoint before changing transport; respect retry budgets and cancellation.
- Show which transport was selected and why, without exposing sensitive endpoint metadata in public logs.
- Evaluate VLESS/REALITY and Hysteria2 only as separate adapters with a compatible server implementation, protocol review, and their own regression suite.

## Hard limits

- No client can guarantee connectivity if the carrier blocks all configured endpoints/transports, the server is unavailable, or Android prevents the VPN service.
- Strict IP/domain allow-lists can block all unapproved VPN endpoints. Split tunneling does not solve an allow-list.
- Free public endpoints can be overloaded, rate-limited, revoked, or disappear. Never promise uptime or privacy properties that ZEUS does not control.
- DNS/HTTPS probes are useful diagnostics, not proof that every app or destination works.
- The current ZEUS Control Plane provisions WireGuard peers. It does not currently provision Cloudflare WARP, AmneziaWG, VLESS/REALITY, or Hysteria2 credentials.

## Acceptance matrix

Run on physical Android devices and real SIM/Wi-Fi networks, recording date, carrier/provider, access type, Android version, selected transport, candidate endpoint identifier, handshake age, RX/TX deltas, HTTPS result, DNS result, MTU, battery cost, and leak checks. Test at least: direct UDP success, UDP blocked, DNS tampering/failure, IPv4-only, IPv6-only/dual-stack, captive portal, Wi-Fi-to-mobile handoff, mobile-to-Wi-Fi handoff, process death, reboot, and control-plane outage while data plane remains active.

Never use the production VPN node for experimental scanning or mutations. Begin with isolated staging endpoints and a documented rollback plan.

## Source and license references

- WarpGen landing page: https://github.com/warpgen/warpgen.github.io
- Varvara release/source repository: https://github.com/warpgen/varvara
- Cloudflare WARP profile generator reference: https://github.com/ViRb3/wgcf
- Official AmneziaWG Android runtime: https://github.com/amnezia-vpn/amneziawg-android
- AmneziaWG native tools submodule: https://github.com/amnezia-vpn/amneziawg-tools
