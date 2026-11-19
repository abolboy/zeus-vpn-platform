# ADR-0003: Isolated AWS staging environment

- **Status:** Proposed — not approved for provisioning
- **Date:** 2026-11-18
- **Owners:** ZEUS platform maintainers
- **Depends on:** the application and deployment contracts in the current integration branch

## Context

The repository now has a process-level CI test that exercises customer activation-code enrollment, PostgreSQL persistence, mTLS node authentication, the real Node Agent process, command leasing, acknowledgement, and the resulting active device/peer state. Its WireGuard executables are test doubles, so it does not prove kernel peer installation, public routing, NAT, DNS, or real VPN traffic.

The AWS account currently has no CloudFormation stacks reported by the operator. The two identified ZEUS EC2 instances are the existing Control Plane and VPN node and must be treated as production. They are not staging capacity. CloudShell is also unavailable while AWS account verification is pending.

## Decision proposal

Create a separate staging environment that has no network path or shared mutable data with production.

Preferred isolation order:

1. A separate AWS account dedicated to staging, if account ownership and billing setup permit.
2. Otherwise, a dedicated VPC in the existing AWS account, with separate subnets, security groups, IAM roles, database, secrets, DNS name, and Cognito User Pool. Do not peer this VPC with the production VPC.

Do not repurpose either existing ZEUS EC2 instance and do not point staging at the production database, Cognito User Pool, certificates, or WireGuard interface.

### Logical topology

- **Control Plane host:** a dedicated Linux EC2 instance. Expose HTTPS on TCP 443 through its TLS reverse proxy. Keep the application listener on loopback TCP 8080.
- **VPN node host:** a second dedicated Linux EC2 instance running the Node Agent and a real, isolated WireGuard interface. Expose only the chosen WireGuard UDP port to intended test clients.
- **Database:** a dedicated PostgreSQL 16 database, private and not publicly accessible. Permit TCP 5432 only from the staging Control Plane security group. An RDS deployment is preferred for managed backups; a local PostgreSQL instance is a lower-cost alternative only if the operator explicitly accepts host-level backup and recovery responsibility.
- **Node control channel:** TCP 8443 on the Control Plane, permitted only from the staging VPN node security group or its fixed private address. The node validates the Control Plane server certificate; the Control Plane validates the node ClientAuth certificate and its node identity.
- **Customer identity:** a staging-only Cognito User Pool and public Authorization Code client if OIDC is enabled. Never reuse production users or app-client credentials.
- **Administration:** use AWS Systems Manager Session Manager. Do not open inbound SSH. Attach least-privilege instance profiles with SSM access; grant secret read access only to the service that needs each secret.

### Network and firewall constraints

- Use separate staging VPC CIDRs and verify that they do not overlap production, operator/client LANs, or the VPN client pools.
- Keep PostgreSQL in private subnets with no public IP and no public ingress.
- Do not expose TCP 8080 or the database to the Internet.
- Public ingress to the Control Plane is TCP 443 only. The mTLS listener is restricted to the staging node.
- Public ingress to the VPN node is limited to the WireGuard UDP port required for the tunnel test. No administrative ports are public.
- Disable EC2 source/destination checks only on the staging VPN node after the routing design is reviewed. Configure forwarding, firewall, and NAT only for the staging client prefixes.
- Do not create VPC peering, Transit Gateway routes, shared subnets, or security-group rules that permit staging-to-production access.

### Identity, secrets, and key custody

- Use separate IAM roles for Control Plane and VPN node. Avoid wildcard secret access and do not put credentials in user data, AMIs, Git, CI logs, or shell history.
- Store database credentials and service secrets in an approved secret store with resource-scoped access.
- Generate the node CA private key offline/on an encrypted administrator workstation. Do not copy the CA private key to either EC2 host.
- Transfer only the per-node client certificate/private key to the staging node and the required public CA bundles to their respective trust consumers. Keep private keys root-owned and mode 0600.
- Use a staging-only DNS name and a valid TLS certificate. Do not reuse production TLS private keys.

