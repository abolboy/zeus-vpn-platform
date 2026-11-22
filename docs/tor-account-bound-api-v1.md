# ZEUS Tor device API contract v1

Decision: Account-bound device registration; Tor transport is independent of WireGuard peer provisioning.
Status: Initial API/database implementation, Android Tor enrollment repository, Tor-only dashboard, and ZEUS-owned VpnService host are on the feature branch. CI, disposable-database and end-to-end data-plane verification remain pending.

## Invariants

1. Every managed Tor installation belongs to one active ZEUS account.
2. Ownership comes only from verified OIDC issuer+subject or a single-use Tor invitation. Never accept client user_id.
3. Tor registration creates an authorization/device record only. It must not create a WireGuard peer, reserve a WireGuard node address, or enqueue peer.ensure.
4. The Tor Android client never generates, stores, uploads, or requests a WireGuard key/profile.
5. Device limits are account-wide across Tor and existing WireGuard devices.
6. Existing WireGuard enrollment routes and server infrastructure remain compatible.
7. The control plane stores no Tor SOCKS credentials, circuit keys, traffic content, or destination history.

## Register a Tor device

POST /v1/client/tor/devices

Authentication:
- Authorization: Bearer <OIDC ID token>; or
- Authorization: EnrollmentCode <single-use Tor invitation>.

Headers:
- Idempotency-Key: 16..128 chars from [A-Za-z0-9._~-]
- Content-Type: application/json
- Accept: application/json

Request, maximum 8 KiB:
{
  "device_ref": "random-installation-uuid",
  "device_name": "Android device",
  "platform": "android",
  "client_version": "0.2.0"
}

Validation:
- device_ref: 16..128 characters, random per installation, unique within account across transport types; both enrollment transactions check both device tables while holding the same account-row lock.
- device_name: 1..80 Unicode characters, existing display-name policy.
- platform: initially android only.
- client_version: bounded version/build string; reject control characters and oversized values.
- Strict JSON decoding; reject unknown fields.
- No public/private key, IP, node ID, endpoint, or account ID accepted.

Success: 201 Created; an identical replay returns the original 201 response with Idempotency-Replayed: true; a changed request or invitation under the same key returns 409 idempotency_conflict.

Response:
{
  "id": "uuid",
  "device_ref": "random-installation-uuid",
  "device_name": "Android device",
  "platform": "android",
  "transport": "tor",
  "status": "active",
  "created_at": "RFC3339 timestamp",
  "entitlement": { "status": "active", "expires_at": null }
}

The handler requires an active account. Current response entitlement is `active` with null expiry; subscription/expiry policy is not yet integrated. Client input cannot set entitlement.

## Read and revoke

GET /v1/client/tor/devices/{deviceID}
- Bearer OIDC: resolve account from verified issuer+subject and scope query to that account.
- Rate limit: 30 requests per source IP per minute.
- EnrollmentCode is not accepted for status reads; use the verified account session.
- Foreign and unknown IDs both return 404 device_not_found.

POST /v1/client/tor/devices/{deviceID}/revoke
- Bearer OIDC only; account-scoped mutation.
- Rate limit: 30 requests per source IP per minute.
- Idempotent; already-revoked device returns 200.
- Revoked device cannot be reactivated by replaying old idempotency key or invitation.

## Current revocation and entitlement gap

The current implementation is not yet a complete remote device-revocation control:

- The Tor API has customer OIDC status and self-revoke routes, but no administrator Tor-device list or revoke route.
- The Android VPN start path checks for a locally stored device ID; it does not verify current server-side device status or account entitlement before starting Tor.
- Invitation-code enrollment is supported, but status and revoke require OIDC. An invitation-only installation cannot currently perform those account operations until it signs in with OIDC.
- `entitlement.status` is currently returned as `active` with `expires_at: null`; subscription expiry and runtime entitlement enforcement are not integrated.

Therefore, admin-initiated revocation of a lost device, immediate enforcement of account suspension, and expiry-based disconnect are NOT IMPLEMENTED. Do not describe these as supported controls. Before release, define an authenticated admin revoke/list contract, a client policy for status refresh and fail-closed behavior, and—if resistance to a modified client is required—a server-controlled data-plane enforcement point. A client-only status check is not a security boundary against a modified APK.

## Administrator invitation

POST /v1/users/{userID}/tor-enrollment-code

Use existing operator authentication/role authorization and equivalent high-entropy, short-lived, single-use code policy. Store only SHA-256 hash. Do not overload vpn_user_enrollment_codes.device_id because its FK points to vpn_devices, not Tor devices.

## Database design

