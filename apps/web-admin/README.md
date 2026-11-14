# ZEUS Web Admin

React SPA for the ZEUS VPN control panel. Persian-first UI with three switchable neon themes.

## Run locally

    npm install
    cp .env.example .env.local
    npm run dev

## Configure the Control Plane

Set VITE_ZEUS_API_BASE_URL to the Go Control Plane base URL. For cross-origin development, configure one exact origin in the API's ZEUS_ALLOWED_ORIGINS; wildcard CORS is not supported.

When the API base URL is configured, the UI starts at an operator login screen. The bearer token stays in React memory only and is not persisted to localStorage. A page reload therefore requires a new login. Logout calls the server-side session revocation endpoint.

After login:
- The user page reads authenticated, cursor-paginated VPN user records.
- Owner/admin roles can create users and edit display name, status, and device limit.
- Owner/admin/support/read_only roles can view devices for a user.
- Owner/admin roles can submit device revocation; completion waits for the node's peer.remove acknowledgement.
- The servers page reads live node inventory and active certificate counts from GET /v1/nodes.
- Owner/admin/support/read_only can inspect each node's certificate inventory.
- Owner/admin can register a public PEM leaf certificate and revoke a certificate. The Control Plane verifies the trusted CA chain, ClientAuth-only usage, validity, and exact spiffe://zeus/node/<UUID> identity before storing its fingerprint.
- Only the public certificate is submitted. Never paste a CA or node private key into the dashboard.
- The node mTLS CA must be configured on the Control Plane for certificate registration. Node inventory and certificate history can still be read when the node listener is not configured.

The admin UI does not create or mutate VPN nodes yet. Node bootstrap and inventory onboarding must remain a separate reviewed operation after verifying the staging node's WireGuard interface, pools, and existing peers.

## Build

    npm run build
    npm run preview

## Security status

The UI is not approved for public production exposure. Configure HTTPS, exact CORS origins, a strong owner password, restricted network access, and a dedicated database role before deployment. Never place any private key in the dashboard, browser storage, a command payload, or the Control Plane.
