# Phase 2 — Multi-user Control Plane

## Goal
Deliver authenticated multi-user management while keeping the already-working production WireGuard node unchanged until a separate staging and production approval process is complete.

## Guardrails
- Work on `phase2-multiplayer-control-plane`; do not merge to `main` as part of this phase.
- No production deployment, AWS resource changes, firewall changes, or live peer changes without separate explicit approval.
- Never store or return WireGuard private keys. Client private keys remain on device; node private keys remain on nodes.
- Operator, VPN user, and node-agent identities are separate.
- Every mutation requires role authorization, validation, idempotency, and audit logging.

## 2A — Data and contracts
- [x] PostgreSQL schema for operators, sessions, users, nodes, devices, WireGuard peers, audit events, idempotency records, and node commands.
- [x] Checksum-tracked migrations with an advisory lock and repeated-application CI checks.
- [x] Explicit per-node IPv4/IPv6 pools and interface addresses.
- [x] Node-scoped unique address indexes that retain assignments until peer removal is acknowledged.
- No migration has been applied to the production AWS database.

## 2B — Persistence and operator authentication
- [x] PostgreSQL connection pool/readiness checks.
- [x] Argon2id password hashing and opaque random 256-bit sessions (only SHA-256 token hashes stored).
- [x] Eight-hour expiry and server-side revocation.
- [x] First-owner bootstrap that never overwrites an existing account.
- [x] Exact-origin CORS, security headers, bounded JSON bodies, request IDs, structured errors.
- [x] Login throttling and bounded concurrent password verification.
- [x] Server-side role checks.
- [x] PostgreSQL integration tests, race detector, and static analysis in CI.
- Remaining: password reset, operator lifecycle/role APIs, distributed rate limits, session cleanup, expanded threat model.

## 2C — User and device lifecycle
- [x] Transactional VPN user create/read/update API.
- [x] Owner/admin-only user and device mutations; support/read-only access is read-only.
- [x] Idempotency keys, payload fingerprints, safe replay, and conflict detection.
- [x] Transactional device registration with user device-limit enforcement.
- [x] Per-node IPv4/IPv6 allocation that excludes interface addresses and existing non-revoked peers.
- [x] Device creation and `peer.ensure` command enqueue in one transaction.
- [x] Device revocation and `peer.remove` command enqueue in one transaction.
- [x] Device/peer state updates after node acknowledgements; acknowledged removal releases addresses.
- [x] PostgreSQL integration test exercises registration, limit, address allocation, ensure, revoke, replay, and address reuse.
- Account suspension blocks new enrollment but does not automatically revoke already-registered devices; revoke each device explicitly.

## 2D — Node agent and WireGuard executor
- [x] Separate optional mTLS listener and certificate-to-node identity binding.
- [x] Durable node command queue, leases, expiry, and constrained acknowledgements.
- [x] Outbound mTLS client with CA verification, redirects/proxy disabled, bounded response sizes, and strict JSON decoding.
- [x] Worker for `peer.ensure` / `peer.remove`, expiry checks, acknowledgements, and bounded backoff.
- [x] Local WireGuard executor with dry-run by default, fixed argv, pool validation, no-follow config reads, atomic writes, and rollback tests.
- [ ] Node certificate enrollment, rotation, and automated revocation tooling.
- [ ] Real isolated-interface and staging-VPS peer add/remove tests, crash recovery, and reboot persistence.
- [ ] Review and minimize the service's OS privileges before enabling mutations.
- Production node mutations remain disabled until staging gates pass.

## 2E — Dashboard and Android integration
- [x] Web operator login/logout with in-memory bearer token.
- [x] Authenticated paginated user listing and owner/admin create/edit flows.
- [x] User device list with peer state, assigned addresses, and endpoint details.
- [x] Owner/admin-triggered device revocation with idempotent retry behavior.
- [ ] Android-generated keypair/device registration and profile delivery.
- [ ] Real server inventory, live telemetry, subscriptions, billing, and audit log screens.
- [ ] Ensure tunnel reconnect from encrypted local profiles while Control Plane is unavailable.

## Exit criteria
- Unit/integration/race/static/browser checks pass.
- Migrations pass clean and repeated application.
- Unauthorized and cross-node access fail closed.
- Staging proves add/remove, replay, address reuse, rollback, restart, and reboot persistence.
- Android reconnect works without a reachable Control Plane using a previously provisioned profile.

## Current status
- Branch: `phase2-multiplayer-control-plane`.
- Current verified scope: authenticated user management, device lifecycle, node command queue, mTLS node transport, and a web device-management view.
- No AWS server, security group, production WireGuard peer, or `main` branch was changed.
