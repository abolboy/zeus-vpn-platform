# ZEUS Web Admin

React SPA for the ZEUS VPN control panel. Persian-first UI with three switchable neon themes.

## Run locally

    npm install
    cp .env.example .env.local
    npm run dev

## Configure the Control Plane

Set \`VITE_ZEUS_API_BASE_URL\` to the Go Control Plane base URL. For cross-origin development, configure one exact origin in the API's \`ZEUS_ALLOWED_ORIGINS\`; wildcard CORS is not supported.

When the API base URL is configured, the UI starts at an operator login screen. The bearer token stays in React memory only and is not persisted to localStorage. A page reload therefore requires a new login. Logout calls the server-side session revocation endpoint.

After login:
- The user page reads the authenticated, cursor-paginated \`GET /v1/users\` endpoint.
- Owner/admin roles can create users and edit display name, status, and device limit.
- Owner/admin/support/read_only roles can view devices for a user.
- Owner/admin roles can submit a device revocation. The UI asks for a second confirmation and shows \`revoking\` until the node successfully acknowledges \`peer.remove\`.
- The UI distinguishes \`provisioning\`, \`active\`, \`revoking\`, \`revoked\`, and \`error\` device states.
- Device creation is intentionally not offered in the operator dashboard. The client device must generate and retain its own WireGuard private key, then submit only the public key through the authenticated device API. Android client registration is a remaining integration task.

Mutation requests use cryptographically random idempotency keys. A key is preserved when retrying the same operation after a network error and is regenerated for a different user-entered payload. Device revocation is asynchronous; a successful HTTP 202 means the command was queued, not that the tunnel is already removed.

## Build

    npm run build
    npm run preview

## Current scope

- Persian-first, responsive dashboard with Neon Core, Cyberpunk and Aurora themes.
- Operator login/logout and authenticated user listing from the Control Plane.
- Create user and edit name/status/device limit with server-side role enforcement.
- Authenticated device list, owner/admin revocation workflow, and one-time client activation-code issuance.
- Device and peer status comes from the database; revocation completes only after node acknowledgement.
- User suspension blocks new device enrollment but does not automatically revoke already-provisioned peers.
- The API connection indicator calls only \`GET /healthz\`; it does not prove database readiness or VPN-node health.
- Server inventory, traffic metrics, subscriptions and activity events remain sample data and are visibly identified as preview/demo information.
- Android-side key generation, device registration, encrypted local profile delivery, billing, subscriptions, live traffic telemetry, and node onboarding UI remain unimplemented.

## Security status

The UI is not approved for public production exposure. Configure TLS, exact CORS origins, a strong owner password, restricted network access, and a dedicated database role before deployment. Never place a client private key in the dashboard, browser storage, a command payload, or the Control Plane.
