# ZEUS staging deployment runbook

This runbook prepares an isolated staging environment. It is not approval to deploy to the production VPN node. The Control Plane and PostgreSQL should run on a separate management host/database; the VPN node agent runs on a separate staging WireGuard host.

## Network layout and trust boundaries

- Admin/browser traffic: public HTTPS ALB -> TCP `8080` on the private Control Plane interface. The Control Plane security group permits this port only from the ALB security group.
- Client IP attribution and IP-based rate limits: set `ZEUS_TRUSTED_PROXY_CIDRS` to the exact ALB subnet CIDRs (the template defaults are `10.240.0.0/24,10.240.1.0/24`). The Control Plane uses the final address appended to `X-Forwarded-For` only when the immediate peer belongs to one of these CIDRs. If subnet parameters change, update the environment file to match. Do not trust forwarded headers from arbitrary peers.
- Node-agent traffic: VPN node -> TCP `8443` (separate mTLS listener). Restrict this port to the exact staging node source IPs, or bind to a private VPC address when available.
- PostgreSQL: private network only; allow access from the Control Plane host only.
- WireGuard data plane: UDP on the configured WireGuard listen port at the VPN node. Do not expose the Control Plane admin port to the Internet.

The Control Plane must validate the public TLS certificate presented by its mTLS listener. The node agent's `ZEUS_NODE_SERVER_CA_FILE` must trust that certificate chain. Separately, `ZEUS_NODE_CA_FILE` on the Control Plane must trust the client certificates issued to VPN nodes. These two directions are distinct trust settings.

## 1. Build on the target Linux architecture

Build on each target host (or use a verified matching Go toolchain/architecture). Do not copy private keys or populated environment files from the repository.

Control Plane host:

```sh
cd services/control-plane
go build -trimpath -o /tmp/zeus-control-plane ./cmd/zeus-control-plane
go build -trimpath -o /tmp/zeus-migrate ./cmd/zeus-migrate
```

Staging VPN node:

```sh
cd services/vpn-node
go build -trimpath -o /tmp/zeus-vpn-node ./cmd/zeus-vpn-node
go build -trimpath -o /tmp/zeus-node-cert ./cmd/zeus-node-cert
```

Install the resulting binaries as root-owned, non-writable executables, for example mode `0755` under `/opt/zeus/bin`. Verify their architecture with `file` before starting them.

## 2. Prepare the Control Plane host

Create a dedicated service account and directories:

```sh
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin zeus
sudo install -d -o root -g root -m 0750 /opt/zeus/bin
sudo install -d -o root -g zeus -m 0750 /etc/zeus
sudo install -d -o root -g zeus -m 0750 /etc/zeus/node-listener
```

Install the Control Plane and migration binaries under `/opt/zeus/bin`. Install `deploy/systemd/zeus-control-plane.service` as `/etc/systemd/system/zeus-control-plane.service` and `deploy/systemd/zeus-migrate.service` as `/etc/systemd/system/zeus-migrate.service`.

Copy `deploy/examples/control-plane.env.example` to `/etc/zeus/control-plane.env`, replace all placeholders, and use mode `0600` owned by root. Keep it unreadable by ordinary users; the database URL is generated separately in `/etc/zeus/database.env`.

Requirements before startup:
- A dedicated PostgreSQL database and least-privilege role.
- PostgreSQL TLS validation with `sslmode=verify-full`; the hostname in the connection URL must match the database certificate.
- Exact browser origin(s) in `ZEUS_ALLOWED_ORIGINS`; do not use `*`.
- A deployed Cognito User Pool and its exact issuer/client ID outputs if automatic customer registration is enabled.
- A server certificate and matching private key for the node mTLS listener. The private key must be a regular file owned by `zeus`, mode `0600`, because the service checks for owner-only permissions. The certificate and client-CA bundle must be readable by `zeus`. The `/etc/zeus` and `/etc/zeus/node-listener` directories must be traversable by the `zeus` group; keep the environment file root-only.
- A firewall rule for TCP `8443` allowing only the registered staging node source IPs. Prefer a private VPC address in `ZEUS_NODE_HTTP_ADDR` instead of `0.0.0.0:8443` when possible.

The staging ALB is the TLS reverse proxy for the admin API. Set `ZEUS_HTTP_ADDR=0.0.0.0:8080` so the target is reachable over the private VPC interface; the Control Plane security group must continue to allow TCP 8080 only from the ALB security group. Do not add a public ingress rule for port 8080.
Keep `ZEUS_TRUSTED_PROXY_CIDRS` limited to the ALB's public subnet CIDRs. The application ignores `X-Forwarded-For` when the direct TCP peer is outside those CIDRs, preventing clients from spoofing their rate-limit and audit source IP.

