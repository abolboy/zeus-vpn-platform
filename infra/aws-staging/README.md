# ZEUS isolated AWS staging foundation

This CloudFormation template creates an isolated staging network and compute/database foundation. It does **not** install ZEUS binaries, configure Nginx/systemd, issue certificates, create customer records, or mutate WireGuard state. Those are separate, reviewable deployment steps.

## Resources

- Dedicated VPC with two public and two private subnets across two Availability Zones.
- Internet Gateway and one NAT Gateway. Both private subnets currently route outbound through the NAT Gateway in the first public subnet; this is a staging cost/availability trade-off, not a multi-AZ NAT design.
- Public Application Load Balancer, HTTPS-only listener, and a private Control Plane EC2 instance.
- Dedicated staging WireGuard EC2 instance in a public subnet. Only its configured WireGuard UDP port is open inbound; no SSH ingress is created. Source/destination checking is disabled for VPN forwarding.
- Private PostgreSQL RDS instance, encrypted storage, seven-day backups, managed master password, and deletion protection enabled.
- Separate SSM-enabled IAM roles for Control Plane and VPN node. The Control Plane role can read the RDS-managed master secret and generated `zeus_app` runtime and `zeus_migrator` migration secrets; the VPN node role cannot read database secrets.
- Private Route 53 hosted zone for the node-to-Control-Plane mTLS name.

The template defaults to Ubuntu Server 24.04 LTS amd64. It uses a small EC2 baseline and a Multi-AZ PostgreSQL 16.15 instance by default. The engine minor version is pinned to 16.15 to match the PostgreSQL 16 test baseline; confirm regional availability before creating the stack. Instance classes, database storage, and Multi-AZ behavior are parameters.

## Mandatory preflight

Do not create the stack until all checks below are complete:

1. Confirm the target AWS account ID and region. The historical ZEUS region is `us-east-2`, but re-check the account selector and region in the AWS Console.
2. Confirm the staging VPC CIDR and all four subnet CIDRs do not overlap the production VPC, office/home LANs, existing VPN pools, or any connected network. The default `10.240.0.0/16` is only a proposal.
3. Obtain a public DNS name that you control for the staging API. Request/validate an ACM certificate for that exact hostname in the same AWS region.
4. Confirm the AWS account has the service quotas needed for two EC2 instances, RDS PostgreSQL, an ALB, NAT Gateway, EIP, and the selected Availability Zones.
5. Confirm the account is verified and CloudFormation can create resources. CloudShell is not required; the template can be reviewed and a change set can be created through the Console.
6. Review the change set for every resource and estimate. This design intentionally includes recurring NAT Gateway, ALB, RDS Multi-AZ, EC2, EIP, storage, and data-transfer charges.
7. Keep the staging account/VPC separate from production. Do not add VPC peering, shared subnets, cross-environment routes, or production database/Cognito references.

The template has no defaults for the public hostname or ACM certificate ARN. It pins RDS to PostgreSQL 16.15, the latest 16.x minor listed in the AWS RDS release notes retrieved on 2026-11-18; confirm that exact minor is offered in the selected region before stack creation. The certificate must be in the same region and cover `PublicApiHostname`.

## Create a reviewable change set

From an authorized AWS CLI session, first validate the template:

```sh
aws cloudformation validate-template \
  --region us-east-2 \
  --template-body file://infra/aws-staging/zeus-staging-foundation.yaml
```

Then create (but do not execute) a change set after replacing the two required values and verifying the CIDRs:

```sh
aws cloudformation create-change-set \
  --region us-east-2 \
  --stack-name zeus-staging-foundation \
  --change-set-name initial-staging-review \
  --change-set-type CREATE \
  --template-body file://infra/aws-staging/zeus-staging-foundation.yaml \
  --capabilities CAPABILITY_IAM \
  --parameters \
    ParameterKey=PublicApiHostname,ParameterValue=staging-api.example.com \
    ParameterKey=AcmCertificateArn,ParameterValue=arn:aws:acm:us-east-2:123456789012:certificate/00000000-0000-0000-0000-000000000000 \
    ParameterKey=VpcCidr,ParameterValue=10.240.0.0/16
```

