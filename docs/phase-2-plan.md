# Phase 2 — Multi-user Control Plane

## Goal
Turn the Phase 1 foundation into a secure, multi-user management plane without changing the already-working WireGuard host until a separately reviewed deployment plan is approved.

## Guardrails
- Work only on `phase2-multiplayer-control-plane`; never push Phase 2 work to `main`.
- No production deployment, AWS resource changes, firewall changes, or live peer changes in this phase without explicit approval.
- Never store or return WireGuard private keys. The control plane stores peer public keys and assigned addresses only.
- Keep admin authentication separate from VPN customer accounts.
- Require authorization, validation, audit logging, and idempotency for every state-changing operation.
- Do not expose the dashboard publicly until authentication, authorization, TLS, and operational hardening are complete.

## Delivery sequence

### 2A — Data and API contracts (current milestone)
- Define PostgreSQL entities for operators, sessions, VPN users, devices, nodes, WireGuard peers, and audit events.
- Define API conventions, role boundaries, error envelopes, and node trust boundary.
- Review schema and migration strategy before applying it to any database.
- No database connection or mutation endpoint is enabled by this milestone.

### 2B — Control-plane persistence and operator authentication
- Add PostgreSQL connection management, readiness checks, migrations, and repository tests.
- Add password hashing (Argon2id), opaque short-lived sessions, token hashing at rest, revocation, rate limiting, and CSRF protection where cookie authentication is used.
- Enforce role-based authorization server-side.
- Add integration tests using an ephemeral PostgreSQL service in CI.

### 2C — User/device lifecycle
- Add transactional user and device operations, device limits, uniqueness checks, and immutable audit events.
- Add peer allocation with database uniqueness and idempotency.
- Do not change the host WireGuard configuration from the API process.

### 2D — VPN node agent
- Implement a separate least-privilege agent with a mutually authenticated control channel.
- Agent accepts only narrowly scoped, signed or mutually authenticated desired-state commands.
- Validate and apply peer changes atomically; report observed state and counters.
- Support credential rotation, revocation, replay protection, and bounded command expiry.

### 2E — Dashboard and Android integration
- Replace demo fixtures with typed API responses and explicit loading/error/offline states.
- Add operator login and permission-aware controls.
- Connect Android provisioning only after the API and node lifecycle have passed integration tests.
- Cache only encrypted, revocable client configuration required for offline reconnect.

## Exit criteria
- Unit, integration, race, static-analysis, and browser tests pass.
- Migrations are tested against a clean database and an upgrade fixture.
- Unauthorized and cross-user access tests fail closed.
- Audit records are created for successful and denied mutations.
- A staging node proves peer add/update/revoke and rollback without touching production.
- Android tunnel reconnect works with the control plane unavailable, using a previously provisioned local profile.

## Current status
- Branch created from Phase 1 commit.
- Milestone 2A schema and API contract are being established.
- No live server or AWS resource has been changed.
