# ZEUS Control API — v1 Contract

Base path: `/v1`. JSON is UTF-8. Timestamps are RFC 3339 UTC. IDs are UUIDs unless otherwise noted.

## Response conventions
Errors use a stable envelope with `error.code`, `error.message`, and `error.request_id`. Responses use `Cache-Control: no-store` and security headers. Stack traces, SQL errors, private keys, and internal host paths are never returned.

## Identity domains
- Operators administer ZEUS. Roles: `owner`, `admin`, `support`, `read_only`.
- VPN users own registered devices. They are separate from operator identities.
- Node agents are machine identities authenticated by mTLS.

## Operator endpoints
- `POST /v1/auth/login`: exchange login/password for an opaque bearer session.
- `GET /v1/auth/me`: return the current operator.
- `POST /v1/auth/logout`: revoke the current session.
- `GET /v1/users?limit=25&cursor=...`: cursor-paginated user list.
- `GET /v1/users/{userID}`: read one VPN user.
- `POST /v1/users`: create a VPN user.
- `PATCH /v1/users/{userID}`: update display name, status, or device limit.
- `GET /v1/users/{userID}/devices`: list the user's registered devices and associated peer profile.
- `POST /v1/users/{userID}/enrollment-code`: issue a one-time 256-bit client activation code (15-minute lifetime).
- `POST /v1/users/{userID}/devices`: operator-only direct registration (administrative/testing path).
- `POST /v1/users/{userID}/devices/{deviceID}/revoke`: enqueue peer removal for one device.
- `POST /v1/client/enroll`: public, rate-limited client registration using a one-time activation code and a client-generated WireGuard public key.

All operator endpoints require a bearer session. `owner` and `admin` may create/update users and register/revoke devices. `support` and `read_only` may read users and device lists but cannot mutate. Authorization is enforced by the API, not just by hiding UI controls.

## Client activation-code flow
- Owner/admin issues a 256-bit random code for an active VPN user. The raw code is returned once, expires after 15 minutes, and is never stored; PostgreSQL stores only its SHA-256 hash.
- Issuing a replacement code revokes the previous unconsumed code for that user.
- The client submits the code, a stable device reference, name, platform, and WireGuard public key to `POST /v1/client/enroll`. This endpoint does not accept or require an operator bearer token.
- The enrollment route is rate-limited by source IP. A code can provision only one device identity.
- Network retries are recoverable: repeating the same code with the same device reference and public key returns the existing device, even with a new idempotency key. A different identity is rejected.
- Code consumption, device/peer creation, `peer.ensure` queueing, idempotency response, and audit event commit transactionally.

## Device registration
Request body:
```json
{
  "device_ref": "client-generated-stable-reference",
  "device_name": "My Android",
  "platform": "android",
  "public_key": "base64-WireGuard-public-key"
}
```
- The client generates the WireGuard key pair. Only the public key is sent to the control plane; the private key remains on the client device. The Android client builds the final tunnel profile locally and saves it using Android Keystore-backed AES-GCM encryption.
- The node must be online, registered with an mTLS certificate fingerprint, and configured with explicit IPv4/IPv6 client pools and interface addresses.
- The user row is locked while the device limit is checked. Device, peer, `peer.ensure` command, idempotency response, and audit event are committed transactionally.
- IP allocation excludes the node interface addresses and all non-revoked peer assignments. The node-scoped unique indexes prevent concurrent duplicate assignments.
- New records start as `provisioning` / peer `pending`. A successful `peer.ensure` acknowledgement moves them to `active`. An error or expired command moves them to `error`.
- The response is HTTP 202 because the peer command is queued; it does not mean the tunnel is already active.

## Device revocation
- Revoke is asynchronous. The API moves the device and peer to `revoking` and enqueues a `peer.remove` command in the same transaction.
- Only a successful node acknowledgement marks the peer and device `revoked` and releases the assigned addresses for reuse.
- A failed or expired removal sets the device to `error`; its address remains reserved because the peer may still exist on the node.
- Repeating the same request with the same idempotency key returns the original result. A different request using an already-consumed key returns `409 idempotency_conflict`.

## Mutation safety
- Every create/update/revoke request requires an `Idempotency-Key` with 16–128 allowed characters.
- Keys are scoped to operator and route; SHA-256 request fingerprints detect payload conflicts.
- Idempotency responses expire after 24 hours.
- User/device/peer changes, command enqueue, audit event, and idempotency response use PostgreSQL transactions.
- User statuses are `active`, `suspended`, or `disabled`; device statuses are `provisioning`, `active`, `revoking`, `error`, or `revoked`.
- A suspended/disabled user cannot enroll new devices. Existing peers are not automatically disconnected: each device must be explicitly revoked and the node must acknowledge removal.
- The device limit counts `provisioning`, `active`, `revoking`, and `error` devices; only acknowledged `revoked` devices stop consuming a slot.

## Session and password security
- Operator passwords use Argon2id (64 MiB, 3 iterations, 2 lanes).
- Sessions are opaque 256-bit random bearer tokens; only SHA-256 hashes are stored.
- Sessions expire after eight hours and can be revoked server-side.
- The first owner is bootstrapped from environment variables. Existing accounts are not overwritten.
- Login errors are generic for unknown, disabled, or wrong-password accounts. Attempts are throttled per source IP and Argon2 work is concurrency-bounded.
- Browser SPA session tokens stay in memory only and are cleared on reload/logout.

## CORS and transport
- CORS is deny-by-default and accepts exact origins configured in `ZEUS_ALLOWED_ORIGINS`.
- Production database URLs require `sslmode=verify-full`.
- Production admin HTTP traffic must be behind a TLS reverse proxy and the application listener kept private.
- Forwarded client-IP headers are not trusted.

## Peer-key handling
- Client private keys are generated and retained on the client device.
- Node private keys remain on the VPN node.
- The control plane stores public keys and assigned addresses only.
- API responses never include private keys or server configuration secrets.

## Node-agent mTLS endpoints
On a separate listener, disabled unless the three node TLS files are configured:
- `GET /agent/v1/commands/next`: lease one command for the node identified by its verified client certificate.
- `POST /agent/v1/commands/{commandID}/ack`: acknowledge success/failure with a constrained error-code allowlist.
- TLS 1.2+, a verified ClientAuth certificate, non-CA leaf certificate, and exact SHA-256 certificate fingerprint mapping are required.
- Queue delivery is at least once, leased for 45 seconds, expires after a bounded period, and is capped at 10 attempts.
- The standalone node client/worker exists, and the local executor defaults to dry-run. Real WireGuard mutations require explicit node-side opt-in and staging validation.

## Not yet implemented
- Node certificate enrollment, rotation, and automated revocation tooling.
- Android app enrollment UI and end-to-end provisioning on a staging node.
- Operator lifecycle/password reset/role-management APIs.
- Billing, subscriptions, live traffic telemetry, and full audit-log UI.
- Production rollout and validation against the current AWS WireGuard host.
