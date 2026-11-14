# ZEUS Control Plane — Phase 2 Alpha

The Go service supports PostgreSQL-backed operator authentication, verified OIDC customer identity, self-service VPN device enrollment, one-time invite-code fallback, a durable peer command queue, and a separate optional mTLS node-agent listener. This remains an alpha: staging deployment, full node inventory bootstrap, production rollout, subscriptions, billing, and live telemetry remain incomplete.

## Requirements
- Go 1.27 or newer.
- PostgreSQL 16 (CI baseline; earlier supported releases require separate compatibility testing).
- A database created specifically for ZEUS; do not point this service at an unrelated database.

## Local setup
Set ZEUS_DATABASE_URL to a PostgreSQL connection string. Keep it in a local environment file or secret manager; never commit credentials.

Run the explicit migration command before starting the API:

    go run ./cmd/zeus-migrate

Create the first owner only by setting both variables for the first startup:

    ZEUS_BOOTSTRAP_ADMIN_LOGIN=owner
    ZEUS_BOOTSTRAP_ADMIN_PASSWORD=<a unique password of at least 14 bytes>

The bootstrap command creates the owner if the login does not already exist. It does not reset or overwrite an existing account. Remove the bootstrap password from the process environment after the first successful startup.

Start the API:

    go run ./cmd/zeus-control-plane

Optional settings:
- ZEUS_HTTP_ADDR: listen address; defaults to :8080.
- ZEUS_VERSION: reported build version.
- ZEUS_ENV=production: requires a database URL using sslmode=verify-full.
- ZEUS_ALLOWED_ORIGINS: comma-separated exact browser origins; no wildcard is accepted.
- ZEUS_OIDC_ISSUER and ZEUS_OIDC_CLIENT_ID: configure together to enable self-service customer enrollment through a verified OIDC identity provider. The app client is a public native client; no client secret belongs in the Android app.

## Customer self-service identity

For a staging account, deploy the Cognito stack from the repository root:

    aws cloudformation deploy \
      --template-file infra/aws-cognito/zeus-customer-user-pool.yaml \
      --stack-name zeus-vpn-customer-identity \
      --capabilities CAPABILITY_IAM

Read the stack outputs OidcIssuer, OidcClientId, and HostedUiDomain. Configure the Control Plane process with:

    ZEUS_OIDC_ISSUER=<OidcIssuer>
    ZEUS_OIDC_CLIENT_ID=<OidcClientId>

Build the Android client with the matching public settings and the deployed HTTPS API URL:

    ./gradlew --no-daemon :apps:android:assembleDebug \
      -PZEUS_API_BASE_URL=https://<your-api-host> \
      -PZEUS_OIDC_ISSUER=<OidcIssuer> \
      -PZEUS_OIDC_CLIENT_ID=<OidcClientId>

The Cognito stack enables public self-registration, email verification, browser sign-in, a public native app client with no client secret, and the Authorization Code grant. AppAuth supplies PKCE and uses the exact callback URI com.zeusvpn.app:/oauth2redirect. Do not add a client secret to the Android package.

This setting makes registration publicly available to anyone who can reach the hosted login page. The current ZEUS alpha applies a per-IP enrollment throttle and a default device cap, but does not yet include billing, subscription enforcement, CAPTCHA, or a customer support workflow. Do not expose public self-registration on a production service until capacity limits, abuse controls, monitoring, and the intended free/trial/paid policy are decided. The template uses Cognito's default email sender for initial setup; configure and verify an Amazon SES identity for production-scale email delivery.

## Node-agent certificates

The certificate utility is built from services/vpn-node:

    go build -o zeus-node-cert ./cmd/zeus-node-cert

Create the root CA on a separate, encrypted offline administrator workstation. Keep its private key offline; it must never be copied to the Control Plane host, a VPN node, or CI. Issue a distinct ClientAuth certificate for each pre-registered vpn_nodes.id, then register the fingerprint and validity interval in vpn_node_agent_certificates. The node receives only its own client certificate/key and the Control Plane server CA. The tool does not write to PostgreSQL or automatically configure a node. Upload the public leaf certificate through the authenticated registration endpoint; the API independently verifies the certificate chain against the configured node CA, ClientAuth-only usage, validity interval, and exact node URI SAN before storing its fingerprint.

Routine rotation uses two active fingerprints temporarily: register the replacement, install it, verify node polling/acknowledgement, and only then revoke the old fingerprint by setting revoked_at. See docs/node-agent-protocol.md and services/vpn-node/README.md for exact instructions.

Do not register a node for client provisioning until its endpoint, server public key, interface addresses, IPv4/IPv6 pools, and pre-existing peers have been independently verified and reserved.

## Endpoints
- GET /healthz: process liveness only.
- GET /readyz: database connectivity and required schema readiness.
- GET /v1/meta: service metadata.
- POST /v1/auth/login, GET /v1/auth/me, POST /v1/auth/logout: operator sessions.
- Authenticated user/device APIs support owner/admin mutations and read access for support/read-only roles.
- POST /v1/client/enroll: accepts either the legacy one-time activation code (no bearer token) or an OIDC ID token (Authorization: Bearer). The OIDC path requires verified email and binds the customer to issuer + subject, provisions the account on first use, and returns HTTP 202 while peer provisioning remains asynchronous.
- GET /v1/nodes/{nodeID}/certificates: authenticated certificate inventory for owner/admin/support/read_only.
- POST /v1/nodes/{nodeID}/certificates: owner/admin registration of a CA-verified, node-bound ClientAuth certificate.
- POST /v1/nodes/{nodeID}/certificates/{certificateID}/revoke: owner/admin certificate revocation.
- GET /agent/v1/commands/next and POST /agent/v1/commands/{commandID}/ack: separate mTLS-only node-agent routes.

## Security behavior
- Passwords use Argon2id (64 MiB, 3 iterations, 2 lanes).
- Raw session tokens are never stored; only SHA-256 hashes are persisted.
- Sessions expire after 8 hours and logout revokes them.
- Login attempts are throttled per remote IP and Argon2 work is concurrency-bounded.
- Errors do not reveal whether an operator login exists.
- API roles are checked server-side.
- The service does not trust forwarded client-IP headers.
- CORS is deny-by-default and only allows explicitly configured origins.
- Client WireGuard private keys remain on client devices; node private keys remain on nodes; the Control Plane stores public keys only.
- Production PostgreSQL connections require sslmode=verify-full. Admin HTTP must be behind a TLS reverse proxy, and the application listener should remain private.

## Database migrations
Migrations are explicit and versioned. API startup never changes database schema. The migration runner takes a PostgreSQL advisory lock, applies each migration transactionally, records a SHA-256 checksum, and refuses to run if an applied migration file was modified.

## Deployment boundary
This alpha is not yet approved for public exposure. Use a dedicated least-privilege database role, secure backups, TLS, and an isolated staging deployment. Do not deploy it to the live VPN node or change production WireGuard peers until staging, rollback, and security tests are complete and separately approved.
