# Phase 2 — Multi-user Control Plane

## Goal
Turn the Phase 1 foundation into a secure, multi-user management plane without changing the already-working WireGuard host until a separately reviewed deployment plan is approved.

## Guardrails
- Work only on `phase2-multiplayer-control-plane`; never push Phase 2 work to `main`.
- No production deployment, AWS resource changes, firewall changes, or live peer changes without explicit approval.
- Never store or return WireGuard private keys. The control plane stores peer public keys and assigned addresses only.
- Keep admin authentication separate from VPN customer accounts.
- Require authorization, validation, audit logging, and idempotency for every state-changing operation.
- Do not expose the dashboard publicly until authentication, authorization, TLS, and operational hardening are complete.

## Delivery sequence

### 2A — Data and API contracts
- [x] Define PostgreSQL entities for operators, sessions, VPN users, devices, nodes, WireGuard peers, and audit events.
- [x] Define API conventions, role boundaries, error envelopes, and node trust boundary.
- [x] Add a checksum-tracked migration runner with a PostgreSQL advisory lock.
- [x] Verify migration idempotency and schema application in CI.
- [x] Add an idempotency-record table with request fingerprints and 24-hour expiry.
- No migration has been applied to the production AWS database.

### 2B — Control-plane persistence and operator authentication
- [x] PostgreSQL connection pool and schema readiness checks.
- [x] Argon2id password hashing (64 MiB, 3 iterations, 2 lanes).
- [x] Opaque random 256-bit bearer sessions; only SHA-256 token hashes are stored.
- [x] Eight-hour expiry and server-side revocation.
- [x] First-owner bootstrap that never overwrites an existing account.
- [x] Exact-origin CORS allowlist, request IDs, security headers, bounded JSON bodies, and structured error envelopes.
- [x] Per-source-IP login throttling and bounded concurrent Argon2 work.
- [x] Server-side role checks for authenticated API reads and mutations.
- [x] Go unit and PostgreSQL integration tests in GitHub Actions.
- Remaining: password-reset workflow, operator lifecycle/role-management endpoints, distributed rate limiting, session cleanup, and deeper threat-model review.

### 2C — User lifecycle (partially implemented)
- [x] Transactional VPN-user create/read/update endpoints.
- [x] Owner/admin-only mutation authorization; support and read-only roles cannot mutate users.
- [x] Idempotency keys, payload fingerprints, safe replay, and conflict detection.
- [x] Mutation and audit event stored in the same database transaction.
- [ ] Device registration/revocation, device-limit enforcement, address allocation, and idempotent peer lifecycle.
- [ ] Immutable audit policy and audit-log viewing/retention controls.
- The API does not change host WireGuard configuration.

### 2D — VPN node agent (not implemented)
- Implement a separate least-privilege agent with a mutually authenticated control channel.
- Accept only narrowly scoped desired-state commands.
- Validate and apply peer changes atomically; report observed state and counters.
- Support credential rotation, revocation, replay protection, and bounded command expiry.

### 2E — Dashboard and Android integration (partially implemented)
- [x] Web operator login against the Go API.
- [x] Keep the bearer token in browser memory only; require login again after a page reload.
- [x] Load authenticated VPN users from PostgreSQL through the API, with cursor pagination.
- [ ] Connect the user create/edit forms to the authenticated API endpoints.
- [ ] Replace remaining demo server, traffic, subscription, and activity data with real APIs.
- [ ] Add operator role-management screens.
- [ ] Connect Android provisioning only after the API and node lifecycle have passed integration tests.
- [ ] Cache only encrypted, revocable client configuration required for offline reconnect.

## Exit criteria
- Unit, integration, race, static-analysis, and browser tests pass.
- Migrations are tested against a clean database and an upgrade fixture.
- Unauthorized and cross-user access tests fail closed.
- Audit records are created for successful and denied mutations.
- A staging node proves peer add/update/revoke and rollback without touching production.
- Android tunnel reconnect works with the control plane unavailable, using a previously provisioned local profile.

## Current status
- Branch: `phase2-multiplayer-control-plane`.
- Latest milestone: PostgreSQL-backed operator authentication and transactional VPN user management API.
- GitHub Actions validates Go, PostgreSQL migrations/integration tests, web build, and repository structure.
- No live server, AWS resource, firewall rule, or production VPN peer has been changed.
