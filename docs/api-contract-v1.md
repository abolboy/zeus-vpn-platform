# ZEUS Control API — v1 Contract

Base path: `/v1`. JSON is UTF-8. Timestamps are RFC 3339 UTC. IDs are UUIDs unless otherwise noted.

## Response conventions
Errors use:
```json
{
  "error": {
    "code": "invalid_request",
    "message": "Request could not be processed",
    "request_id": "opaque-correlation-id"
  }
}
```
Do not return stack traces, SQL errors, secrets, private keys, or internal host paths. Responses set `Cache-Control: no-store` and security headers.

## Identity domains
- Operators: people who administer ZEUS. Roles: `owner`, `admin`, `support`, `read_only`.
- VPN users: customers or personal accounts that own devices. They are not operators.
- Node agents: machine identities, separate from both human domains.

## Implemented public endpoints
- `GET /healthz`: process liveness only; it does not prove database or VPN-node health.
- `GET /readyz`: database connectivity and required schema readiness.
- `GET /v1/meta`: product, API version, build version, and stage.

## Implemented operator endpoints
- `POST /v1/auth/login`: accepts `{"login":"...","password":"..."}`; returns a one-time bearer token, expiry, and public operator identity.
- `GET /v1/auth/me`: returns the authenticated operator identity.
- `POST /v1/auth/logout`: revokes the current bearer session.
- `GET /v1/users?limit=25&cursor=...`: authenticated, cursor-paginated user listing.
- `GET /v1/users/{userID}`: read one user record.
- `POST /v1/users`: create a VPN user.
- `PATCH /v1/users/{userID}`: update display name, status, or maximum device count.
- `GET /v1/users/{userID}`: read one user record.
- `POST /v1/users`: create a VPN user.
- `PATCH /v1/users/{userID}`: update display name, status, or maximum device count.

All user endpoints require an operator bearer session. `owner` and `admin` may create or update users. `support` and `read_only` are read-only. The API enforces permissions server-side.

## Mutation safety
- Every create/update requires an `Idempotency-Key` (16–128 characters from `A-Z a-z 0-9 . _ ~ -`).
- The server scopes keys to operator and route, stores a SHA-256 request fingerprint, and replays the original response for an identical request.
- Reusing a key with a different request body returns `409 idempotency_conflict`.
- Idempotency responses expire after 24 hours; expired entries are removed when the key is reused.
- User mutation, idempotency response, and audit event are committed in one PostgreSQL transaction.
- Display names are limited to 1–80 Unicode characters; device limit is 1–20; statuses are `active`, `suspended`, or `disabled`.
- A suspended/disabled user cannot enroll new devices, and max_devices is enforced during enrollment. Changing user status does not automatically revoke devices already provisioned; revoke those devices explicitly and wait for node acknowledgement.
- No device, peer, node, payment, subscription, or WireGuard host mutation endpoint is implemented.

## Session and password security
Operator routes require a short-lived opaque bearer session. Store only a cryptographic hash of the session token; never log bearer tokens. Sessions expire after eight hours. A revoked, expired, disabled, or unknown session is rejected. The browser SPA keeps the token in memory only and requires a new login after reload.
- Operator passwords use Argon2id with 64 MiB memory, 3 iterations, and 2 lanes.
- Session tokens contain 256 bits of cryptographic randomness; only SHA-256 token hashes are stored.
- The first owner is created from bootstrap environment variables. Existing accounts are never overwritten by bootstrap.
- Login returns a generic credential error for unknown, disabled, or wrong-password accounts.
- Login attempts are throttled per source IP; Argon2 work is concurrency-bounded.
- Do not put production passwords in source control, browser storage, or logs.

## CORS and transport
- CORS is deny-by-default and accepts only exact origins configured in `ZEUS_ALLOWED_ORIGINS`.
- Production requires the database URL to use `sslmode=verify-full`.
- Production HTTP traffic must be terminated by a TLS reverse proxy; keep the application listener private.
- The service does not trust forwarded client-IP headers.

## Peer key handling
- Client private keys are generated and retained on the client device.
- Node private keys remain on the VPN node.
- Control-plane records contain public keys only.
- API responses never contain private keys or full server configuration secrets.

## Pagination
Use `limit` (default 25, maximum 100) and an opaque base64url cursor. Return `items` and `next_cursor`. Reject invalid filters or cursors instead of silently broadening a query.

## Node-agent mTLS endpoints (separate listener)
- `GET /agent/v1/commands/next`: leases a command for the node identified by its verified mTLS certificate; 204 means no command is ready.
- `POST /agent/v1/commands/{commandID}/ack`: acknowledges succeeded/failed with a constrained error-code allowlist.
- The separate TLS listener requires ClientAuth certificates, rejects CA certificates, checks the ClientAuth EKU, and maps the SHA-256 leaf-certificate fingerprint to one non-disabled node row.
- The listener is disabled unless the three node TLS environment settings are configured. Default bind is loopback.
- Queue delivery is at least once with a 45-second lease, expiry, 10-attempt ceiling, and idempotent acknowledgement.
- The standalone node client and network-triggered WireGuard execution are not implemented yet. No command is dispatched to the current AWS VPN node.

## Planned, not implemented
- Device register/revoke endpoints and enforcement against WireGuard peer lifecycle.
- Node inventory, node-agent heartbeat/desired-state endpoints, and live traffic telemetry.
- Operator creation, role changes, password reset, billing, subscriptions, and peer provisioning.

Every future mutation affecting devices or nodes must validate role, scope, device limits, node state, and address allocation inside a transaction, support idempotency, and create an audit event before any node command is issued.
