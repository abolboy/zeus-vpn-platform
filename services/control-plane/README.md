# ZEUS Control Plane — Foundation

This service currently exposes process health, readiness, and API metadata only. It has no user, peer, subscription, database, or VPN-node mutation endpoints yet.

Run locally:

    go test ./...
    go run ./cmd/zeus-control-plane

Default listen address: :8080.
- ZEUS_HTTP_ADDR: listen address override.
- ZEUS_VERSION: build version override.

Endpoints:
- GET /healthz — process liveness.
- GET /readyz — process-only readiness; no database or VPN-node dependency exists yet.
- GET /v1/meta — product, API, build version, and development stage.

No production deployment or unauthenticated control operation is included in this foundation.