## 2A. Prepare customer identity (Cognito)

The template `infra/aws-cognito/zeus-customer-user-pool.yaml` creates an Essentials-tier User Pool, an app-specific Managed Login branding resource with Cognito-provided defaults, a public Authorization Code client, and refresh-token rotation. It deliberately does not enable Plus-tier threat protection. Review the AWS feature-tier pricing and the public self-sign-up exposure before deploying it.

Deploy only to the dedicated staging AWS account and region:

```sh
aws cloudformation validate-template \
  --template-body file://infra/aws-cognito/zeus-customer-user-pool.yaml
aws cloudformation deploy \
  --template-file infra/aws-cognito/zeus-customer-user-pool.yaml \
  --stack-name zeus-vpn-customer-identity-staging
aws cloudformation describe-stacks \
  --stack-name zeus-vpn-customer-identity-staging \
  --query 'Stacks[0].Outputs'
```

Record the exact `OidcIssuer`, `OidcClientId`, and `HostedUiDomain` outputs. Configure the Control Plane and Android build with the same issuer/client pair. Never put a client secret in the Android package. Confirm the managed-login authorization endpoint responds before building the configured APK. Do not enable production public signup until abuse controls, capacity policy, email delivery, and monitoring are approved.

## 2B. Prepare PostgreSQL credentials and RDS certificate trust

For the CloudFormation RDS deployment, do not run the application with the RDS master account. The stack creates a separate generated password secret for `zeus_app`; the Control Plane instance role can read that secret and the RDS-managed master secret, while the VPN node role cannot read either one.

On the isolated Control Plane host, ensure the AWS CLI, `jq`, `psql`, and `openssl` are installed. Confirm the instance is using the staging Control Plane role through IMDSv2/SSM; do not copy access keys to the host.

Download the AWS RDS CA bundle and install it as a public trust file:

```sh
sudo install -d -o root -g root -m 0755 /etc/zeus/rds
curl --fail --location --proto '=https' --tlsv1.2 \
  https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
  --output /tmp/zeus-rds-global-bundle.pem
openssl crl2pkcs7 -nocrl -certfile /tmp/zeus-rds-global-bundle.pem -out /dev/null
sudo install -o root -g root -m 0644 \
  /tmp/zeus-rds-global-bundle.pem /etc/zeus/rds/global-bundle.pem
rm -f /tmp/zeus-rds-global-bundle.pem
```

Run the bootstrap script from the exact reviewed repository commit on the Control Plane host. Supply the literal RDS endpoint and both secret ARNs from the staging CloudFormation outputs:

```sh
sudo bash scripts/bootstrap-rds-app-role.sh \
  --region us-east-2 \
  --db-host '<DatabaseEndpoint>' \
  --master-secret-arn '<DatabaseSecretArn>' \
  --app-secret-arn '<DatabaseAppSecretArn>'
```

The script retrieves both secrets through the instance role, creates or updates the non-superuser `zeus_app` login, grants only database connection and schema/table creation rights needed by the explicit migration runner, and writes `/etc/zeus/database.env` as root-owned mode `0600`. It does not print either password. It refuses to overwrite an existing database environment file; preserve and review any existing file before deciding how to rotate it. The script requires the exact RDS endpoint hostname so `sslmode=verify-full` validates both the RDS CA chain and hostname.

Both the migration and Control Plane systemd units load this generated file. Do not start migrations or the API until the script reports success and the file exists with the expected owner and permissions.

## 3. Apply the database migrations

Back up the empty/dedicated staging database first. Then run the migration binary using the same root-only environment file as the service:

```sh
sudo systemctl daemon-reload
sudo systemctl start zeus-migrate
sudo systemctl status zeus-migrate --no-pager
sudo journalctl -u zeus-migrate -n 100 --no-pager
```

Do not point this command at an unrelated database. The application does not auto-migrate schema at startup.

For the first startup only, uncomment both bootstrap variables in the environment file and supply a unique strong password. Start the service, confirm the owner account was created, then immediately remove both bootstrap variables and restart the service. The bootstrap operation does not overwrite an existing login.