## Deployment prerequisites — unresolved inputs

Provisioning is blocked until the following are confirmed:

- AWS account and region to use (the existing project region has historically been `us-east-2`; re-verify in the console before any action).
- Whether a separate AWS account is available, or whether same-account VPC isolation is accepted.
- Maximum monthly staging budget and automatic cost-alert threshold.
- DNS name/subdomain controlled by the operator and certificate issuance method.
- Whether PostgreSQL will be RDS or host-local; backup retention and restore owner.
- Instance sizes, operating-system image, storage sizes, and expected test window.
- Whether OIDC/Cognito is in scope for the first staging pass or activation-code enrollment is sufficient.
- IPv4/IPv6 client pools and WireGuard UDP port, checked against all existing networks.

Do not infer these values from the production hosts or hard-code them into a template.

## Acceptance gates

A staging environment is considered provisioned only after all of the following are recorded:

1. Resource inventory and tags identify every resource as staging; no production instance or database is in the deployment plan.
2. Security-group review proves the port restrictions above and no staging-to-production route exists.
3. Database connectivity works only from the Control Plane; migration backup/restore is rehearsed on staging data.
4. The Control Plane is healthy and ready; its admin HTTPS certificate validates.
5. A staging node authenticates with mTLS using a node-bound certificate; invalid, expired, and revoked certificates are rejected.
6. One staging customer/device is enrolled; the real Node Agent receives and acknowledges `peer.ensure`.
7. `wg show` on the staging node confirms the peer and a recent handshake from a test client.
8. IPv4 forwarding, NAT, DNS, and outbound HTTPS are verified from the client. IPv6 is either proven end-to-end or protected by an explicit leak-prevention policy.
9. Peer removal, retry/replay, agent restart, Control Plane restart, and host reboot are tested.
10. The rollback procedure restores the staging WireGuard configuration and database from verified backups.

CI success alone does not satisfy these gates.

## Cost and teardown controls

- Require a budget owner and monthly limit before provisioning.
- Add a budget alert before creating billable resources.
- Prefer single-AZ staging and small instance/storage sizes only after the workload and budget are approved.
- Avoid NAT Gateway, load balancers, and always-on managed services unless the design requires them; their recurring costs must be reviewed explicitly.
- Tag every resource with `Project=ZEUS`, `Environment=staging`, and an owner tag.
- Define a teardown checklist that first exports required test evidence, then removes only resources tagged for this staging environment. Never use broad account-wide cleanup commands.

## Infrastructure implementation status

A review-only CloudFormation foundation is maintained at `infra/aws-staging/zeus-staging-foundation.yaml`, with operator guidance in `infra/aws-staging/README.md`. It defines the isolated VPC, public HTTPS ALB, private Control Plane, dedicated WireGuard node, private PostgreSQL, managed RDS secret, SSM roles, and private mTLS DNS. It intentionally does not deploy application binaries, configure host services, issue certificates, or execute WireGuard changes. The template must pass repository CloudFormation linting and receive a separate operator review before any change set is executed.

## Rollout sequence

1. Approve the account/isolation model, budget, DNS, database choice, and network CIDRs.
2. Author and lint an infrastructure-as-code template with explicit parameters and safe defaults.
3. Review the generated change set before any stack creation.
4. Provision only after explicit operator approval; verify the resulting resource inventory.
5. Deploy application binaries/configuration through SSM or another audited channel; do not place secrets in instance user data.
6. Execute the staging acceptance gates and capture sanitized evidence.
7. Keep staging separate from production. Production deployment requires its own explicit approval and rollback plan.

## Consequences

This design adds infrastructure and recurring cost, but gives the team a place to verify actual WireGuard kernel behavior and network traffic without risking production. It intentionally leaves the concrete AWS template and provisioning unimplemented until the unresolved inputs and budget are approved.
