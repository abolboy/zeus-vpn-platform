# ZEUS optional Tor transport — implementation decision

## Status

Proposed implementation on the stacked branch `feat/android-tor-transport`, based on the current Draft integration candidate PR #27. This branch is not based on `main`, and this work must remain a Draft stacked change until its prerequisites are reviewed.

## Product behavior

- Keep WireGuard as the default transport and preserve its existing profile, provisioning, and reconnect behavior.
- Add Tor as an explicitly selected alternative transport. Never silently switch providers or send traffic through Tor.
- A single transport coordinator owns connection state and serializes start/stop/switch operations. Only one engine may own Android's active VPN interface at a time.
- Switching transports first stops and verifies shutdown of the current engine, then requests/uses Android VPN consent and starts the selected engine.
- Display the selected transport and distinguish engine-started, Tor circuit-established, and verified traffic states.

## Android data path

Tor mode requires more than starting a local SOCKS proxy:

1. Start a bundled Tor daemon using the maintained Guardian Project `tor-android` runtime and wait for a confirmed circuit-established event.
2. Create the Android TUN interface from the ZEUS-owned VPN service.
3. Forward supported TCP flows from TUN to Tor's loopback SOCKS listener through a pinned, reproducible tun2socks implementation (candidate: `heiher/hev-socks5-tunnel`, MIT).
4. Do not route the Tor daemon's own upstream sockets back into the TUN. Protect/bypass those sockets through the owning VpnService.
5. Resolve DNS through Tor-side resolution only. Do not send DNS queries to the device's ordinary resolver or a public DNS service outside Tor.
6. Fail closed: if Tor, the circuit, the TUN-to-SOCKS bridge, or DNS path fails, stop the Tor tunnel and do not fall back to direct networking.

## Protocol limitations

Tor's SOCKS interface is for TCP streams; it is not a general UDP tunnel. The first Tor mode must explicitly block/drop unsupported UDP rather than leak it outside Tor. This means apps and features that require UDP (including many real-time, gaming, VoIP, and QUIC paths) may fail or fall back to TCP where the app supports it. Do not advertise full protocol parity with WireGuard.

IPv4/IPv6 routing and DNS behavior must be explicit and tested. Do not claim leak protection until tests verify both address families and failure paths on a physical device.

## Censorship circumvention

Direct Tor connectivity may be blocked on some networks. Bridge and pluggable-transport support (for example, supported obfs4/Snowflake paths) should be a separate, explicit configuration phase. Do not ship copied bridge credentials, public bridge lists, or third-party endpoints as trusted defaults. Bridge configuration must be validated and stored without exposing secrets in logs.

## Dependency and supply-chain gates

- Verify the exact published `tor-android` and `jtorctl` artifact versions, checksums, licenses, and transitive dependencies before pinning them. The upstream README and current source snapshot do not show identical Tor artifact version strings; do not guess the coordinate.
- Pin the tun2socks release/tag and artifact checksum or build it reproducibly from a pinned source commit. Preserve MIT notices.
- Review Tor and all bundled native component licenses/notices for the APK distribution.
- Keep native ABIs aligned with ZEUS's supported Android ABIs and verify the merged manifest/service declarations.

## Implementation stages

1. Dependency feasibility spike: resolve exact Tor artifacts, verify service lifecycle/API, ABI contents, licenses, and reproducible CI retrieval.
2. Tor runtime adapter: start/stop, control/status events, circuit-ready timeout, bridge configuration boundary, and redacted diagnostics.
3. Single-owner VPN transport coordinator: serialize WireGuard/Tor switching and recover cleanly from partial startup/shutdown.
4. TUN-to-SOCKS bridge: protected upstream sockets, TCP forwarding, explicit UDP drop policy, DNS-over-Tor, IPv4/IPv6 policy, and fail-closed behavior.
5. UI: WireGuard/Tor selector, clear unsupported-UDP warning, bridge settings, and truthful connection states.
6. Tests: unit and instrumentation tests for state transitions, cancellation, failure cleanup, DNS routing, UDP blocking, process death, network handover, and no-direct-fallback.
7. Physical-device acceptance: verify Tor circuit, external IP, DNS path, IPv4/IPv6 leaks, app routing, reconnect, battery, and blocked-network/bridge scenarios.

## Release gates

- No production VPN node, WireGuard peer, AWS resource, database, or firewall changes are part of this client-side feature.
- Do not merge or deploy this stacked Draft branch without explicit user approval.
- CI success is not proof of Tor anonymity, censorship circumvention, DNS leak protection, or physical-device traffic routing.
