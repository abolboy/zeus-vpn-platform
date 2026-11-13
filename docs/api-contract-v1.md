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

Operator routes require a short-lived opaque bearer session. Store only a cryptographic hash of the session token; never log bearer tokens. Sessions expire after eight hours. A revoked, expired, disabled, or unknown session is rejected. The browser SPA keeps the token in memory only and requires a new login after reload.

## Current authorization
- `owner`, `admin`, `support`, and `read_only` may read the current user list.
- No user/device/node mutation endpoint is enabled yet.
- Hiding a button in the dashboard is not authorization; the API checks roles server-side.

## Authentication and password storage
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

## Planned, not implemented
- `POST /v1/users`, `PATCH /v1/users/{userID}`, and `GET /v1/users/{userID}`.
- `POST /v1/users/{userID}/devices` and `DELETE /v1/devices/{deviceID}`.
- `GET /v1/nodes` and node-agent heartbeat/desired-state endpoints.
- Operator creation, role changes, password reset, billing, subscriptions, and peer provisioning.

Every future mutation must validate role, scope, device limits, node state, and address allocation inside a transaction, support idempotency, and create an audit event before any node command is issued.
