# ZEUS Tor transport integration design

**Status:** Proposed implementation plan; not a functioning Tor VPN yet.  
**Base:** PR #27 head `bc6ae552c884b9ee487d2f0c9f70507b0c979b52`.  
**Scope:** Android client only. WireGuard remains the default transport and is not removed or modified on servers.

## Decision

Add Tor as a second, user-selectable transport. Do not run two Android VPN interfaces concurrently. A single transport coordinator owns the connection state and enforces exclusive ownership of Android's `VpnService` TUN interface.

- WireGuard mode continues to use the existing official `GoBackend`.
- Tor mode runs an embedded Tor daemon and exposes a loopback SOCKS5 endpoint.
- A dedicated, audited TUN-to-SOCKS adapter is required to route Android app traffic into that SOCKS endpoint. The Tor daemon alone is only a local proxy; it does not transparently tunnel all device traffic.
- The Tor adapter must be integrated into ZEUS's single VPN-service lifecycle, not by starting Orbot's independent VPN service alongside WireGuard. The current WireGuard AAR contributes `GoBackend.VpnService`, and `GoBackend` explicitly starts that nested service class. Do not add a second `android.net.VpnService` component until Always-on resolution and lifecycle ownership are proven. The TUN milestone requires one shared service host (or a controlled WireGuard backend fork/bridge) that can hand the single TUN descriptor to exactly one engine at a time; a simple subclass alone does not redirect GoBackend's explicit service intent.
- Guardian Project's embedded `TorService` has no `VpnService.protect()` integration. Because it runs under the ZEUS app UID, the Tor VPN builder must exclude the ZEUS package from its TUN (or omit it from an allowlist) so Tor's own outbound sockets reach the physical network instead of looping into the TUN. This also means ZEUS app/control-plane traffic itself bypasses Tor by design. If product requirements later demand tunneling ZEUS's own app traffic, a reviewed per-socket protection hook in Tor's native layer is required.

## Android component boundaries

1. `TransportCoordinator`: serializes connect, switch, disconnect, and recovery; stops and verifies the current owner before starting another.
2. `VpnServiceHost`: the single manifest-declared Android VPN service; it owns consent/Always-on lifecycle and hands one TUN descriptor to either the WireGuard backend or Tor adapter. Its integration with the current `GoBackend.VpnService` is an unresolved design gate, not yet implemented.
3. `TorDaemonController`: owns the embedded Tor daemon lifecycle, readiness, SOCKS endpoint discovery, error propagation, and bounded shutdown.
4. `TorVpnEngine`: owns the TUN-to-SOCKS native adapter after `VpnServiceHost` transfers the TUN descriptor. It must not claim connected until Tor has a usable circuit and the adapter is forwarding.
5. `WireGuardEngine`: remains the existing implementation and is called through the coordinator.
6. UI state: `DISCONNECTED / STARTING / CONNECTED / STOPPING / FAILED`, with transport identity and a user-visible failure reason. Never infer internet reachability from daemon startup alone.

## Routing and leak-prevention requirements

- Tor supports TCP streams, not arbitrary UDP forwarding. Tor SOCKS5 does not implement UDP ASSOCIATE or IPv6 CONNECT; unsupported UDP and IPv6 must be dropped inside the tunnel, never sent directly.
- DNS requests must be resolved through Tor (remote hostname resolution after a controlled fake-DNS mapping, or another reviewed design); local resolver fallback is forbidden in Tor mode. Verify that the chosen adapter never sends DNS to the device's physical resolver.
- IPv4 and IPv6 policy must be explicit. Until IPv6 is supported through the Tor path, block IPv6 inside Tor mode rather than allowing direct egress.
- The TUN adapter must have bounded queues, clean cancellation, descriptor ownership, and deterministic shutdown.
- On startup failure, adapter crash, Tor circuit loss, or transport switch, fail closed: keep the TUN/service in a drop-only state until teardown is coordinated; never fall back to direct forwarding while Tor mode is selected.
- A process/service crash can make Android remove the VPN and resume the underlying network unless system lockdown is enabled. For a strict no-leak Tor mode, require Android Always-on VPN with **Block connections without VPN** (lockdown); verify `VpnService.isLockdownEnabled()` on API 29+ and guide the user through system Settings. Android's framework exempts the VPN provider package from lockdown's blocked-UID set; Tor mode still must explicitly exclude the ZEUS package from the TUN so Tor's own sockets do not loop. Verify this interaction on supported Android/OEM versions before claiming crash-resilient fail-closed protection.
- Per-app include/exclude policy must be implemented and tested for Tor separately; do not assume WireGuard profile directives apply to the Tor engine. Never allow the ZEUS package itself into the Tor TUN: in include-only mode omit it, and in full/exclude mode add it to the disallowed set. This is required for Tor's outbound sockets and means ZEUS app traffic is outside Tor.
- Do not expose the SOCKS or control port on LAN interfaces. Bind to loopback only.

