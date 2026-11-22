# ZEUS Tor server-enforced access architecture

Status: **Architecture decision accepted for design; implementation and deployment are not yet complete.**
Scope: ZEUS Tor-only Android client, control plane, PostgreSQL device registry, and a ZEUS-controlled Tor egress gateway.
Related branch: `feat/android-tor-transport-pr27`
Related PR: #28

## 1. Decision and security boundary

The Android app must not be the final authorization authority. A modified APK can remove local checks, forge local state, or bypass client-side status polling.

ZEUS will enforce access at a mandatory ZEUS-controlled gateway. The gateway is the only supported path from the managed client to the ZEUS-provided Tor service. It authenticates a device for each new proxy session, checks the device and account entitlement against server-controlled state, and forwards authorized traffic through a Tor client/daemon controlled by ZEUS.

This controls access to ZEUS's managed Tor gateway and ZEUS-provided service. It cannot prevent a modified Android app or a user from independently connecting to the public Tor network or another third-party proxy outside ZEUS. Do not claim otherwise.

## 2. Target data path

1. Android authenticates to the ZEUS control plane using OIDC or completes a one-time invitation exchange.
2. Control plane derives account ownership from verified identity; the client never supplies an authoritative user ID.
3. Android requests a short-lived, device-bound gateway authorization grant.
4. Android establishes an authenticated, encrypted transport to the ZEUS gateway. Credentials must not traverse the public network as cleartext SOCKS5 username/password.
5. A local client-side adapter exposes a loopback SOCKS5 endpoint to Hev. The adapter transports the session securely to the gateway.
6. The gateway validates the device grant and current account/device entitlement before accepting each new proxy session.
7. The gateway forwards authorized requests through its Tor daemon. The Android client must not silently fall back to the embedded local Tor path or direct networking when gateway authorization or transport fails.
8. The gateway records only the minimum operational/security metadata. It must not persist destination history, SOCKS request contents, Tor circuit identifiers, or traffic content.

The current Android branch embeds Tor locally and configures Hev to use a loopback Tor SOCKS endpoint. This is not yet the server-enforced data path. The existing local-Tor mode must remain unavailable as a claimed secure fallback once gateway mode is introduced.

## 3. Authorization and revocation contract

### Device enrollment
- Preserve the existing account-bound Tor device registration and shared Tor/WireGuard device quota.
- OIDC enrollment derives account identity from verified issuer + subject.
- Invitation enrollment remains single-use and high entropy. After enrollment, exchange the invitation for a device-bound credential; do not use the invitation itself as a long-lived gateway password.
- Device credentials are scoped to one Tor device, one account, and the gateway audience. They cannot authorize WireGuard operations.

### Gateway session grant
Proposed API:
- `POST /v1/client/tor/devices/{deviceID}/gateway-grants`
- Requires authenticated OIDC or an approved device-bound session after invitation enrollment.
- Request contains no account/user ID, gateway address, role, entitlement, or client-selected expiry.
- Response returns a short-lived, audience-bound grant and the gateway connection metadata. Secret material is returned once and never stored in plaintext.
- Idempotency, replay handling, rate limits, token audience, expiry, issuer, key rotation, and revocation semantics must be documented before implementation.

A signed JWT alone is not sufficient for immediate revocation. The gateway must either perform online introspection for each new proxy session or use a bounded cache with authenticated invalidation and a documented maximum revocation delay. Fail closed if authorization state cannot be established.

### Administrator device controls
Proposed admin API:
- `GET /v1/admin/tor/devices` with account/status pagination and audit controls.
- `POST /v1/admin/tor/devices/{deviceID}/revoke` with a required reason code and idempotency key.

Authorization must use the existing operator role model. Account/device ownership must be checked server-side. Every mutation produces an audit event without raw credentials or traffic destinations.

### Active-session termination
Revocation must:
1. atomically mark the device revoked in the control plane;
2. publish an authenticated revocation event or make the gateway observe the new state within the defined SLA;
3. deny new gateway sessions immediately after that state is observed;
4. terminate all active gateway sessions associated with the device;
5. remain revoked after retries, process restarts, or stale idempotency replays.

Account suspension and entitlement expiry use the same gateway enforcement path.

## 4. Android changes required

