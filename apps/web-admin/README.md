# ZEUS Web Admin

React SPA for the ZEUS VPN control panel. The current milestone is a responsive, Persian-first UI prototype with three switchable neon themes.

## Run locally

```sh
npm install
npm run dev
```

## Build

```sh
npm run build
npm run preview
```

## Current scope

- Dashboard overview, sample server inventory, user list/search, subscription plan cards, activity feed and UI settings.
- Three live themes: Neon Core, Cyberpunk and Aurora.
- Responsive navigation and layouts for desktop and mobile.
- Sample data only. Mutations are local UI state and are not sent to a backend.

## Security and integration status

This UI is not production-connected. Authentication, authorization/RBAC, API integration, audit persistence, CSRF/CORS policy, server provisioning and real telemetry are not implemented here. Do not expose this prototype as a production admin panel or enter real credentials/secrets.