## 4. Start and verify the Control Plane

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now zeus-control-plane
sudo systemctl status zeus-control-plane --no-pager
curl --fail --silent http://127.0.0.1:8080/healthz
curl --fail --silent http://127.0.0.1:8080/readyz
```

`/healthz` proves process liveness only; `/readyz` checks PostgreSQL connectivity with a bounded timeout. Schema migrations remain a separate explicit step. Also inspect `journalctl -u zeus-control-plane` and verify the mTLS listener is bound only to the intended interface.

## 5. Enroll the staging VPN node

1. Confirm the staging WireGuard interface, endpoint, public key, interface IPs, and exact IPv4/IPv6 client pools. Verify there is no overlap with existing peers, VPC routes, or host networks.
2. Create a node record in the ZEUS admin dashboard using those exact values.
3. Generate the node-agent CA and per-node ClientAuth certificate on an encrypted/offline administrator workstation with `zeus-node-cert`. Never copy the CA private key to either server.
4. Register the public node certificate through the authenticated node-certificate endpoint for the exact node record. The Control Plane checks its chain, ClientAuth EKU, expiry, and node URI identity.
5. On the staging node, create `/etc/zeus-node` as root-owned mode `0700`.
6. Transfer only the node client certificate, its matching private key, and the Control Plane server CA trust bundle to that directory. Keep the client private key root-owned mode `0600`.
7. Copy `deploy/examples/vpn-node.env.example` to `/etc/zeus/vpn-node.env`; replace the Control Plane URL, trust paths, and client pools with values confirmed on the staging host. Keep it root-owned mode `0600`.
8. Install `deploy/systemd/zeus-vpn-node.service` as `/etc/systemd/system/zeus-vpn-node.service`.

The server TLS certificate chain and the node client certificate are not interchangeable. The client certificate is used by the node to authenticate to the Control Plane; the Control Plane's server certificate is validated by the node against `ZEUS_NODE_SERVER_CA_FILE`.

## 6. Dry-run and mutation gate

The node agent defaults to dry-run. With `ZEUS_NODE_ALLOW_MUTATIONS=false`, commands are validated but peer operations are not applied and are acknowledged as `mutation_disabled`; a device will not reach active status. This is expected and must not be mistaken for a working VPN enrollment.

Before enabling mutations on the isolated staging host:
- Take and verify a restorable backup of `/etc/wireguard/wg0.conf`.
- Capture `wg show`, `ip -4 route show`, `ip -6 route show`, forwarding sysctls, firewall/NAT rules, and cloud source/destination-check settings.
- Confirm exact client pools and all pre-existing peer assignments.
- Confirm the service's `ReadWritePaths=/etc/wireguard` matches the actual config location.
- Confirm only TCP 8443 from the staging node is permitted to the mTLS listener; the admin listener remains behind TLS proxy.
- Review the pending command and route plan.

Only after those staging-specific checks and an approved rollback plan should `ZEUS_NODE_ALLOW_MUTATIONS=true` be set on the staging node. The unit is root-run but its service capability bounding set is limited to `CAP_NET_ADMIN`; filesystem writes are restricted to `/etc/wireguard`. Never enable mutations on the production VPN node as a shortcut.

## 7. End-to-end validation

A successful API login or a `/healthz` response does not prove VPN traffic works. Validate each layer separately:

1. Admin API: `/healthz` and `/readyz`.
2. Node channel: TLS certificate verification and successful mTLS polling/acknowledgement from the expected node identity.
3. Provisioning: add one staging peer; confirm both the device and peer reach `active` only after node acknowledgement.
4. Tunnel: confirm a recent WireGuard handshake and non-zero transfer counters on both sides.
5. Egress: confirm IPv4 forwarding, exact-client-subnet NAT/SNAT, forward and established-return rules, working DNS, MTU, and upstream HTTPS.
6. IPv6: either verify a real IPv6 route and egress end-to-end or make an explicit leak-prevention policy decision. Do not remove `::/0` blindly.
7. Resilience: test peer removal, retry/replay, rollback, Control Plane restart, node restart, and reboot persistence.
8. Android: test sign-in/enrollment, tunnel connection, real traffic, network switching, app process death, and device reboot on a physical device.

Use `scripts/diagnose-wireguard-node.sh` as a read-only aid. It does not repair the server. Keep the generated output private if it contains public endpoint or network topology details.

## Rollback

Stop the staging node agent before restoring its WireGuard configuration. Restore the verified backup, re-read the configuration with `wg-quick strip`/the applicable host tooling, and confirm the live peer set and routes match the captured baseline. Keep the Control Plane database backup and migration history. Do not test rollback against the production VPN node.