- Replace the local embedded Tor SOCKS upstream with the authenticated gateway transport.
- Retain Hev only if the selected secure local-to-gateway adapter and pinned native version are compatible and tested.
- The current Hev SOCKS5 configuration supports optional username/password fields in upstream documentation, but that alone is not transport encryption. Never place reusable credentials in a plaintext remote SOCKS5 connection.
- Keep all gateway credentials in Android Keystore-backed storage, short-lived, device-scoped, and excluded from logs, crash reports, backups, and UI.
- Refresh authorization before starting and periodically while connected. On denial, expiry, account suspension, revocation, or gateway loss, stop the tunnel and fail closed.
- Do not report Connected until the gateway-authenticated path has passed an independent remote-domain probe through Tor and packet-level leak tests.
- Remove the local embedded Tor fallback only after migration and rollback behavior are explicitly designed; no direct-network fallback is permitted.

## 5. Gateway and operations requirements

- Gateway is a distinct service boundary, not an ordinary API handler. Separate its runtime identity and permissions from the control plane.
- Use encrypted transport with authenticated server identity; use client/device authentication that supports revocation. Do not rely on static shared passwords.
- Restrict gateway egress to the intended Tor process/network path. No direct destination egress fallback.
- Protect the gateway's Tor SOCKS/control ports; bind them to loopback/private interfaces only.
- Enforce connection/session limits, timeouts, bounded concurrency, abuse controls, and per-device resource quotas.
- Health checks must distinguish gateway process health, Tor bootstrap/circuit readiness, authorization-service reachability, and successful Tor-routed probe.
- Minimize logs: device pseudonymous ID, auth result, timestamps, byte counters, and coarse failure reason only. No destination history or traffic payload.
- Define capacity, bandwidth/cost ceilings, regional placement, HA, failover, DDoS exposure, incident response, backup scope, and rollback before provisioning.
- No AWS/VPS, DNS, firewall, security-group, database, node, or production resource changes are authorized by this design document.

## 6. Security and privacy threats

| Threat | Required control |
|---|---|
| Modified APK skips local checks | Mandatory gateway authorization; client checks are defense-in-depth only |
| Stolen grant | Short TTL, device scope, gateway audience, encrypted transport, revocation and replay controls |
| Cleartext SOCKS credentials | Prohibit remote plaintext SOCKS auth; use authenticated encrypted transport |
| Revoked device keeps active sessions | Gateway session registry and forced session termination |
| Control plane unavailable | Fail closed for new sessions; existing-session policy explicitly bounded |
| Gateway bypasses Tor | Network egress restrictions and packet-level tests; no direct fallback |
| Gateway logs destinations | No destination logging or request-body capture; verify logs and telemetry |
| Gateway compromise | Least privilege, isolation, key rotation, audit, patching, abuse limits |
| Invitation-only device cannot refresh | Explicit one-time invitation-to-device-auth exchange; no long-lived invitation token |

## 7. Acceptance gates

The architecture is not release-ready until all gates pass on the exact release commit:

1. API contract tests: account ownership, operator authorization, grant audience/expiry, invitation exchange, idempotency, replay, rate limits, account suspension and entitlement expiry.
2. PostgreSQL tests: additive migration, constraints, transactionality, shared quota concurrency, admin revocation, audit event atomicity, and restore test in disposable infrastructure.
3. Gateway tests: valid/invalid/expired/revoked grant, wrong audience, control-plane outage, cache invalidation, connection limits, active-session termination, process restart, and no direct egress.
4. Android tests: Keystore storage, refresh, denial, revoke while connected, process death, network handover, consent denial, API 24+ compatibility, no local-Tor fallback, no credential leakage.
5. Data-plane tests: IPv4 TCP, IPv6 policy, DNS, UDP/QUIC fail-closed, Tor exit IP, gateway failure, packet capture on the physical interface, and independent remote-domain probe.
6. Operational tests: load/capacity, metrics without destination data, alerting, rollback, and recovery.
7. Security review: threat model updated with real gateway implementation, dependency review, secrets scan, auth boundary review, and negative tests.

Unexecuted gates remain **NOT RUN**. Mocks do not count as live gateway or end-to-end evidence.

## 8. Open implementation decisions

These must be resolved from repository and deployment evidence before coding the gateway:
- Existing hosting/runtime target and its actual network topology.
- Whether the gateway will be a dedicated service or a dedicated role on an existing ZEUS node.
- Secure Android-to-gateway transport implementation and its maintained library/runtime.
- Whether the gateway authorization check is online per new session or uses a bounded cache with revocation events.
- Expected concurrency, throughput, bandwidth budget, regions, and service availability target.
- How existing invitation-only devices transition to device-bound gateway authentication.

Until these decisions and tests are complete, the existing embedded-Tor branch is a development foundation only; it is not server-enforced access control.
