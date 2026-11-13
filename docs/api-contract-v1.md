# ZEUS Control API — v1 Contract (Draft)

Base path: `/v1`. JSON is UTF-8. All timestamps are RFC 3339 UTC. UUIDs are canonical strings.

## Response conventions
Successful reads return a JSON object. Errors use:
```json
{
  "error": {
    "code": "invalid_request",
    "message": "Request could not be processed",
    "request_id": "opaque-correlation-id"
  }
}
```
Do not return stack traces, SQL errors, secrets, private keys, or internal host paths.

## Identity domains
- Operators: people who administer ZEUS. Roles: `owner`, `admin`, `support`, `read_only`.
- VPN users: customers or personal accounts that own devices. They are not operators.
- Node agents: machine identities, separate from both human domains.

## Planned read endpoints
- `GET /healthz` — process liveness; no dependency checks.
- `GET /readyz` — readiness, including required database availability once persistence is enabled.
- `GET /v1/meta` — product and API metadata.
- `GET /v1/users` — paginated user list; operator auth required.
- `GET /v1/users/{userID}` — user and device summary; operator auth required.
- `GET /v1/nodes` — node inventory; operator auth required.

## Planned mutation endpoints
- `POST /v1/users` — create a user.
- `PATCH /v1/users/{userID}` — update allowed profile/status fields.
- `POST /v1/users/{userID}/devices` — register a device and request peer allocation.
- `DELETE /v1/devices/{deviceID}` — revoke device and peer.
- `POST /v1/nodes/{nodeID}/heartbeat` — node-agent identity only.

Mutation requests must support an idempotency key. The server must validate role, ownership/scope, device limits, node status, and address allocation inside a transaction before issuing a node command.

## Authentication and authorization
- Human operator endpoints require a short-lived opaque bearer session.
- Store only a cryptographic hash of the session token; never log bearer tokens.
- Use Argon2id for operator password hashes; never store plaintext passwords.
- Node-agent endpoints use a distinct mutually authenticated credential and audience.
- A VPN user's client key is never accepted as an operator credential.
- Roles are enforced by the API, not by hiding dashboard controls.
- `read_only` can read operational metadata but cannot mutate state.
- `support` can view and perform explicitly scoped support actions; it cannot create operators or change node trust.
- `admin` manages users/devices; `owner` manages operators and security policy.

## Peer key handling
- Client private keys are generated and retained on the client device.
- Node private keys remain on the VPN node.
- Control-plane records contain public keys only.
- API responses never contain private keys or full server configuration secrets.

## Pagination and filtering
Use `limit` (default 25, maximum 100) and opaque `cursor`. Return `items` and `next_cursor`. Reject invalid filters instead of silently broadening a query.

## Status
This is a design contract. Only health, readiness, and metadata routes are implemented today. User, device, node, authentication, and peer endpoints must not be represented as live until their implementation and tests exist.
