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

After login, the user page reads the authenticated paginated `GET /v1/users` endpoint. User data is read-only in this milestone.

## Build

```sh
npm run build
npm run preview
```

## Current scope

- Persian-first, responsive dashboard with Neon Core, Cyberpunk and Aurora themes.
- Operator login/logout and authenticated user listing from the Control Plane.
- Explicit loading, error and no-data states for remote user data.
- The API connection indicator calls only `GET /healthz`; it does not prove database readiness or VPN-node health.
- Server inventory, traffic metrics, subscriptions and activity events remain sample data and are visibly identified as preview/demo information.
- Server creation and user status actions remain local demo actions when the API is not configured. They are not shown as live operations in connected mode.

## Security status

The UI is not approved for public production exposure. Configure TLS, exact CORS origins, a strong owner password, restricted network access, and a dedicated database role before deployment. The API currently provides read-only user access; device provisioning, peer operations, billing, and real telemetry are not implemented.
