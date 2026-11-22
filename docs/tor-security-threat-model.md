# ZEUS Tor-only client security threat model

Scope: Android Tor client, account-bound API, embedded Tor + Hev + Android TUN.
Status: Design review plus account-bound API/schema, Android Tor enrollment repository, Tor-only dashboard and ZEUS VpnService host. No penetration test, disposable-database concurrency proof, Android service/data-plane test, or live-network validation is claimed.

## Assets and trust boundaries

- Customer OIDC session and refresh token encrypted with Android Keystore.
- Installation device reference and server-side account/device entitlement.
- Android VPN consent, TUN descriptor, DNS and application traffic.
- Embedded Tor daemon, local SOCKS listener and Tor control interface.
- Hev native worker and generated private configuration.
- Control-plane database, audit records and invitation-code hashes.
- External network, Tor relays/exits, Android OS and other apps.

## Threat scenarios and required controls

| Threat | Impact | Required control |
|---|---|---|
| Direct-network leak when Tor/Hev fails | Traffic bypasses privacy tunnel | Full default routes for supported families; fail closed; test daemon death, DNS failure, network switch and revoke. |
| SOCKS/control listener bound to LAN | Local peers can access Tor interfaces | Verify effective Tor config and runtime listeners are loopback only; reject wildcard bind. |
| Tor app socket loops into its TUN | Tor cannot bootstrap | Protect every Tor upstream socket via VpnService.protect if integration permits. UID exclusion is currently applied by the registered VpnService and disclosed in the Tor-only dashboard; it remains a temporary compromise until socket protection is verified. |
| IPv6 bypass | IPv6 leaks outside tunnel | Route IPv6 default into TUN; prove Hev and Tor SOCKS IPv6. If unsupported, block inside TUN, never omit route. |
| UDP bypass | UDP/QUIC exits physical interface | Tor SOCKS5 UDP relay is unsupported; fail inside TUN. Verify with packet capture. |
| DNS leak/rebinding | Queries reveal destinations or map to hostile addresses | Route DNS through TUN/Hev mapdns; test A/AAAA, synthetic range, cache expiry and no system fallback. |
| Account/device IDOR | Cross-account inspection or revoke | Derive owner from verified OIDC issuer+subject; scope SQL queries; unknown/foreign IDs both 404. |
| Invitation theft/replay | Unauthorized device registration | High entropy, hashed at rest, short expiry, atomic consume, no raw-code logs, one-use semantics. |
| Concurrent registration | Bypass max device count | Lock account row and count Tor + WireGuard devices in one transaction. |
| Tor/Hev crash | Stale FD, stuck service, traffic leak | Single lifecycle owner, cancellation-safe cleanup, confirmed FD ownership, fail-closed status and recovery tests. |
| False Connected indicator | User believes traffic protected prematurely | UI stages: account authorized, Tor circuit ready, TUN established, Hev active, independent connectivity probe passed. |

## Severity-ranked release blockers

Critical: the ZEUS-owned VpnService host now applies TorVpnTunnelPolicy and starts embedded Tor + Hev, but worker creation is not proof of packet forwarding. No independent remote-domain SOCKS probe or end-to-end TUN-to-SOCKS egress proof has passed. Do not release or claim VPN protection until packet-level leak tests pass.

High: current app UID exclusion bypasses ZEUS app traffic. Investigate socket protection. Do not claim all-device coverage while bypass remains.

High: the existing TunnelConnectivityChecker uses Proxy.NO_PROXY and makes HTTPS requests from the ZEUS app process. Under the current Tor policy that excludes the ZEUS UID, this probe would use the physical network, not the Tor SOCKS path. It must not be reused as proof of Tor connectivity. Add a SOCKS5 remote-domain probe through the loopback Tor listener and validate the observed exit path; never report Connected from a direct app-process HTTPS check.

High: IPv6 SOCKS behavior and UDP fail-closed behavior are not verified. Configuration alone is insufficient.

High: remote Tor-device revocation and account suspension are not enforced by the VPN service. The API exposes customer OIDC self-revoke but no administrator Tor-device revoke/list route; the service start path checks only the locally stored device ID and does not fetch current server status. Invitation-only enrollment cannot use status/revoke endpoints because those require OIDC. Treat remote revocation, inactive-account enforcement and entitlement expiry as NOT IMPLEMENTED until a server-backed policy is designed and tested.

High: the Tor-specific account/device API, additive schema, Android repository, Tor-only dashboard and ZEUS-owned VpnService host are present on the feature branch. Exact-HEAD CI and disposable-database verification are pending, and the actual TUN-to-SOCKS data plane remains unverified. Existing /v1/client/enroll remains WireGuard-specific; never send dummy keys or weaken public-key validation.

## Security retest

- Inspect runtime Tor listeners and socket bindings.
- Capture egress while Tor is stopped, bootstrapping, ready, disconnected and reconnecting.
- Test IPv4, IPv6, UDP, QUIC, DNS, captive portal, airplane mode and Wi-Fi/mobile handoff.
- Verify account A cannot read/revoke account B device, including guessed UUIDs.
- Test expired/replayed/revoked invitation codes and idempotency conflicts.
- Test parallel registrations at max-device boundary.
- Test API 24 through API 36/37, VPN consent denial, process death and service revocation.

No production AWS/VPS, firewall, DNS, WireGuard peer, database or customer record changed.


## Selected server-enforcement architecture

The accepted target is a mandatory ZEUS-controlled Tor egress gateway. The current embedded local Tor path is not server-enforced: a modified client can bypass local checks and use public Tor independently. The gateway can enforce access to ZEUS's managed Tor service only; it cannot prevent independent access to public Tor or third-party proxies.

Required controls before implementation/release:
- Short-lived, device-bound, gateway-audience grants; invitation codes are exchanged once and never reused as long-lived gateway credentials.
- Authenticated encrypted Android-to-gateway transport. Hev's optional SOCKS5 username/password fields are authentication only, not transport encryption; plaintext public SOCKS credentials are prohibited.
- Online authorization per new proxy session, or a bounded authorization cache with authenticated invalidation and a documented revocation SLA.
- Gateway tracks active sessions by device and closes them on device revocation, account suspension, or entitlement expiry.
- No direct-egress fallback. Gateway egress must be constrained to the intended Tor daemon path.
- Destination history, SOCKS request content, circuit IDs, and traffic payloads must not be persisted in logs or telemetry.

A signed JWT with a long expiry is insufficient for immediate revocation. A client-side preflight is defense-in-depth only and cannot be treated as the security boundary.

Proposed API routes for review (NOT IMPLEMENTED): `POST /v1/client/tor/devices/{deviceID}/gateway-grants`, `GET /v1/admin/tor/devices`, and `POST /v1/admin/tor/devices/{deviceID}/revoke`.

See [tor-server-enforced-access-architecture.md](tor-server-enforced-access-architecture.md) for the full design and acceptance gates.