The example hostname, account ID, certificate ARN, and CIDR are placeholders, not real ZEUS values. Add explicit overrides for subnet CIDRs if the defaults overlap any existing network.

Inspect the proposed resources and replacement/deletion actions:

```sh
aws cloudformation describe-change-set \
  --region us-east-2 \
  --stack-name zeus-staging-foundation \
  --change-set-name initial-staging-review
```

**Do not execute the change set until the operator has reviewed and explicitly approved it.** This repository change does not authorize AWS provisioning.

## Outputs and follow-up configuration

After an approved stack creation, record these outputs without publishing secrets:

- `PublicApiUrl` and `LoadBalancerDnsName`: create an external DNS A/ALIAS record for the exact public hostname to the ALB. The ACM certificate must match that hostname.
- `ControlPlanePrivateIp` and `ControlPlanePrivateMtlsUrl`: issue/install a separate internal server certificate whose SAN includes the private mTLS DNS name. The node agent uses this private endpoint over the VPC.
- `NodePublicIp`, `NodePrivateIp`, and `NodeInstanceId`: the staging VPN node endpoint and SSM target.
- `DatabaseEndpoint`, `DatabasePort`, `DatabaseSecretArn`, `DatabaseAppSecretArn`, and `DatabaseMigratorSecretArn`: run `scripts/bootstrap-rds-app-role.sh` on the isolated Control Plane host to create/update separate `zeus_app` and `zeus_migrator` logins and write root-only runtime/migration environment files. The script retrieves secrets through the Control Plane instance role and does not print passwords. Never copy any secret into Git, user data, command history, or CI logs.

The private mTLS listener must be configured on the Control Plane's private interface at TCP 8443. The public ALB terminates HTTPS and forwards to the Control Plane HTTP listener on TCP 8080 over the private VPC. The service must bind to `0.0.0.0:8080` (or the exact private interface address); the Control Plane security group permits this port only from the ALB security group. The Node Agent connects to the private Route 53 name on TCP 8443 using the dedicated node ClientAuth certificate; the Control Plane independently verifies the node certificate.

## Operational caveats

- This stack does not deploy the ZEUS applications or run database migrations. The ALB target health check uses `/readyz`, which verifies the Control Plane can reach PostgreSQL; expect it to remain unhealthy until the binary, schema, service configuration, listener, and database connectivity are ready.
- The node security group allows the WireGuard UDP port from the Internet. Restrict the source CIDR to known test-client addresses if those addresses are stable; otherwise retain the minimum required UDP exposure and monitor it.
- The VPN node has `SourceDestCheck=false`, but forwarding sysctls, nftables/iptables forwarding, NAT, WireGuard interface configuration, and peer pools still require a separate reviewed configuration.
- The RDS master secret is generated and managed by RDS. Separate Secrets Manager secrets generate the `zeus_app` runtime and `zeus_migrator` migration passwords. The Control Plane instance role can read all three secrets for bootstrap; the Node role receives SSM permissions only. This is a host-level trust boundary, not IAM isolation between processes on the Control Plane instance.
- SSM is the intended administrative channel. No inbound SSH rule is created.
- The Control Plane private subnet uses one NAT Gateway in AZ A for outbound access and SSM. Loss of that AZ interrupts private-subnet egress until recovery. The ALB spans two AZs; RDS is Multi-AZ by default, but the `DatabaseMultiAZ` parameter can disable that behavior.
- RDS deletion protection is enabled and the resource has a snapshot deletion policy. Teardown requires an explicit reviewed update to disable deletion protection, a verified final snapshot, and a second explicit operator approval.
- Do not point staging at production Cognito, database, certificates, node registry, or WireGuard interfaces.

## Teardown

There is intentionally no automatic cleanup script. Before teardown, export test evidence, verify a final database snapshot, review all staging resources, using `Project=ZEUS` and `Environment=staging` tags wherever the resource type supports tagging, and confirm the stack contains no production resources. Disable RDS deletion protection only through a separately reviewed CloudFormation update, then take and verify the final snapshot. Delete only this named staging stack after explicit approval.
