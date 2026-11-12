# ZEUS Web Admin

React SPA for the ZEUS VPN control panel. Persian-first UI with three switchable neon themes.

## Run locally

```sh
npm install
cp .env.example .env.local
npm run dev
```

## Configure read-only Control Plane health

Set `VITE_ZEUS_API_BASE_URL` to the Go Control Plane base URL, for example `http://127.0.0.1:8080`. The UI calls only `GET /healthz`. For cross-origin development, configure a narrowly scoped CORS policy; production should preferably serve the SPA and API behind one origin/reverse proxy.

This check does not authenticate an administrator and does not load server, user, subscription, or traffic data. It is not proof that VPN nodes or tunnels are healthy.

## Build

```sh
npm run build
npm run preview
```

## Current scope

- Dashboard, sample server inventory, user list/search, subscription plan cards, activity feed and UI settings.
- Three live themes: Neon Core, Cyberpunk and Aurora; selected theme and compact layout are saved locally in the browser.
- Premium operations-style dashboard hero, stronger visual hierarchy, refined cards/tables, keyboard focus states and reduced-motion support.
- Responsive desktop/mobile layouts.
- Sample data only; UI mutations remain local.
- Optional read-only Control Plane health check via `GET /healthz`.

## Security status

Authentication, RBAC, protected API routes, audit persistence, server provisioning, real telemetry and production CORS policy are not implemented. Do not expose this prototype as a production admin panel or enter real credentials/secrets.
