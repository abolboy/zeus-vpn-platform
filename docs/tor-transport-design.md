# ZEUS Tor transport integration design

**Status:** Proposed implementation plan; not a functioning Tor VPN yet.  
**Base:** PR #27 head `bc6ae552c884b9ee487d2f0c9f70507b0c979b52`.  
**Scope:** Android client only. WireGuard remains the default transport and is not removed or modified on servers.

## Decision

Add Tor as a second, user-selectable transport. Do not run two Android VPN interfaces concurrently. A single transport coordinator owns the connection state and enforces exclusive ownership of Android's `VpnService` TUN interface.

- WireGuard mode continues to use the existing official `GoBackend`.
- Tor mode runs an embedded Tor daemon and exposes a loopback SOCKS5 endpoint.
- A dedicated, audited TUN-to-SOCKS adapter is required to route Android app traffic into that SOCKS endpoint. The Tor daemon alone is only a local proxy; it does not transparently tunnel all device traffic.
- The Tor adapter must be integrated into ZEUS's single VPN-service lifecycle, not by starting Orbot's independent VPN service alongside WireGuard.

## Android component boundaries

1. `TransportCoordinator`: serializes connect, switch, disconnect, and recovery; stops and verifies the current owner before starting another.
2. `TorDaemonController`: owns the embedded Tor daemon lifecycle, readiness, SOCKS endpoint discovery, error propagation, and bounded shutdown.
3. `TorVpnEngine`: owns the Android TUN descriptor and the TUN-to-SOCKS native adapter. It must not claim connected until Tor has a usable circuit and the adapter is forwarding.
4. `WireGuardEngine`: remains the existing implementation and is called through the coordinator.
5. UI state: `DISCONNECTED / STARTING / CONNECTED / STOPPING / FAILED`, with transport identity and a user-visible failure reason. Never infer internet reachability from daemon startup alone.

## Routing and leak-prevention requirements

- Tor supports TCP streams, not arbitrary UDP forwarding. Do not silently send unsupported UDP traffic outside the tunnel.
- DNS requests must be resolved through Tor (remote SOCKS hostname resolution or a reviewed DNS-over-TCP design); local resolver fallback is forbidden in Tor mode.
- IPv4 and IPv6 policy must be explicit. Until IPv6 is supported through the Tor path, block IPv6 inside Tor mode rather than allowing direct egress.
- The TUN adapter must have bounded queues, clean cancellation, descriptor ownership, and deterministic shutdown.
- On startup failure, adapter crash, Tor circuit loss, or transport switch, fail closed: remove/close the TUN and do not restore direct connectivity while Tor mode is selected.
- Per-app include/exclude policy must be implemented and tested for Tor separately; do not assume WireGuard profile directives apply to the Tor engine.
- Do not expose the SOCKS or control port on LAN interfaces. Bind to loopback only.

## Dependency research

- Guardian Project's `tor-android` provides the native Tor daemon and Android `TorService`; it does not by itself implement device-wide VPN routing.
- The current Maven Central artifact metadata reports `info.guardianproject:tor-android:0.4.9.17`. Pinning and ABI packaging must be verified in CI before adoption.
- Orbot uses `hev-socks5-tunnel` for TUN-to-SOCKS routing. ZEUS must review its upstream source, license, ABI outputs, lifecycle API, and compatibility before vendoring or depending on it. Do not copy Orbot's VPN service or assume its app-level integration is reusable as a library.

## Delivery sequence and acceptance gates

### Gate A — Tor daemon only
- Add a pinned Tor Android dependency and a lifecycle wrapper.
- Verify supported ABIs include the ZEUS release ABI and minSdk 24.
- Test start, first-circuit readiness, stop-during-start, repeated start/stop, process recreation, and bounded shutdown.
- Keep this gate explicitly labelled **local Tor proxy only**; no VPN-connected claim.

### Gate B — TUN-to-SOCKS data plane
- Select and pin a maintained TUN-to-SOCKS implementation after source/license/API review.
- Integrate one TUN owner with the existing Android VPN consent and foreground-service lifecycle.
- Test TCP forwarding, DNS through Tor, UDP blocking, IPv4/IPv6 leak resistance, adapter crash, cancellation, and network handover.

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