Add tor_client_devices:
- UUID primary key, user_id FK to vpn_users;
- device_ref, device_name, platform, client_version;
- status constrained to active/revoked;
- created_at, updated_at, nullable revoked_at;
- unique (user_id, device_ref);
- index (user_id, status).

Add the separate `tor_client_enrollment_codes` table; reuse `client_device_idempotency_records` with route `POST:/v1/client/tor/devices`. Do not store Tor IDs in the WireGuard-only invitation FK.

Shared device quota must be transactional:
- lock vpn_users row;
- verify account active;
- count active Tor devices plus WireGuard devices in slot-reserving states: provisioning, active, revoking, error;
- compare max_devices;
- insert device, consume invitation if used, persist idempotent response, write audit event in one transaction.

Existing WireGuard createVPNDeviceTx also counts active Tor devices under the same account lock, preventing parallel cross-transport enrollments from exceeding quota.

## Stable errors

customer_identity_invalid, customer_identity_not_configured, customer_account_inactive, enrollment_code_invalid, enrollment_code_expired, enrollment_code_used, invalid_device_ref, invalid_device_name, invalid_client, device_limit_reached, device_conflict, idempotency_key_required, idempotency_conflict, device_not_found, rate_limited.

Use existing safe error envelope and request ID. Never return SQL, stack traces, topology, token hashes, or raw invitation codes.

## Security requirements

- Preserve OIDC issuer, audience, signature, expiry, subject, and verified-email checks.
- Never trust user_id, transport, status, or entitlement supplied by client.
- Scope every read/write to authenticated account in SQL.
- Use per-account idempotency request hashes and lock account row before quota check.
- Hash invitation codes at rest; redact Authorization headers; never log tokens/codes.
- Enrollment body is capped at 8 KiB and decoded with strict JSON validation; enrollment, status and revocation requests are rate-limited by source IP and audit events are recorded.
- Revocation is atomic; replay cannot restore revoked state.
- Tor SOCKS/control ports remain loopback-only.

## Rollout

1. Additive migration 0008 and initial API/store implementation are in the feature branch; validate migration and quota concurrency against disposable PostgreSQL. No production migration in this branch.
2. Add new endpoints alongside existing WireGuard routes.
3. Add contract tests for OIDC, invitation, idempotency, quotas, ownership.
4. Android Tor enrollment repository, Tor-only dashboard, and ZEUS-owned VpnService host are present on the draft branch; they are not release-ready.
5. Verify the VpnService + Hev data plane, SOCKS exit probe, and leak tests before any release or user-facing Connected state.
6. Remove Android WireGuard dependency only after no references remain and merged manifest/build checks pass.
7. Keep server-side WireGuard support until separately deprecated.

Non-goals: server-side Tor relay, AWS/VPS/firewall/DNS/node-agent changes, production database changes.


## Server-enforced gateway access — accepted architecture, not yet implemented

The selected security model is a mandatory ZEUS-controlled Tor egress gateway. The Android app is not an authorization boundary. A modified APK may bypass client checks; only a gateway that every ZEUS-managed session must traverse can enforce ZEUS account/device access.

The gateway protects access to ZEUS's managed Tor service. It cannot prevent a user or modified APK from independently connecting to public Tor or unrelated third-party proxies outside ZEUS.

See [tor-server-enforced-access-architecture.md](tor-server-enforced-access-architecture.md) for the target data path, threat model, operational requirements, and release gates.

Proposed future API surface (NOT IMPLEMENTED):
- `POST /v1/client/tor/devices/{deviceID}/gateway-grants`: issue a short-lived, device-bound, gateway-audience grant after account/device/entitlement checks.
- `GET /v1/admin/tor/devices`: operator-authorized, paginated device inventory.
- `POST /v1/admin/tor/devices/{deviceID}/revoke`: operator-authorized, audited, idempotent revocation.

A signed token by itself does not provide immediate revocation. The gateway must check online authorization for each new proxy session or use a bounded cache with authenticated invalidation and a documented maximum revocation delay. Active sessions must be tracked and terminated after revocation. Control-plane or authorization uncertainty must fail closed.

Invitation enrollment must exchange the one-time invitation for a device-bound credential; the invitation must never become a long-lived gateway secret. Gateway credentials must be returned once, scoped to one device and audience, encrypted in transit, and never logged or persisted in plaintext.

Hev upstream SOCKS5 username/password support does not encrypt the SOCKS5 transport. Do not connect Hev directly to a public plaintext SOCKS5 gateway with reusable credentials. A maintained authenticated encrypted transport/adapter is a prerequisite and remains an implementation decision.
