# ZEUS Tor-only migration DevOps and rollout plan

Status: Additive migration 0008 and Tor API persistence are committed to the feature branch. CI and disposable-PostgreSQL migration checks are pending. No deployment authorized or performed.
Delivery target: Draft PR #28, branch feat/android-tor-transport-pr27.

## Environment boundary

- Development: feature branch and CI only.
- Test: disposable PostgreSQL, Android emulator/device, isolated Tor test path.
- Production: AWS/VPS, control-plane database, node agents, WireGuard peers and customer data remain unchanged.

## CI requirements

1. Go control-plane unit/integration tests, migrations and OIDC test provider. Current HEAD CI is queued; queued is not PASS.
2. Android unit tests, debug/release builds, lint and merged-manifest inspection.
3. Hev native ABI/API compatibility checks for minSdk 24 and supported ABI.
4. Emulator VPN lifecycle tests; distinguish from physical-device network/leak tests.
5. Associate every result with exact commit SHA. Queued/in-progress is not PASS.
6. Do not publish/distribute release artifacts without explicit approval.

## Database migration

- Additive migration 0008 adds Tor client devices and Tor invitations; the existing account-scoped client idempotency table is reused under a distinct route key.
- Test fresh install and upgrade from every supported migration baseline.
- Test rollback on disposable DB; prefer forward-fix if rollback risks authorization data.
- Readiness schema checks now require both Tor tables; startup remains blocked until migration 0008 is applied in the target environment.
- Ensure quotas count both Tor and WireGuard devices under one transaction/lock.
- Never run production migration without explicit deployment approval and verified backup/restore plan.

## Release and rollback

- Keep PR draft until Android VpnService, Hev data path, account API, security and QA gates pass.
- Deploy additive API before app release; old clients continue using existing WireGuard endpoints.
- Do not silently change /v1/client/enroll.
- Roll back Android app if service stability or leak tests fail. Keep additive tables while Tor clients depend on them.
- No production deploy, AWS/VPS, firewall, DNS or peer mutation is part of this work without separate approval.

## Monitoring

Track registration outcomes, rate limits, revocations, Tor bootstrap stage, service restart counts, Hev failures and anonymized connectivity outcomes. Never log OIDC tokens, invitation codes, SOCKS/control credentials, destination domains, DNS query contents or traffic payloads.