## Dependency research

- Guardian Project's `tor-android` provides the native Tor daemon and Android `TorService`; it does not by itself implement device-wide VPN routing.
- The current Maven Central artifact metadata reports `info.guardianproject:tor-android:0.4.9.17`. Pinning and ABI packaging must be verified in CI before adoption.
- Orbot uses `hev-socks5-tunnel` as a native TUN-to-SOCKS component. Upstream Hev is MIT-licensed and its 2.20.0 release publishes Android ABI artifacts and an AAR with a JNI wrapper. This is the leading candidate, but it is not yet added to ZEUS.
- Hev supports UDP relay through SOCKS5 UDP ASSOCIATE or its own UDP-over-TCP `FWD_UDP` extension; its `socks5.udp` setting selects a relay mode and has no documented `off` value. For Tor, configure the standard `udp: 'udp'` mode only: Tor rejects UDP ASSOCIATE, and the Hev source returns a handshake error for every non-success SOCKS reply; the session runner then exits and the UDP-session destructor frees queued datagrams/removes the PCB. Do not enable Hev's UDP-over-TCP extension. This is source-level fail-closed evidence, not runtime proof; add an integration test with a SOCKS endpoint returning `REP=0x07` and verify no direct egress. Hev's MapDNS handler answers DNS packets addressed to its configured fake-DNS IP/port with synthetic A records; verify that subsequent fake-IP TCP connections are sent to Tor by hostname and that no physical-resolver fallback occurs.
- Do not copy Orbot's VPN service or assume its app-level integration is reusable as a library.

## Delivery sequence and acceptance gates

### Gate A — Tor daemon only
- [x] Add a pinned Tor Android dependency and an initial lifecycle wrapper.
- [x] Bind to the embedded TorService and wait for its first-circuit-ready status plus a valid loopback SOCKS port.
- [x] Handle startup timeout, startup error, unexpected service stop, binding loss, and stop-during-start at the controller level.
- [ ] Verify resolved AAR contents and release ABI packaging in an actual Android build.
- [x] Remove the readiness race by waiting for Tor's first-circuit event and polling the live ControlPort `GETINFO net/listeners/socks` value under the startup deadline; accept loopback IPv4/IPv6 only (static review; build/runtime retest pending).
- [x] Add unit regression cases for quoted IPv4/IPv6 loopback listeners and rejection of non-loopback or invalid endpoints (execution pending CI).
- [ ] Test start, first-circuit readiness, stop-during-start, repeated start/stop, process recreation, and bounded native shutdown on an emulator/device.
- Keep this gate explicitly labelled **local Tor proxy only**; no VPN-connected claim. The controller cannot independently observe completion of native Tor shutdown after unbinding.

### Gate B — TUN-to-SOCKS data plane
- [x] Review Hev upstream source, MIT license, JNI API, Android AAR packaging, and release ABI assets; identify it as the leading candidate.
- [x] Static source review confirms Hev terminates UDP sessions when the SOCKS server returns a non-success response; runtime UDP drop/no-egress test remains required.
- [ ] Pin and integrate the selected TUN-to-SOCKS artifact after artifact hash, ABI, JNI and R8 verification.
- [ ] Resolve the single-service integration with `GoBackend.VpnService` (or a controlled backend fork/bridge); do not register a second VPN service until Always-on behavior is verified.
- [ ] Integrate one TUN owner with the existing Android VPN consent and foreground-service lifecycle.
- [ ] Exclude the ZEUS app package from the Tor TUN and verify Tor bootstrap works while Android lockdown is enabled; verify other apps remain inside the TUN.
- [ ] Test TCP forwarding, MapDNS/remote hostname resolution, UDP ASSOCIATE rejection with no egress, IPv4/IPv6 leak resistance, adapter crash, cancellation, network handover, and Android lockdown behavior.

### Gate C — transport coordinator and UI
- Add WireGuard / Tor selection, persisted preference, truthful state, and serialized switching.
- Preserve current WireGuard split-tunnel behavior. Add Tor-specific per-app policy only after the data plane is proven.
- Verify that switching never leaves two active VPN owners and that failure in Tor mode cannot fall back to direct traffic.

### Gate D — release verification
- Unit tests, Android build, instrumentation tests, emulator tests, and physical-device tests.
- Independent external IP and DNS checks, IPv4/IPv6 leak checks, TCP/UDP behavior checks, network handover, process death, and battery/background behavior.
- Keep the PR Draft until all applicable checks pass. No deployment or server-side changes are part of this work.

## Explicit non-goals

- Replacing or deleting WireGuard.
- Sending WireGuard server traffic through Tor.
- Claiming anonymity against global traffic correlation or a compromised endpoint.
- Supporting arbitrary UDP in Tor mode.
- Changing AWS, databases, VPN nodes, firewall rules, DNS, customer state, or WireGuard peers.
