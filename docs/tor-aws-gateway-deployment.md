# ZEUS Tor Gateway on AWS — deployment decision and runbook

Status: **AWS selected as the target hosting platform. No AWS resources have been inspected or changed in this work.**
Related architecture: [tor-server-enforced-access-architecture.md](tor-server-enforced-access-architecture.md)
Related rollout plan: [tor-devops-rollout-plan.md](tor-devops-rollout-plan.md)
Compatibility constraint: the existing WireGuard service must remain operational and unchanged.

## 1. Hosting decision

AWS is the selected platform for the ZEUS-managed Tor gateway.

The exact placement is **not yet verified** because this repository/session has no live AWS inventory evidence for account, region, EC2 instance IDs, operating system, instance size, VPC/subnets, security groups, existing WireGuard service placement, or access method.

Preferred low-operations deployment, subject to read-only capacity and topology checks:
- If the existing WireGuard EC2 host has sufficient spare CPU, memory, disk, bandwidth, and acceptable failure-domain risk, deploy Tor as a separate systemd-managed service on that same host.
- Keep Tor gateway, Tor daemon, and authorization adapter isolated from WireGuard using dedicated unprivileged Linux service accounts, separate systemd units, separate configuration/state/log directories, explicit resource limits, and loopback/private-only Tor SOCKS/control listeners.
- Do not change WireGuard interface, keys, peer state, AllowedIPs, listen port, routing, firewall rules, or existing systemd unit as part of Tor installation.
- If capacity, isolation, or availability checks fail, use a separate small EC2 instance in the same AWS region/VPC where practical. This costs more but avoids resource contention and reduces blast radius.

This is a placement policy, not permission to modify AWS. Co-location is not assumed safe until the actual host is inspected.

## 2. Why this is not identical to installing WireGuard

WireGuard is a kernel VPN interface with peer keys and UDP transport. The Tor gateway requires:
- a Tor daemon and controlled SOCKS interface;
- an authenticated device authorization layer;
- an encrypted Android-to-gateway transport;
- per-device session tracking and forced termination on revocation;
- strict egress controls so gateway traffic cannot bypass Tor;
- privacy-preserving operational telemetry and resource limits.

A standalone Tor daemon or public SOCKS port is **not** sufficient and must never be exposed as an unauthenticated public proxy.

The Android-to-gateway transport remains a design prerequisite. SOCKS5 username/password is not transport encryption. Do not deploy reusable credentials over plaintext SOCKS5. The final transport must provide authenticated encryption and a revocation-aware device identity.

## 3. AWS network model

Target topology, to be mapped against actual AWS inventory before implementation:

- Existing ZEUS control plane remains the authority for account, device, and entitlement state.
- Tor Gateway has a distinct runtime identity and only the minimum control-plane access required for authorization.
- Public ingress exposes only the selected authenticated encrypted gateway transport. The Tor SOCKS and Tor control ports bind to loopback or a private interface and are never public.
- Tor destination egress is restricted to the Tor daemon's required network behavior. Gateway application code must not open direct destination connections as a fallback.
- Existing WireGuard ingress and routes remain untouched.
- Do not assume the gateway should share the WireGuard security group. Review ingress/egress rules and the actual VPC path first.
- Avoid introducing a NAT Gateway or Network Load Balancer without cost and topology justification. Public IP/EIP, load balancer, NAT, data transfer, and log storage can materially affect monthly cost.

No port number, CIDR, security-group rule, public IP, DNS name, IAM role, or region is selected in this document.

## 4. Service isolation on a co-located EC2 host

