# ZEUS Tor-only migration DevOps and rollout plan

Status: Additive migration 0008, Tor API persistence, Android enrollment repository, Tor-only dashboard, and VpnService host are committed to the feature branch. CI and disposable-PostgreSQL migration checks are pending. No deployment authorized or performed.
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


## Addendum — gateway-based server enforcement (2026-11-22)

The accepted target architecture now includes a mandatory ZEUS-controlled Tor egress gateway. This is a new service boundary and is not covered by the original Android-only rollout sequence.

Before provisioning any gateway:
- Select and verify the hosting target, account/project, region, network path, and owner.
- Complete a capacity/bandwidth and cost estimate, ingress exposure review, egress restrictions, TLS identity/rotation plan, and rollback design.
- Confirm the gateway can reach only its intended Tor daemon path for destination traffic; there must be no direct-egress fallback.
- Define the control-plane authorization protocol, online-check/cache behavior, revocation propagation SLA, active-session termination, and fail-closed behavior.
- Define observability that excludes destination history, DNS query content, traffic payloads, and reusable credentials.
- Build and test the service in CI and an isolated environment before any host provisioning.

No AWS/VPS resource, DNS record, security group, firewall, database, Tor node, or production service was created or modified. Hosting target is currently **UNSELECTED / NEEDS USER DECISION**. The detailed contract and gates are in [tor-server-enforced-access-architecture.md](tor-server-enforced-access-architecture.md).


## AWS hosting choice (2026-11-22)

AWS is now the user-selected target platform. The exact EC2 placement is still unverified because no live AWS account/region/instance/VPC/security-group inventory is available in this workflow.

Preferred low-operations option: co-locate Tor Gateway as isolated systemd services on the existing WireGuard EC2 host only if read-only capacity, network topology, and blast-radius checks pass. Keep separate service users, units, directories, resource limits, and private-only Tor SOCKS/control listeners. Do not modify WireGuard interfaces, keys, peers, AllowedIPs, routes, ports, firewall rules, or systemd unit. If co-location fails those checks, use a separate EC2 instance.

See [tor-aws-gateway-deployment.md](tor-aws-gateway-deployment.md) for preflight, isolation, network, cost, deployment, and rollback gates. AWS resource changes remain unapproved and unperformed.
