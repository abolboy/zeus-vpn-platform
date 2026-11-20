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
- [x] One-time first-owner bootstrap serialized with a PostgreSQL advisory lock; it does not create a second active owner or overwrite an existing password.
- [x] Exact-origin CORS, security headers, bounded JSON bodies, request IDs, structured errors.
- [x] Client IP and IP-based rate-limit attribution trust X-Forwarded-For only from configured ALB subnet CIDRs.
- [x] Login throttling and bounded concurrent password verification.
- [x] Server-side role checks.
- [x] PostgreSQL integration tests, race detector, and static analysis in CI.
- Remaining: password reset, operator lifecycle/role APIs, distributed rate limits, expanded threat model.

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
- [x] One-time 256-bit enrollment codes with 15-minute expiry, hashed storage, replacement revocation, and safe retry recovery.
- [x] Public client enrollment endpoint that accepts only the client-generated WireGuard public key; rate-limited and idempotent.
- Account suspension blocks new enrollment but does not automatically revoke already-registered devices; revoke each device explicitly.

## 2D — Node agent and WireGuard executor
- [x] Separate optional mTLS listener and certificate-to-node identity binding.
- [x] Durable node command queue, leases, expiry, and constrained acknowledgements.
- [x] Outbound mTLS client with CA verification, redirects/proxy disabled, bounded response sizes, and strict JSON decoding.
- [x] Worker for `peer.ensure` / `peer.remove`, expiry checks, acknowledgements, and bounded backoff.
- [x] Local WireGuard executor with dry-run by default, fixed argv, pool validation, no-follow config reads, atomic writes, and rollback tests.
- [x] Offline per-node ClientAuth certificate issuance, node-bound certificate registration, and owner/admin revocation endpoints.
- [x] Manual certificate rotation sequence documented; overlapping valid fingerprints are supported during replacement.
- [ ] Automated certificate expiry monitoring, rotation orchestration, and revocation alerts.
- [x] Hardened systemd templates added for the Control Plane, migration runner, and VPN node; the node unit is limited to CAP_NET_ADMIN and writes only to /etc/wireguard. All three units deny EC2 IMDS IPv4/IPv6 endpoints with systemd IPAddressDeny (cgroup v2 required).
- [x] Staging environment examples and a deployment/rollback runbook added under deploy/ and docs/staging-deployment.md.
- [ ] Verify service ownership, permissions, mTLS reachability, and peer lifecycle on an isolated staging VPS.
- Production node mutations remain disabled until staging gates pass.

## 2E — Dashboard and Android integration
- [x] Web operator login/logout with in-memory bearer token.
- [x] Authenticated paginated user listing and owner/admin create/edit flows.
- [x] User device list with peer state, assigned addresses, and endpoint details.
- [x] Owner/admin-triggered device revocation with idempotent retry behavior.
- [x] Android-generated WireGuard keypair, public-key enrollment over HTTPS, local profile construction, and Android Keystore AES-GCM persistence.
- [x] Encrypted pending-enrollment recovery with a stable device identity and idempotency key after a lost response.
- [ ] End-to-end enrollment and tunnel handshake against a deployed Control Plane and isolated staging WireGuard node.
- [x] Live Android download/upload throughput chart calculated from actual WireGuard byte-counter deltas.
- [x] Web admin refreshes authenticated node/user inventory every 30 seconds when auto-refresh is enabled; an open device list is refreshed too.
- [x] Node inventory now reports a previously online node as offline after 90 seconds without an mTLS heartbeat and exposes last-seen time.
- [x] mTLS certificate inventory distinguishes revoked, not-yet-valid, expiring-within-30-days, expired, and malformed validity windows; an open certificate list refreshes with the 30-second auto-refresh cycle.
- [x] Control Plane removes operator sessions in bounded batches after 30 days of expiry/revocation retention, on startup and every 15 minutes, with clean shutdown and PostgreSQL integration coverage.
- [x] Removed misleading sample traffic, subscription-plan, and activity numbers from the connected admin dashboard; unsupported telemetry/entitlements are shown as unavailable.
- [ ] Node RX/TX telemetry ingestion, server load/latency metrics, subscriptions, billing, and production audit-log API/screens.
- [x] Previously provisioned encrypted local profile remains available for reconnect without Control Plane access.
- [ ] Verify reconnect and sustained traffic after process death, network changes, and device reboot on a physical Android device.

## Exit criteria
- Unit/integration/race/static/browser checks pass.
- Migrations pass clean and repeated application.
- Unauthorized and cross-node access fail closed.
- Staging proves add/remove, replay, address reuse, rollback, restart, and reboot persistence.
- Android reconnect works without a reachable Control Plane using a previously provisioned profile.

## Current status
- Current delivery chain: PR #21 (customer lifecycle/node-agent integration) → PR #22 (process-level mTLS enrollment test) → PR #23 (isolated staging architecture) → PR #24 (AWS staging foundation) → PR #26 (database runtime/migration role separation) → PR #27 (integrated Android split tunneling, coroutine cancellation safety, and truthful web-admin status). Keep all PRs Draft and unmerged until the full dependency chain and release gates are reviewed.
- PR #25 was closed as superseded, not merged. PR #26 is its synchronized replacement: it adds a dedicated Secrets Manager secret and PostgreSQL login for `zeus_migrator`, keeps `zeus_app` away from schema CREATE, resets existing and default runtime grants to DML plus sequence USAGE/SELECT only (no TRUNCATE/REFERENCES/TRIGGER or UPDATE/setval), separates systemd environment files, avoids exposing database passwords in process arguments during verification, and blocks IMDS access from service cgroups.
- PR #26 head `cda18983a446ba3ba2db00ce87a5fb832d7ef01d` passed ZEUS CI (run `44428217505`), Repository checks (`44428217248`), and PostgreSQL Migration Check (`44428217133`). PR #22's current head `96dbcf2a8f25a598df269bdbd44e68e7eaa10a7b` passed ZEUS CI (`44445197963`) and Repository checks (`44445198388`). PR #27 composes the Android/web workstreams on top of PR #26; its CI must be green on the latest PR #27 head after every further change. These checks do not prove a live AWS deployment or a real VPN tunnel.
- The default Android build has no Control Plane URL/OIDC values. Automatic customer registration requires a deployed HTTPS Control Plane and matching Cognito provider outputs.
- Staging systemd units and environment examples are templates only; they have not been installed on any host. Node mutations remain disabled by default.
- Blocked on AWS account/CloudShell activation and a separately approved staging deployment: create/inspect the isolated stack, deploy Cognito and PostgreSQL, install binaries/systemd units, establish public TLS and private mTLS, enroll the staging node, run real peer add/remove, and verify VPN egress/DNS/IPv6.
- PR #27 integrates source work from PRs #2 (split tunneling), #6 (Coroutine cancellation propagation), and #17 (web-admin live-state truthfulness); those source PRs remain open, Draft, and unmerged. The integration PR must be reviewed as a whole because the Android changes overlap in the dashboard.
- Requires a physical Android device test: verify process-death recovery, network changes, sustained traffic, force-stop, and reboot using a real staging tunnel.
- Requires product/operations decisions before implementation: password-reset delivery and recovery policy, operator lifecycle/role-management rules, distributed rate-limit backend, subscription/billing policy, telemetry retention/collection, audit-log access policy, and certificate rotation/revocation notification channel. The dashboard already displays certificate validity warnings; it does not yet automate rotation or send external alerts.
- No AWS resource, firewall/security group, production WireGuard peer, production database, or `main` branch was changed.
