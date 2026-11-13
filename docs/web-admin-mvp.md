# ZEUS Web Admin

React SPA for the ZEUS VPN control panel. Persian-first UI with three switchable neon themes.

## Run locally

```sh
npm install
cp .env.example .env.local
npm run dev
```

## Configure the Control Plane

Set `VITE_ZEUS_API_BASE_URL` to the Go Control Plane base URL. For cross-origin development, configure one exact origin in the API's `ZEUS_ALLOWED_ORIGINS`; wildcard CORS is not supported.

When the API base URL is configured, the UI starts at an operator login screen. The bearer token stays in React memory only and is not persisted to localStorage. A page reload therefore requires a new login. Logout calls the server-side session revocation endpoint.

After login, the user page reads the authenticated paginated `GET /v1/users` endpoint. Owner/admin roles can create users and edit display name, status, and device limit through authenticated API calls. Support/read-only roles see the user data but do not get mutation controls.

Mutation requests use cryptographically random idempotency keys. The key is preserved when retrying the same form payload after a network error and regenerated if the payload is changed.

## Build

```sh
npm run build
npm run preview
```

## Current scope

- Persian-first, responsive dashboard with Neon Core, Cyberpunk and Aurora themes.
- Operator login/logout and authenticated user listing from the Control Plane.
- Create user and edit name/status/device limit with server-side role enforcement.
- Explicit loading, error and no-data states for remote user data.
- The API connection indicator calls only `GET /healthz`; it does not prove database readiness or VPN-node health.
- Server inventory, traffic metrics, subscriptions and activity events remain sample data and are visibly identified as preview/demo information.
- Device registration/revocation, WireGuard peer provisioning, billing, subscriptions, and live traffic telemetry are not implemented.

## Security status

The UI is not approved for public production exposure. Configure TLS, exact CORS origins, a strong owner password, restricted network access, and a dedicated database role before deployment. The API currently supports operator authentication and VPN-user CRUD only; no device or node actions are exposed.
