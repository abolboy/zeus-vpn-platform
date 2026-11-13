# Phase 2 — Multi-user Control Plane

## Goal
Build a secure multi-user management plane without changing the already-working WireGuard host until a separately reviewed production deployment is approved.

## Guardrails
- Work only on \`phase2-multiplayer-control-plane\`; never push Phase 2 work to \`main\`.
- No production deployment, AWS resource changes, firewall changes, or live peer changes without explicit approval.
- Never store or return WireGuard private keys. Control-plane records hold peer public keys and assigned addresses only.
- Keep operator identities separate from VPN customer identities.
- Require authorization, validation, audit logging, and idempotency for each mutation.
- Do not expose admin or node-agent listeners publicly before TLS, network isolation, and operational hardening are complete.

## 2A — Data and contracts
- [x] PostgreSQL schema for operators, sessions, VPN users, nodes, devices, peers, audit events, idempotency records, and node commands.
- [x] Checksum-tracked migrations with a PostgreSQL advisory lock.
- [x] CI verification of migration idempotency.
- [x] Node certificate fingerprint registration and bounded command queue schema.
- No migration has been applied to the production AWS database.

## 2B — Control-plane persistence and operator authentication
- [x] PostgreSQL connection pool and readiness checks.
- [x] Argon2id password hashing (64 MiB, 3 iterations, 2 lanes).
- [x] Opaque random 256-bit bearer sessions; only SHA-256 token hashes are persisted.
- [x] Eight-hour expiry and server-side revocation.
- [x] First-owner bootstrap that never overwrites an existing account.
- [x] Exact-origin CORS, request IDs, security headers, bounded JSON bodies, structured errors.
- [x] Login throttling and bounded concurrent password verification.
- [x] Server-side role checks for user reads and mutations.
- [x] PostgreSQL integration tests in CI.
- Remaining: password reset, operator lifecycle and role management, distributed rate limits, session cleanup, expanded threat model.

## 2C — User lifecycle (partially implemented)
- [x] Transactional create/read/update user API.
- [x] Owner/admin-only mutation rights; support/read-only are read-only.
- [x] Idempotency keys, request fingerprints, safe replay, conflict detection.
- [x] Mutation and audit event in one database transaction.
- [x] Web create/edit forms wired to authenticated API.
- [ ] Device registration/revocation and actual device-limit enforcement.
- [ ] WireGuard peer desired-state workflow and address allocation.
- Account suspension blocks new device enrollment but does not automatically revoke existing peers. Explicit device revocation is asynchronous and completes only after a successful node acknowledgement. The device limit is enforced during enrollment.

## 2D — Node agent and WireGuard executor
- [x] Separate optional mTLS listener for node commands.
- [x] Verified ClientAuth certificate-to-node identity binding.
- [x] Durable command leasing and constrained acknowledgement endpoints.
- [x] Outbound mTLS node HTTP client with CA verification, redirects/proxy disabled, bounded response sizes, and strict decoding.
- [x] Command worker for peer.ensure and peer.remove with expiry checks, acknowledgement, and bounded backoff.
- [x] Local WireGuard executor with dry-run by default, fixed argv, pool validation, no-follow config reads, atomic writes, and rollback tests.
- [x] Unit, TLS, and PostgreSQL queue tests in GitHub Actions.
- [ ] Certificate enrollment, rotation, and automated revocation.
- [ ] Connect operator/device changes to transactional peer command creation.
- [ ] Real isolated-interface test, staging VPS test, failure injection, and reboot recovery.
- [ ] Production rollout approval.

## 2E — Dashboard and Android integration
- [x] Web operator login/logout; bearer token kept in memory.
- [x] Authenticated, cursor-paginated user listing.
- [x] Owner/admin user create/edit flows.
- [ ] Real node inventory, live telemetry, subscriptions, billing, and audit log UI.
- [ ] Connect Android device provisioning only after the end-to-end node lifecycle passes staging tests.
- [ ] Ensure existing tunnels reconnect from encrypted local profiles while the control plane is unreachable.

## Exit criteria
- Unit, integration, race, static analysis, and browser tests pass.
- Migrations are tested on clean databases and upgrade fixtures.
- Unauthorized and cross-node access tests fail closed.
- Device lifecycle, address allocation, command queue, and peer state remain consistent.
- Staging node proves add/update/revoke, replay, rollback, restart, and reboot persistence.
- Android reconnect works without a live control-plane connection, using a previously provisioned local profile.

## Current status
- Branch: \`phase2-multiplayer-control-plane\`.
- Latest scope: operator/user management plus initial mTLS node command transport and dry-run-first local executor.
- No AWS server, security group, main branch, or production WireGuard peer has been changed.
