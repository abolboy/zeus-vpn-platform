# ZEUS Control Plane — Phase 2 Alpha

The Go service now supports PostgreSQL-backed operator authentication and a read-only VPN-user listing. Peer provisioning, node-agent commands, subscriptions, billing, and production deployment are not implemented.

## Requirements
- Go 1.25 or newer.
- PostgreSQL 14 or newer.
- A database created specifically for ZEUS; do not point this service at an unrelated database.

## Local setup
Set `ZEUS_DATABASE_URL` to a PostgreSQL connection string. Keep it in a local environment file or secret manager; never commit credentials.

Run the explicit migration command before starting the API:

    go run ./cmd/zeus-migrate

Create the first owner only by setting both variables for the first startup:

    ZEUS_BOOTSTRAP_ADMIN_LOGIN=owner
    ZEUS_BOOTSTRAP_ADMIN_PASSWORD=<a unique password of at least 14 bytes>

The bootstrap command creates the owner if the login does not already exist. It does not reset or overwrite an existing account. Remove the bootstrap password from the process environment after the first successful startup.

Start the API:

    go run ./cmd/zeus-control-plane

Optional settings:
- `ZEUS_HTTP_ADDR`: listen address; defaults to `:8080`.
- `ZEUS_VERSION`: reported build version.
- `ZEUS_ENV=production`: requires a database URL using `sslmode=verify-full` and bootstrap credentials.
- `ZEUS_ALLOWED_ORIGINS`: comma-separated exact browser origins; no wildcard is accepted.

## Endpoints
- `GET /healthz`: process liveness only.
- `GET /readyz`: database connectivity and required schema readiness.
- `GET /v1/meta`: service metadata.
- `POST /v1/auth/login`: operator login; returns a random, short-lived bearer token once.
- `GET /v1/auth/me`: current operator identity.
- `POST /v1/auth/logout`: revoke the current session.
- `GET /v1/users?limit=25&cursor=...`: paginated read-only user list; operator authentication required.

## Security behavior
- Passwords use Argon2id (64 MiB, 3 iterations, 2 lanes).
- Raw session tokens are never stored; only SHA-256 hashes are persisted.
- Sessions expire after 8 hours and logout revokes them.
- Login attempts are throttled per remote IP and Argon2 work is concurrency-bounded.
- Errors do not reveal whether an operator login exists.
- API roles are checked server-side. This milestone exposes reads only.
- The service does not trust forwarded client-IP headers.
- CORS is deny-by-default and only allows explicitly configured origins.
- Private WireGuard keys are neither stored nor returned by the control plane.

## Database migrations
Migrations are explicit and versioned. API startup never changes database schema. The migration runner takes a PostgreSQL advisory lock, applies each migration transactionally, records a SHA-256 checksum, and refuses to run if an applied migration file was modified.

## Deployment boundary
This alpha is not yet approved for public exposure. Put it behind a TLS-terminating reverse proxy, restrict administrative access, configure a strong owner password and exact CORS origins, and use a dedicated least-privilege database role. Do not deploy it to the live VPN node until staging, backup, rollback, and security tests are complete.
