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

## UAC SNI Spoofer Android — source review (2026-11-16)

Repository reviewed: https://github.com/Floxu1/UAC-SNI-Spoofer-Android  
Reviewed main commit: `200de3b1f029839d2860042680afebf8817c3a90`  
Latest GitHub release observed: `2.1.2`, published 2026-11-13.

### What the inspected implementation actually contains

The repository is more than an SNI utility. Its current source has four engine modes (the README is stale and still documents 2.0.8 / three engines):

1. **Xray** — VLESS/VMess/Trojan profiles plus an automatic candidate pipeline. It fetches a third-party profile list from GitHub with jsDelivr/API fallbacks, probes the current network, resolves ECH material, tests candidate routes, caches rankings per network, and attempts failover.
2. **Tor WebTunnel** — a separate Tor runtime and WebTunnel bridge path.
3. **UAC PoW** — an Aether Rust runtime that establishes a Cloudflare WARP/MASQUE outer path and runs Psiphon as an inner tunnel.
4. **UAC Direct** — a separate Xray-based direct/fragment path, with optional local TLS interception and re-packaging for a limited domain set.

The app owns one Android `VpnService` and explicitly switches engines only after stopping the active engine. The TUN-to-SOCKS/native bridge is part of the design. This confirms that ZEUS needs a single engine coordinator and explicit transport adapters, not multiple independent VPN services competing for Android's one active VPN slot.

### Useful concepts for ZEUS

- A clear engine state machine: prepare, connect, verify, monitor, fail over, stop.
- Separate connection diagnostics for device network, profile source, tunnel handshake, real traffic, DNS and HTTPS reachability.
- Network-scoped last-known-good ranking with expiry and bounded retry budgets.
- Candidate testing that verifies data through the candidate tunnel, not only ICMP or a TCP port.
- One owner for the Android TUN descriptor and socket protection.
- Per-engine logs and live route explanations, with credentials and private profile fields redacted.
- Optional local SOCKS mode and app routing, implemented as features of the active engine rather than separate VPN services.

The existing ZEUS split-tunnel feature is the first part of that direction. The next useful ZEUS work is the common engine contract plus a provider-authorized, signed endpoint manifest and a real end-to-end candidate verifier.

### Important security and reliability findings

- The README's automatic Xray list is fetched from GitHub/jsDelivr and cached with ETags. The inspected fetcher validates that entries parse, but the fetched list is not cryptographically signed. ZEUS must not trust a mutable third-party config list as an authority for user traffic. Any remote inventory should be signed, versioned, expiry-bound and restricted to provider-authorized hosts/protocols.
- The UAC Direct code creates a per-install CA for a local TLS MITM/re-pack path. Its own source says the user must install that CA for browsers to trust intercepted sites. This is genuine TLS interception, not ordinary SNI selection. ZEUS must not enable it silently or by default; it needs explicit consent, a narrow scope, clear certificate lifecycle/removal, and tests for certificate pinning and app trust behavior.
- The inspected Direct CA private key is written under app-private `filesDir` as a PEM file, not generated as a non-exportable Android Keystore key. Android backup is disabled in its manifest, but this is not equivalent to Keystore-backed encryption.
- Aether's persisted identity structure contains an access token, MASQUE certificate private key and WireGuard private key in its TOML identity record under app-private storage. Its writer sets restrictive Unix file permissions, and the Android manifest disables backup, but ZEUS should use its existing Keystore-backed encrypted storage pattern for any comparable identity.
- PoW, Tor and adaptive logs are mirrored to logcat by default, and the app offers user-initiated log export. ZEUS must redact credentials, profile URIs, access tokens, certificate material, and sensitive endpoint details before any log is exposed.
- The project depends on multiple independent services and trust domains (Cloudflare, Psiphon, Tor/WebTunnel, GitHub/jsDelivr, and public DoH resolvers). These are separate providers with their own availability, privacy, account and policy boundaries. They must not be combined into one opaque "automatic" service without disclosure.
- Engine 4's domain fronting and fragmentation behavior is network- and destination-dependent. A successful probe of one Cloudflare endpoint or one hostname does not establish that every site or app will work.

### License, build and supply-chain blockers

- GitHub repository metadata reports no project-level license, and there is no root `LICENSE` file. The presence of `THIRD_PARTY_NOTICES.md` does not grant ZEUS permission to copy the whole application. Do not copy the UAC app source or ship its APK without a clear license from the project owner.
- The notices identify materially different licenses among components, including AGPL-3.0 for Aether, GPL-3.0 for Psiphon, MPL-2.0 for Xray, and BSD-2-Clause for quiche. Each component must be handled under its own terms; bundling the full stack has substantial source-distribution and compliance implications.
- The repository commits large prebuilt native components (including approximately 58 MB and 44 MB AARs). Its latest arm64 release APK is approximately 66.9 MB. Importing everything would substantially increase ZEUS APK size, build time, attack surface and maintenance burden.
- The app targets/compiles against Android SDK 35, uses JDK 17 and NDK 26.3.11579264. ZEUS currently uses a newer AGP/Kotlin/compile-SDK toolchain. Native ABI and Gradle integration need a dedicated spike.
- Gradle invokes `powershell.exe` to rebuild Aether when its prebuilt shared library is missing, although a shell build script also exists. This is not directly compatible with the current Termux workflow, and the build path should be made reproducible on Linux before adopting that native component.
- Unit-test source files are present, but no GitHub Actions workflow was found in the inspected repository tree. We did not build, install, execute or independently validate its APK or claim its README performance/censorship results as proven.

### ZEUS decision

Do **not** copy the whole UAC application, import its APK, embed its remote server list, or enable its local TLS MITM behavior.

Use its architecture as a reference and implement in ZEUS in this order:

1. Keep the current WireGuard engine stable and complete physical-device tests for split routing.
2. Add a provider-owned signed endpoint inventory, bounded candidate probes, verified tunnel/traffic checks, and per-network last-known-good memory.
3. Add a separate Xray adapter only after selecting a compatible, properly licensed Xray distribution and a reproducible Android TUN/SOCKS integration. Start with user-owned profiles; do not fetch arbitrary public credentials.
4. Evaluate WARP/MASQUE and Psiphon as independent optional adapters with separate identity, consent, provider disclosures and credential storage.
5. Evaluate Tor WebTunnel as a separate privacy/availability option with clear exit-node and performance expectations.
6. Keep TLS interception and any carrier-specific packet desynchronization out of the default path. Treat them as isolated, opt-in research requiring protocol review, explicit risk disclosure and real-device acceptance tests.

No UAC source, binary, server list, certificate or account credential was copied into ZEUS by this review. No device or VPN server was changed.