If co-location passes the capacity and risk gate:
- Run separate services for Tor daemon, gateway transport/authentication, and any local adapter; use distinct unprivileged service users.
- Store configuration and state under dedicated directories with restrictive ownership/modes.
- Keep secrets in AWS-supported secret storage or protected systemd credentials; never in Git, AMIs, user-data plaintext, command history, or logs.
- Use systemd sandboxing where compatible: NoNewPrivileges, PrivateTmp, ProtectSystem, ProtectHome, restricted writable paths, capability bounding, and resource controls. Validate Tor's required networking before enabling restrictive syscall or namespace settings.
- Set bounded CPU, memory, file-descriptor, connection, and restart policies; alert before saturation.
- Keep Tor control and SOCKS listeners off public interfaces.
- Do not edit or restart the WireGuard unit. A Tor deployment/restart must not interrupt WireGuard.

If the host is not suitable, deploy a separate EC2 service boundary rather than weakening these controls.

## 5. Read-only preflight required

Before selecting same-host versus separate-host, collect:
1. AWS account identity, caller ARN, and selected region.
2. EC2 instance IDs, state, instance type, architecture, AMI/OS, attached storage, and current CPU/network metrics.
3. VPC, subnet, route table, public/private address classification, security-group IDs and effective rules.
4. WireGuard service location, systemd unit name, interface, listen port, routes, firewall ownership, and current health; never collect private keys.
5. Available memory/disk, current CPU/load, network throughput, and expected Tor bandwidth/concurrency.
6. Current access path (SSM preferred where already configured; otherwise existing approved SSH path) and rollback/recovery access.
7. Existing control-plane endpoint and secure connectivity from the intended gateway host.

Do not print credentials, private keys, OIDC tokens, or secret values. Redact public IPs and account identifiers in shared logs if not necessary.

## 6. Deployment and rollback gates

No deployment until:
- exact target instance and environment are verified;
- current WireGuard health and baseline metrics are recorded;
- Tor and gateway binaries/images are pinned and scanned;
- service users, filesystem paths, ports, IAM permissions, and egress policy are reviewed;
- backup/recovery of only the new Tor configuration/state is defined;
- resource/cost estimates and alert thresholds are approved;
- the exact AWS change (if any) and rollback are reviewed and explicitly approved.

Rollout sequence after approval:
1. Validate IaC and artifacts without applying them.
2. Install Tor and gateway into isolated paths without touching WireGuard.
3. Start Tor daemon with private-only SOCKS/control listeners and verify bootstrap.
4. Start gateway with authenticated encrypted transport and online device authorization.
5. Test invalid/revoked/expired credentials, active-session termination, and no direct-egress behavior.
6. Test Android data path and packet capture; do not show Connected until end-to-end Tor-routed probe passes.
7. Observe resource use and WireGuard handshake/traffic before and after; rollback Tor-only units if any WireGuard regression occurs.

Rollback must stop/disable only Tor-related units and remove only their dedicated ingress rule/config after approval. Never flush firewall rules or reset network configuration. Never delete or rewrite WireGuard configuration, keys, peers, or state.

## 7. Cost and operational notes

Potential cost drivers:
- EC2 compute and EBS (especially if a second instance is required);
- public IPv4/EIP and load balancer if selected;
- data transfer out and sustained Tor bandwidth;
- CloudWatch logs/metrics retention;
- NAT Gateway, if introduced;
- snapshots and backups.

Do not claim the lowest-cost layout until actual region, instance type, expected concurrency, bandwidth, and current AWS pricing are known. Add AWS Budget alerts before sustained public service exposure.

## 8. Current status

- AWS target platform: **SELECTED by user**.
- Same EC2 as WireGuard: **PREFERRED ONLY IF READ-ONLY PREFLIGHT PASSES**.
- Separate EC2 fallback: **REQUIRED if co-location fails capacity/isolation gates**.
- AWS account/region/EC2/VPC/SG inventory: **NOT AVAILABLE / NOT VERIFIED**.
- Gateway, secure transport, grant API, admin revocation, active-session termination: **NOT IMPLEMENTED**.
- AWS resources, IAM, networking, security groups, DNS, hosts, databases, WireGuard services/peers: **UNCHANGED**.
